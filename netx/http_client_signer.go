package netx

import (
	"net/http"
)

// Signature signs an outgoing request; body is the exact payload sent.
// AWS SigV4 lives in the netx/awssign module.
type Signature interface {
	Sign(originalRequest *http.Request, body []byte) (*http.Request, error)
}
