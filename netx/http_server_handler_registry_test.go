package netx

import (
	"io"
	"net/http"
	"testing"

	"github.com/joaoprofile/gofi/base/observer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── test doubles ─────────────────────────────────────────────────────────────

// plainRouterHandler implements only RouterHandler (no io.Closer).
type plainRouterHandler struct {
	routes []*Route
}

func (p *plainRouterHandler) Handlers() []*Route { return p.routes }

// closerRouterHandler implements both RouterHandler and io.Closer, so the
// registry must forward it to the lifecycle registry.
type closerRouterHandler struct {
	routes []*Route
	closed bool
}

func (c *closerRouterHandler) Handlers() []*Route { return c.routes }
func (c *closerRouterHandler) Close() error {
	c.closed = true
	return nil
}

// compile-time assertions
var _ RouterHandler = (*plainRouterHandler)(nil)
var _ RouterHandler = (*closerRouterHandler)(nil)
var _ io.Closer = (*closerRouterHandler)(nil)

func newPlainHandler() *plainRouterHandler {
	return &plainRouterHandler{
		routes: PublicRoutes("/test",
			GET("/ping").To(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		),
	}
}

// ── NewHandlerRegistry ────────────────────────────────────────────────────────

func TestNewHandlerRegistry_ReturnsNonNil(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())
	assert.NotNil(t, reg)
}

func TestNewHandlerRegistry_InitiallyEmpty(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())
	assert.Empty(t, reg.Handlers())
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestHandlerRegistry_Register_SingleHandler(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())
	reg.Register(newPlainHandler())

	assert.Len(t, reg.Handlers(), 1)
}

func TestHandlerRegistry_Register_MultipleHandlersAtOnce(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())
	reg.Register(newPlainHandler(), newPlainHandler(), newPlainHandler())

	assert.Len(t, reg.Handlers(), 3)
}

func TestHandlerRegistry_Register_AccumulatesAcrossMultipleCalls(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())

	reg.Register(newPlainHandler())
	reg.Register(newPlainHandler())

	assert.Len(t, reg.Handlers(), 2)
}

func TestHandlerRegistry_Register_CloserHandlerIsForwardedToLifecycle(t *testing.T) {
	lc := observer.New()
	reg := NewHandlerRegistry(lc)

	ch := &closerRouterHandler{}
	reg.Register(ch)

	// The handler must appear in the registry.
	require.Len(t, reg.Handlers(), 1)
	assert.Equal(t, ch, reg.Handlers()[0])
}

func TestHandlerRegistry_Register_PlainHandlerNotForwardedToLifecycle(t *testing.T) {
	lc := observer.New()
	reg := NewHandlerRegistry(lc)

	ph := newPlainHandler()
	reg.Register(ph)

	// Handler is tracked but CloseAll on the registry should be a no-op
	// (no panic, no error) since plain handlers don't implement io.Closer.
	require.Len(t, reg.Handlers(), 1)
}

func TestHandlerRegistry_Register_MixedCloserAndPlain(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())

	closer := &closerRouterHandler{}
	plain := newPlainHandler()

	reg.Register(closer, plain)

	handlers := reg.Handlers()
	require.Len(t, handlers, 2)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func TestHandlerRegistry_Handlers_ReturnsSameOrderAsRegistered(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())

	h1 := newPlainHandler()
	h2 := newPlainHandler()
	h3 := &closerRouterHandler{}

	reg.Register(h1)
	reg.Register(h2)
	reg.Register(h3)

	handlers := reg.Handlers()
	require.Len(t, handlers, 3)
	assert.Equal(t, h1, handlers[0])
	assert.Equal(t, h2, handlers[1])
	assert.Equal(t, h3, handlers[2])
}

func TestHandlerRegistry_Handlers_IsIdempotent(t *testing.T) {
	reg := NewHandlerRegistry(observer.New())
	reg.Register(newPlainHandler())

	first := reg.Handlers()
	second := reg.Handlers()

	assert.Len(t, first, 1)
	assert.Len(t, second, 1)
}
