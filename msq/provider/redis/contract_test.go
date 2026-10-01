package redis_test

import (
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofi-labs/gofi-sdk-go/msq/msqtest"
	redisprovider "github.com/gofi-labs/gofi-sdk-go/msq/provider/redis"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

func addr(t *testing.T) string {
	if a := os.Getenv("GOFI_IT_REDIS_ADDR"); a != "" {
		return a
	}
	return miniredis.RunT(t).Addr()
}

func topic(*testing.T) string { return "it-" + uuid.NewString()[:8] }

func TestContract_PubSub(t *testing.T) {
	b := redisprovider.NewWithClient(goredis.NewClient(&goredis.Options{Addr: addr(t)}))
	msqtest.Run(t, msqtest.Target{Broker: b, Topic: topic})
}

func TestContract_Streams(t *testing.T) {
	b := redisprovider.NewWithClient(goredis.NewClient(&goredis.Options{Addr: addr(t)}),
		redisprovider.Config{Mode: redisprovider.ModeStreams, ClaimIdle: 200 * time.Millisecond})
	msqtest.Run(t, msqtest.Target{Broker: b, Topic: topic, Caps: msqtest.Capabilities{Redelivery: true, DeliveryCount: true}})
}
