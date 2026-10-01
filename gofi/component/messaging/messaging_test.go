package messaging

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
	"github.com/gofi-labs/gofi-sdk-go/msq"
	msqcore "github.com/gofi-labs/gofi-sdk-go/msq/core"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBroker struct {
	setupCalled bool
	setupErr    error
	closed      bool
}

func (m *mockBroker) NewProducer() (msq.Producer, error)                  { return nil, nil }
func (m *mockBroker) NewConsumer(msq.ConsumeConfig) (msq.Consumer, error) { return nil, nil }
func (m *mockBroker) Close() error                                        { m.closed = true; return nil }
func (m *mockBroker) Setup(context.Context) error                         { m.setupCalled = true; return m.setupErr }

func newRuntime(env *environment.Environment) *gofi.Runtime {
	logging.NewLogger("test")
	return gofi.NewRuntime(env)
}

func TestNewOpensRegisteredProvider(t *testing.T) {
	const bt msq.BrokerType = "test-registry"
	broker := &mockBroker{}
	var got msq.ProviderConfig
	msq.Register(bt, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
		got = cfg
		return broker, nil
	})

	rt := newRuntime(&environment.Environment{MessagingProvider: string(bt), MessagingHost: "mq", MessagingPort: 5672})
	c := New(msq.Config{Exchange: "ex"})
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Equal(t, bt, got.Type)
	assert.Equal(t, "ex", got.Exchange)
	assert.Equal(t, "mq:5672", got.Addr)
	assert.NotNil(t, c.Broker())
	assert.True(t, broker.setupCalled)

	require.NoError(t, rt.Close(context.Background()))
	assert.True(t, broker.closed, "the broker opened by gofi is closed")
}

func TestNewWithoutProviderFailsStart(t *testing.T) {
	err := New().Start(context.Background(), newRuntime(&environment.Environment{}))
	assert.ErrorContains(t, err, "no broker configured")
}

func TestNewWithPreBuiltBrokerRunsSetup(t *testing.T) {
	broker := &mockBroker{}
	c := New(msq.Config{Broker: broker})
	assert.False(t, broker.setupCalled, "setup runs in Start")

	require.NoError(t, c.Start(context.Background(), newRuntime(&environment.Environment{})))
	assert.True(t, broker.setupCalled)
	assert.NotNil(t, c.Broker())
}

func TestNewSetupErrorFailsStart(t *testing.T) {
	broker := &mockBroker{setupErr: errors.New("setup failed")}
	err := New(msq.Config{Broker: broker}).Start(context.Background(), newRuntime(&environment.Environment{}))
	assert.ErrorContains(t, err, "broker setup: setup failed")
}

func TestFromBrokerIsUsedAsIs(t *testing.T) {
	broker := &mockBroker{}
	rt := newRuntime(&environment.Environment{})
	c := FromBroker(broker)
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Same(t, broker, c.Broker())
	assert.False(t, broker.setupCalled)

	require.NoError(t, rt.Close(context.Background()))
	assert.False(t, broker.closed, "the caller owns FromBroker brokers")
}

func TestLogEventDoesNotPanic(t *testing.T) {
	logging.NewLogger("test")
	for _, ev := range []msq.BrokerEvent{
		{Type: msq.EventProducerError, Error: errors.New("x")},
		{Type: msq.EventMessageNacked, MessageID: "id"},
		{Type: msq.EventConsumerStarted},
		{Type: "other"},
	} {
		assert.NotPanics(t, func() { logEvent(context.Background(), ev) })
	}
}

func TestEventLevel(t *testing.T) {
	for typ, want := range map[msq.BrokerEventType]slog.Level{
		msq.EventProducerError:       slog.LevelError,
		msq.EventConsumerError:       slog.LevelError,
		msq.EventMessageRejected:     slog.LevelError,
		msq.EventConsumerRestarting:  slog.LevelError,
		msq.EventMessageNacked:       slog.LevelWarn,
		msq.EventMessageDeadLettered: slog.LevelWarn,
		msq.EventConsumerStarted:     slog.LevelInfo,
		msq.EventConsumerStopped:     slog.LevelInfo,
		msq.EventMessageAcked:        slog.LevelDebug,
	} {
		assert.Equal(t, want, eventLevel(typ), typ)
	}
}

func TestLifecycle_HealthAndCloseContext(t *testing.T) {
	const bt msq.BrokerType = "test-lifecycle"
	var got msq.ProviderConfig
	msq.Register(bt, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
		got = cfg
		return &mockBroker{}, nil
	})
	rt := newRuntime(&environment.Environment{
		MessagingProvider: string(bt), MessagingTLSCAFile: "/ca.pem", MessagingAllowPlaintextSASL: true,
	})
	c := New()
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Equal(t, "/ca.pem", got.TLS.CAFile)
	assert.True(t, got.AllowPlaintextSASL)

	hcs := rt.HealthChecks()
	require.Len(t, hcs, 1)
	assert.Equal(t, "messaging", hcs[0].Name)
	assert.NoError(t, hcs[0].Check(context.Background()), "ready without consumers")

	svc, ok := c.Broker().(*msqcore.BrokerService)
	require.True(t, ok)
	svc.NewConsumerManager().Close()
	assert.ErrorIs(t, hcs[0].Check(context.Background()), msqcore.ErrManagerClosed, "a stopped manager is not ready")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NoError(t, rt.Close(ctx))
}

func TestInsecureTransports(t *testing.T) {
	var _ gofi.TransportChecker = New()
	env := &environment.Environment{AppEnvironment: "prod", MessagingProvider: "kafka", MessagingHost: "mq", MessagingPort: 9092}

	found := New().InsecureTransports(env)
	require.Len(t, found, 1)
	assert.Equal(t, gofi.InsecureTransport{Resource: "messaging", Setting: "MESSAGING_USE_TLS / MESSAGING_TLS_*", Detail: "kafka broker mq:9092 without verified TLS"}, found[0])
	assert.ErrorIs(t, core.CheckTransport(env, found), core.ErrInsecureTransport, "refused in prod")
	env.AllowInsecureTransport = "messaging"
	assert.NoError(t, core.CheckTransport(env, found), "allowed by the hatch")

	env.MessagingUseTLS = true
	assert.Empty(t, New().InsecureTransports(env))
	env.MessagingTLSInsecureSkipVerify = true
	assert.Equal(t, "MESSAGING_TLS_INSECURE_SKIP_VERIFY", New().InsecureTransports(env)[0].Setting)

	redis := &environment.Environment{MessagingProvider: "redis", CacheURI: "redis://u:secret@r:6379"}
	found = New().InsecureTransports(redis)
	require.Len(t, found, 1)
	assert.Equal(t, "CACHE_USE_TLS / MESSAGING_TLS_*", found[0].Setting)
	assert.NotContains(t, found[0].Detail, "secret")

	assert.Empty(t, New(msq.Config{BrokerType: msq.BrokerSQS}).InsecureTransports(env), "SQS is always HTTPS")
	assert.Empty(t, New(msq.Config{Broker: &mockBroker{}}).InsecureTransports(env), "caller's broker")
	assert.Empty(t, FromBroker(&mockBroker{}).InsecureTransports(env), "injected broker")
	assert.Empty(t, New().InsecureTransports(&environment.Environment{}), "no provider")
}

func TestNewUnregisteredProviderFailsStart(t *testing.T) {
	rt := newRuntime(&environment.Environment{MessagingProvider: "not-registered-xyz"})
	assert.ErrorContains(t, New().Start(context.Background(), rt), "not-registered-xyz")
}

func TestIdentity(t *testing.T) {
	assert.Equal(t, "messaging", New().Name())
	assert.Equal(t, gofi.StageMessaging, New().Stage())
}
