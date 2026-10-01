package cache

import "testing"

func TestRedisOptions_TLS(t *testing.T) {
	if redisOptions(Config{URI: "cache:6380", TLS: true}).TLSConfig == nil {
		t.Fatal("TLSConfig must be set when TLS is enabled")
	}
	if redisOptions(Config{URI: "cache:6379"}).TLSConfig != nil {
		t.Fatal("TLS must stay off by default")
	}
}
