package redis

import (
	"crypto/tls"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestNewProvider_TLS(t *testing.T) {
	standalone := NewProvider(Config{Addr: "cache:6380", TLSEnabled: true})
	if cfg := standalone.client.(*goredis.Client).Options().TLSConfig; cfg == nil || cfg.MinVersion < tls.VersionTLS12 {
		t.Fatalf("standalone TLSConfig=%v, want TLS 1.2+", cfg)
	}

	cluster := NewProvider(Config{ClusterAddrs: []string{"n1:6380"}, TLSEnabled: true})
	if cluster.client.(*goredis.ClusterClient).Options().TLSConfig == nil {
		t.Fatal("cluster TLSConfig must be set")
	}

	plain := NewProvider(Config{Addr: "cache:6379"})
	if plain.client.(*goredis.Client).Options().TLSConfig != nil {
		t.Fatal("TLS must stay off when TLSEnabled is false")
	}
}
