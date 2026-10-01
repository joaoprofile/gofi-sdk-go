package handler

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/examples/obs/telemetry"
	"github.com/gofi-labs/gofi-sdk-go/netx"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type PaymentRequest struct {
	Amount float64 `json:"amount"`
	Method string  `json:"method"`
}

type PaymentResult struct {
	Approved bool `json:"approved"`
}

// PaymentHandler fakes an external payment API. It runs in the same process
// only to keep the example small: the trace looks the same as with a real
// second service, because the context travels in the traceparent header.
type PaymentHandler struct {
	metrics *telemetry.Metrics
}

func NewPaymentHandler(m *telemetry.Metrics) *PaymentHandler { return &PaymentHandler{metrics: m} }

func (h *PaymentHandler) Handlers() []*netx.Route {
	return netx.PublicRoutes("/payments", netx.POST("/").To(h.pay))
}

// POST /payments
func (h *PaymentHandler) pay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in PaymentRequest
	if err := netx.ParseRequestBody(w, r, &in); err != nil {
		netx.Error(w, http.StatusBadRequest, err)
		return
	}

	// Occasionally slow, so the latency percentiles have something to show.
	delay := time.Duration(20+rand.IntN(80)) * time.Millisecond // #nosec G404 -- simulated load, not a secret
	if rand.IntN(100) < 5 {                                     // #nosec G404 -- simulated load, not a secret
		delay += 400 * time.Millisecond
	}
	time.Sleep(delay)

	approved := rand.IntN(100) >= 8 // #nosec G404 -- simulated load, not a secret
	outcome := "approved"
	if !approved {
		outcome = "declined"
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("payment.outcome", outcome))
	// A span event is a timestamped note inside the span.
	span.AddEvent("payment.authorized", trace.WithAttributes(attribute.Bool("approved", approved)))
	h.metrics.PaymentsProcessed.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))

	if !approved {
		logging.FromContext(ctx).WarnContext(ctx, "payment declined", "method", in.Method)
		netx.Error(w, http.StatusPaymentRequired, errors.New("payment declined"))
		return
	}
	netx.Response(w, http.StatusOK, PaymentResult{Approved: true})
}
