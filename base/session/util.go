package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var sessionCounter uint64

func GenerateSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)

	counter := atomic.AddUint64(&sessionCounter, 1)

	return fmt.Sprintf(
		"session_%s_%x_%x",
		hex.EncodeToString(b),
		time.Now().UnixNano(),
		counter,
	)
}

func NewKey(prefix string, parts ...interface{}) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, part := range parts {
		b.WriteRune('|')
		b.WriteString(fmt.Sprintf("%v", part))
	}
	hash := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%s:%s", prefix, hex.EncodeToString(hash[:])[:16])
}
