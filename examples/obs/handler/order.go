package handler

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/examples/obs/store"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/telemetry"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/worker"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	errInvalidOrder = errors.New("amount must be positive and payment_method set")
	errOutOfStock   = errors.New("out of stock")
	errDeclined     = errors.New("payment declined")
)

type CreateOrder struct {
	Amount        float64 `json:"amount"`
	PaymentMethod string  `json:"payment_method"`
}

// OrderHandler shows how to instrument a request that runs a multi-step flow:
// validate -> reserve stock -> charge (HTTP call) -> save -> publish event.
type OrderHandler struct {
	store    *store.Store
	queue    *worker.Queue
	payments *httpx.HttpClient
	metrics  *telemetry.Metrics
}

func NewOrderHandler(s *store.Store, q *worker.Queue, payments *httpx.HttpClient, m *telemetry.Metrics) *OrderHandler {
	return &OrderHandler{store: s, queue: q, payments: payments, metrics: m}
}

func (h *OrderHandler) Handlers() []*httpx.Route {
	return httpx.PublicRoutes("/orders",
		httpx.POST("/").To(h.create),
		httpx.GET("/{id}").To(h.get),
	)
}

// POST /orders
func (h *OrderHandler) create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Trace context in the logger: every line carries trace_id and span_id,
	// so Grafana jumps from a log line to its trace and back.
	log := logging.FromContext(ctx)

	var in CreateOrder
	if err := httpx.ParseRequestBody(w, r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err)
		return
	}

	// httpx already opened the server span; business attributes go on it so
	// traces can be searched by them in Tempo ({span.order.payment_method="pix"}).
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("order.payment_method", in.PaymentMethod),
		attribute.Float64("order.amount", in.Amount),
	)

	h.metrics.OrdersInProgress.Add(ctx, 1)
	order, err := h.place(ctx, in)
	h.metrics.OrdersInProgress.Add(ctx, -1)

	// Keep metric attributes low-cardinality: a few known values each, never
	// IDs, emails or amounts (those belong in spans and logs).
	status := statusLabel(err)
	h.metrics.OrdersCreated.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", status),
		attribute.String("payment_method", in.PaymentMethod),
	))

	if err != nil {
		log.WarnContext(ctx, "order rejected", "reason", status, "error", err)
		httpx.Error(w, httpStatus(err), err)
		return
	}

	h.metrics.OrderAmount.Record(ctx, order.Amount,
		metric.WithAttributes(attribute.String("payment_method", order.PaymentMethod)))
	log.InfoContext(ctx, "order created", "order_id", order.ID, "amount", order.Amount)
	httpx.Response(w, http.StatusCreated, order)
}

// place is the order flow. Its span groups the steps; each step is a child.
func (h *OrderHandler) place(ctx context.Context, in CreateOrder) (order store.Order, err error) {
	ctx, span := telemetry.Tracer().Start(ctx, "order.place")
	defer func() {
		if err != nil {
			telemetry.Fail(span, err)
		}
		span.End()
	}()

	if err = telemetry.Step(ctx, "order.validate", func(context.Context) error {
		if in.Amount <= 0 || in.PaymentMethod == "" {
			return errInvalidOrder
		}
		return nil
	}); err != nil {
		return order, err
	}

	if err = telemetry.Step(ctx, "stock.reserve", reserveStock); err != nil {
		return order, err
	}

	if err = h.charge(ctx, in); err != nil {
		return order, err
	}

	order = h.store.Create(in.Amount, in.PaymentMethod)
	span.SetAttributes(attribute.String("order.id", order.ID))

	// The event carries the trace context, so the consumer continues this trace.
	return order, h.queue.Publish(ctx, worker.Message{Type: "order.paid", OrderID: order.ID})
}

// charge calls the payment API through httpx.HttpClient. Its transport is
// instrumented: it creates a client span and sends the traceparent header, so
// the payment server span becomes a child of this one, even across services.
func (h *OrderHandler) charge(ctx context.Context, in CreateOrder) error {
	req := httpx.NewRequest[PaymentResult](ctx, h.payments, http.MethodPost, "/payments")
	req.SetBody(PaymentRequest{Amount: in.Amount, Method: in.PaymentMethod})
	if _, err := req.Execute(); err != nil {
		var httpErr *httpx.HttpError
		if errors.As(err, &httpErr) && httpErr.Status == http.StatusPaymentRequired {
			return errDeclined
		}
		return err
	}
	return nil
}

// reserveStock simulates a database call: some latency and a few conflicts.
func reserveStock(ctx context.Context) error {
	time.Sleep(time.Duration(5+rand.IntN(30)) * time.Millisecond) // #nosec G404 -- simulated load, not a secret
	if rand.IntN(100) < 3 {                                       // #nosec G404 -- simulated load, not a secret
		return errOutOfStock
	}
	return nil
}

// GET /orders/{id}
func (h *OrderHandler) get(w http.ResponseWriter, r *http.Request) {
	order, err := h.store.Get(httpx.GetPathParam("id", r))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, err)
		return
	}
	httpx.Response(w, http.StatusOK, order)
}

func statusLabel(err error) string {
	switch {
	case err == nil:
		return "created"
	case errors.Is(err, errInvalidOrder):
		return "invalid"
	case errors.Is(err, errOutOfStock):
		return "out_of_stock"
	case errors.Is(err, errDeclined):
		return "declined"
	default:
		return "error"
	}
}

func httpStatus(err error) int {
	switch {
	case errors.Is(err, errInvalidOrder):
		return http.StatusBadRequest
	case errors.Is(err, errOutOfStock):
		return http.StatusConflict
	case errors.Is(err, errDeclined):
		return http.StatusPaymentRequired
	default:
		return http.StatusInternalServerError
	}
}
