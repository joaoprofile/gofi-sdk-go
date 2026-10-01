package redis

import (
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestNew_TLS(t *testing.T) {
	if New(Config{Addr: "cache:6380", TLSEnabled: true}).client.(*goredis.Client).Options().TLSConfig == nil {
		t.Fatal("standalone TLSConfig must be set")
	}
	if New(Config{ClusterAddrs: []string{"n1:6380"}, TLSEnabled: true}).client.(*goredis.ClusterClient).Options().TLSConfig == nil {
		t.Fatal("cluster TLSConfig must be set")
	}
	if New(Config{Addr: "cache:6379"}).client.(*goredis.Client).Options().TLSConfig != nil {
		t.Fatal("TLS must stay off when TLSEnabled is false")
	}
}
