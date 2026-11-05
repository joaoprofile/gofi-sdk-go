package cache

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// go-redis ignores a context deadline during network I/O unless
// ContextTimeoutEnabled is on: a Redis that stops answering would hold every
// caller for the client's ReadTimeout (seconds) however short its deadline.
func TestRedisOptions_ContextDeadlineBoundsAHungServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	client := redis.NewUniversalClient(redisOptions(Config{URI: ln.Addr().String()}))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = client.Get(ctx, "k").Err()

	if err == nil {
		t.Fatal("expected an error from a server that never answers")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("a 100ms deadline held the call for %s", took)
	}
}
