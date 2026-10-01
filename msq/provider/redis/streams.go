package redis

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/msq/worker"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const (
	streamField      = "m"
	defaultClaimIdle = time.Minute
	readBlock        = 2 * time.Second
	ackTimeout       = 5 * time.Second
)

// Producer

type streamProducer struct {
	client goredis.UniversalClient
	maxLen int64
}

func (p *streamProducer) args(msg *types.Message) (*goredis.XAddArgs, error) {
	if msg.Topic == "" {
		return nil, errors.New("redis producer: msg.Topic (stream) must be set")
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("redis producer: marshal failed: %w", err)
	}
	return &goredis.XAddArgs{Stream: msg.Topic, MaxLen: p.maxLen, Approx: p.maxLen > 0, Values: []any{streamField, payload}}, nil
}

func (p *streamProducer) SendMessage(ctx context.Context, msg *types.Message) error {
	a, err := p.args(msg)
	if err != nil {
		return err
	}
	if err := p.client.XAdd(ctx, a).Err(); err != nil {
		return fmt.Errorf("redis producer: xadd to %q failed: %w", msg.Topic, err)
	}
	return nil
}

func (p *streamProducer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	pipe := p.client.Pipeline()
	for _, msg := range msgs {
		a, err := p.args(msg)
		if err != nil {
			return err
		}
		pipe.XAdd(ctx, a)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis producer batch: pipeline exec failed: %w", err)
	}
	return nil
}

func (p *streamProducer) Close() error { return nil }

// Consumer

type streamConsumer struct {
	client      goredis.UniversalClient
	cfg         types.ConsumeConfig
	concurrency int
	group, name string
	claimIdle   time.Duration
	gate        worker.Gate
}

func newStreamConsumer(client goredis.UniversalClient, cfg types.ConsumeConfig, concurrency int, claimIdle time.Duration) *streamConsumer {
	host, _ := os.Hostname()
	return &streamConsumer{
		client:      client,
		cfg:         cfg,
		concurrency: concurrency,
		group:       cmp.Or(cfg.GroupID, cfg.Topic),
		name:        cmp.Or(host, "gofi") + "-" + uuid.NewString()[:8],
		claimIdle:   cmp.Or(cfg.VisibilityTimeout, claimIdle, defaultClaimIdle),
	}
}

// Consume reads new entries for the group and periodically claims entries
// left unacked for ClaimIdle (crashes and Nacks). Entries being handled here
// have their idle time reset every ClaimIdle/3, so they are never claimed
// while their handler runs, by another consumer or by this one.
func (c *streamConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	start := "$"
	if c.cfg.InitialOffset == types.OffsetResetEarliest {
		start = "0"
	}
	err := c.client.XGroupCreateMkStream(ctx, c.cfg.Topic, c.group, start).Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("redis consumer: create group %q on %q: %w", c.group, c.cfg.Topic, err)
	}

	pool := worker.New(c.concurrency)
	defer pool.Close()
	slots := worker.NewSlots(c.concurrency)

	backoff := worker.Backoff{Min: worker.ReceiveBackoffMin, Max: worker.ReceiveBackoffMax}
	var lastClaim time.Time
	for {
		if c.gate.Wait(ctx) != nil || ctx.Err() != nil {
			return nil
		}
		// Read only what can start now: a buffered entry would sit idle and be
		// claimed by another consumer before its handler even starts.
		free, err := slots.Acquire(ctx, c.concurrency)
		if err != nil {
			continue
		}
		var msgs []goredis.XMessage
		var counts map[string]int
		if time.Since(lastClaim) >= c.claimIdle/2 {
			msgs, counts = c.claim(ctx, free)
			lastClaim = time.Now()
		}
		if len(msgs) < free {
			read, err := c.read(ctx, free-len(msgs))
			if err != nil {
				c.dispatch(ctx, pool, slots, handler, msgs, counts)
				slots.Release(free - len(msgs))
				if ctx.Err() != nil {
					return nil
				}
				logging.Error("redis consumer: xreadgroup failed", slog.String("stream", c.cfg.Topic), slog.Any("error", err))
				_ = worker.Sleep(ctx, backoff.Next())
				continue
			}
			backoff.Reset()
			msgs = append(msgs, read...)
		}
		c.dispatch(ctx, pool, slots, handler, msgs, counts)
		slots.Release(free - len(msgs))
	}
}

// read returns up to n new entries; none when the block times out.
func (c *streamConsumer) read(ctx context.Context, n int) ([]goredis.XMessage, error) {
	streams, err := c.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.name,
		Streams:  []string{c.cfg.Topic, ">"},
		Count:    int64(n),
		Block:    min(readBlock, c.claimIdle/2), // keeps claims on schedule
	}).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var msgs []goredis.XMessage
	for _, s := range streams {
		msgs = append(msgs, s.Messages...)
	}
	return msgs, nil
}

// claim takes over up to n entries idle for ClaimIdle and returns their
// delivery counts (XAUTOCLAIM increments them).
func (c *streamConsumer) claim(ctx context.Context, n int) ([]goredis.XMessage, map[string]int) {
	msgs, _, err := c.client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
		Stream:   c.cfg.Topic,
		Group:    c.group,
		Consumer: c.name,
		MinIdle:  c.claimIdle,
		Start:    "0-0",
		Count:    int64(n),
	}).Result()
	if err != nil {
		if ctx.Err() == nil {
			logging.Warn("redis consumer: xautoclaim failed", slog.String("stream", c.cfg.Topic), slog.Any("error", err))
		}
		return nil, nil
	}
	if len(msgs) == 0 {
		return nil, nil
	}
	return msgs, c.deliveryCounts(ctx, msgs)
}

// deliveryCounts reads times_delivered of the claimed entries from XPENDING.
// The range is this consumer's, so it holds at most the claimed entries plus
// the ones in its handlers. A failure leaves the counts unknown (0).
func (c *streamConsumer) deliveryCounts(ctx context.Context, msgs []goredis.XMessage) map[string]int {
	pending, err := c.client.XPendingExt(ctx, &goredis.XPendingExtArgs{
		Stream:   c.cfg.Topic,
		Group:    c.group,
		Start:    msgs[0].ID,
		End:      msgs[len(msgs)-1].ID,
		Count:    int64(len(msgs) + c.concurrency),
		Consumer: c.name,
	}).Result()
	if err != nil {
		if ctx.Err() == nil {
			logging.Warn("redis consumer: xpending failed", slog.String("stream", c.cfg.Topic), slog.Any("error", err))
		}
		return nil
	}
	counts := make(map[string]int, len(pending))
	for _, p := range pending {
		counts[p.ID] = int(min(p.RetryCount, math.MaxInt32)) // #nosec G115 -- capped
	}
	return counts
}

// dispatch hands each entry to the pool; each holds one slot until handled.
// Entries without a count were read new: their first delivery.
func (c *streamConsumer) dispatch(ctx context.Context, pool *worker.Pool, slots *worker.Slots, handler port.MessageHandler, msgs []goredis.XMessage, counts map[string]int) {
	for _, m := range msgs {
		stop := c.keepAlive(ctx, m.ID)
		count, claimed := counts[m.ID]
		if !claimed {
			count = 1
		}
		pool.Enqueue(func() {
			defer slots.Release(1)
			c.handle(ctx, m, count, handler, stop)
		})
	}
}

// keepAlive resets the entry's idle time (XCLAIM to itself, JUSTID) until the
// returned stop is called, so XAUTOCLAIM never picks it while it is handled.
func (c *streamConsumer) keepAlive(ctx context.Context, id string) func() {
	return worker.KeepAlive(ctx, worker.RenewEvery(c.claimIdle), func(rctx context.Context) {
		if err := c.client.XClaimJustID(rctx, &goredis.XClaimArgs{
			Stream: c.cfg.Topic, Group: c.group, Consumer: c.name, Messages: []string{id},
		}).Err(); err != nil {
			logging.Warn("redis consumer: idle reset failed", slog.String("stream", c.cfg.Topic), slog.String("id", id), slog.Any("error", err))
		}
	})
}

// handle stops the idle reset before settling, so it never races the ack.
// Topic is the stream the entry was read from, never the payload's.
func (c *streamConsumer) handle(ctx context.Context, xm goredis.XMessage, deliveries int, handler port.MessageHandler, stopLease func()) {
	payload, _ := xm.Values[streamField].(string)
	msg := types.DecodeEnvelope([]byte(payload))
	msg.Topic = c.cfg.Topic
	msg.DeliveryCount = deliveries
	if msg.Id == uuid.Nil {
		msg.Id = types.StableID("redis/" + c.cfg.Topic + "/" + xm.ID)
	}
	result, err := handler.Handle(ctx, &msg)
	stopLease()
	// Reject acks too: the entry leaves the pending list for good.
	if result == types.Nack {
		// Left pending: claimed again after ClaimIdle.
		logging.Warn("redis consumer: handler nacked, redelivery after claim idle",
			slog.String("stream", c.cfg.Topic), slog.String("id", xm.ID), slog.Any("error", err))
		return
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
	defer cancel()
	if err := c.client.XAck(actx, c.cfg.Topic, c.group, xm.ID).Err(); err != nil {
		logging.Error("redis consumer: xack failed", slog.String("stream", c.cfg.Topic), slog.String("id", xm.ID), slog.Any("error", err))
	}
}

func (c *streamConsumer) Close() error  { return nil }
func (c *streamConsumer) Pause() error  { c.gate.Pause(); return nil }
func (c *streamConsumer) Resume() error { c.gate.Resume(); return nil }
