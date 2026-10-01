// Package job shows how to instrument scheduled work: no request starts it,
// so every run opens its own root trace and reports its own metrics.
package job

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/examples/obs/store"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/telemetry"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	name      = "archive-orders"
	batchSize = 10
)

var errStorageTimeout = errors.New("archive storage timeout")

// Archive moves notified orders older than a minute to the archive.
type Archive struct {
	store    *store.Store
	metrics  *telemetry.Metrics
	interval time.Duration
}

func NewArchive(s *store.Store, m *telemetry.Metrics, interval time.Duration) *Archive {
	return &Archive{store: s, metrics: m, interval: interval}
}

// Run executes the job every interval until ctx is cancelled.
func (j *Archive) Run(ctx context.Context) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.runOnce(ctx)
		}
	}
}

func (j *Archive) runOnce(ctx context.Context) {
	start := time.Now()
	// WithNewRoot: a run is its own trace, never a child of whatever
	// started the scheduler.
	ctx, span := telemetry.Tracer().Start(ctx, "job "+name,
		trace.WithNewRoot(),
		trace.WithAttributes(attribute.String("job.name", name)),
	)
	defer span.End()
	log := logging.FromContext(ctx)

	archived, err := j.archive(ctx)
	span.SetAttributes(attribute.Int("job.items", archived))

	outcome := "success"
	if err != nil {
		outcome = "error"
		telemetry.Fail(span, err)
		log.ErrorContext(ctx, "job failed", "job", name, "archived", archived, "error", err)
	} else {
		log.InfoContext(ctx, "job finished", "job", name, "archived", archived)
	}

	// Duration and outcome per run: enough for "is it running, is it failing,
	// is it getting slower" alerts.
	j.metrics.JobDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
		attribute.String("job.name", name),
		attribute.String("outcome", outcome),
	))
	j.metrics.JobItems.Add(ctx, int64(archived), metric.WithAttributes(attribute.String("job.name", name)))
}

// archive processes the orders in batches, one child span per batch.
func (j *Archive) archive(ctx context.Context) (int, error) {
	ids := j.store.ListByStatus(store.StatusNotified, time.Now().Add(-time.Minute))
	done := 0
	for i := 0; i < len(ids); i += batchSize {
		batch := ids[i:min(i+batchSize, len(ids))]
		err := telemetry.Step(ctx, "archive.batch", func(context.Context) error {
			time.Sleep(time.Duration(5+rand.IntN(20)) * time.Millisecond) // #nosec G404 -- simulated load, not a secret
			if rand.IntN(100) < 2 {                                       // #nosec G404 -- simulated load, not a secret
				return errStorageTimeout
			}
			for _, id := range batch {
				_ = j.store.SetStatus(id, store.StatusArchived)
			}
			return nil
		}, attribute.Int("batch.size", len(batch)))
		if err != nil {
			return done, err
		}
		done += len(batch)
	}
	return done, nil
}
