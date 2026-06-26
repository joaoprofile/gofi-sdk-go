package gofi

import (
	"context"
	"database/sql"
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/base/session"
	"github.com/joaoprofile/gofi/msq"
	"github.com/joaoprofile/gofi/netx"
	"github.com/joaoprofile/gofi/obs"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"

	_ "github.com/lib/pq"
)

//  Compile-time interface compliance
// These lines fail at compile time if gofiInstance stops satisfying its contracts.

var _ Builder = (*gofiInstance)(nil)
var _ Service = (*gofiInstance)(nil)

// newTestInstance creates a gofiInstance directly, bypassing newInstance() and its
// external side effects (TLS, cloud, observer). Safe for unit tests.
func newTestInstance() *gofiInstance {
	logging.NewLogger("test") // required before any method that logs
	return &gofiInstance{
		serviceName: "test-service",
		env:         &environment.Environment{},
	}
}

//  Builder: fluent API

// TestBuilderFluentReturnsBuilder covers methods that have no external
// infrastructure dependency (no DB, no Redis). Methods that connect to
// real infrastructure are covered in integration tests below.
func TestBuilderFluentReturnsBuilder(t *testing.T) {
	g := newTestInstance()

	assert.Implements(t, (*Builder)(nil), g.AddObservability())
	assert.Implements(t, (*Builder)(nil), g.AddMessaging())
	assert.Implements(t, (*Builder)(nil), g.NewHttpServer(":9999"))
}

func TestBuilderFluentReturnsSameInstance(t *testing.T) {
	g := newTestInstance()

	// Each chained call must return the same underlying pointer — no copies
	b1 := g.AddObservability()
	b2 := b1.AddMessaging()

	assert.Same(t, g, b1.(*gofiInstance))
	assert.Same(t, g, b2.(*gofiInstance))
}

func TestBuildReturnsService(t *testing.T) {
	g := newTestInstance()

	svc := g.Build()

	assert.NotNil(t, svc)
	assert.Implements(t, (*Service)(nil), svc)
}

func TestBuildReturnsSameUnderlyingInstance(t *testing.T) {
	g := newTestInstance()

	svc := g.Build()

	// Service and Builder point to the same struct — no copy is made
	assert.Same(t, g, svc.(*gofiInstance))
}

//  Service: resource accessors

func TestResourceAccessorsReturnNilWhenNotConfigured(t *testing.T) {
	svc := newTestInstance().Build()

	assert.Nil(t, svc.Database(), "Database() must be nil when AddDatabase() was not called")
	assert.Nil(t, svc.Cache(), "Cache() must be nil when AddCache() was not called")
	assert.Nil(t, svc.Messaging(), "Messaging() must be nil when AddMessaging() was not called")
	assert.Nil(t, svc.HttpServer(), "HttpServer() must be nil when NewHttpServer() was not called")
}

func TestDatabaseAccessorAfterSet(t *testing.T) {
	g := newTestInstance()

	// sql.Open does not connect — safe to use in unit tests
	db, err := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	assert.NoError(t, err)
	defer db.Close()

	g.databaseConn = db

	assert.Equal(t, db, g.Build().Database())
}

func TestCacheAccessorAfterSet(t *testing.T) {
	g := newTestInstance()

	// redis.NewClient does not connect until a command is issued
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6399"})
	defer rdb.Close()

	g.cacheConn = rdb

	assert.Equal(t, rdb, g.Build().Cache())
}

func TestMessagingAccessorAfterSet(t *testing.T) {
	g := newTestInstance()

	mock := &mockMessaging{}
	g.messagingConn = mock

	assert.Equal(t, mock, g.Build().Messaging())
}

func TestHttpServerAccessorAfterSet(t *testing.T) {
	g := newTestInstance()

	mock := &mockHttpServer{}
	g.httpServer = mock

	assert.Equal(t, mock, g.Build().HttpServer())
}

func TestEnvironmentAccessorReturnsInstance(t *testing.T) {
	g := newTestInstance()

	env := g.Build().Environment()

	assert.NotNil(t, env)
	assert.IsType(t, &environment.Environment{}, env)
}

func TestEnvironmentAccessorReturnsSamePointer(t *testing.T) {
	g := newTestInstance()

	env := &environment.Environment{AppName: "my-service"}
	g.env = env

	assert.Same(t, env, g.Build().Environment())
}

//  Service: Shutdown

func TestShutdownWithNoResourcesDoesNotPanic(t *testing.T) {
	svc := newTestInstance().Build()

	err := svc.Shutdown(context.Background())

	assert.NoError(t, err)
}

func TestShutdownClosesCacheConnection(t *testing.T) {
	g := newTestInstance()

	// Non-connected client — Close() is still callable
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6399"})
	g.cacheConn = rdb

	err := g.Shutdown(context.Background())

	assert.NoError(t, err)
	// After Close(), any command must return an error
	assert.Error(t, rdb.Ping(context.Background()).Err())
}

func TestShutdownClosesDatabaseConnection(t *testing.T) {
	g := newTestInstance()

	// sql.Open does not connect — Close() is still callable
	db, err := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	assert.NoError(t, err)
	g.databaseConn = db

	shutdownErr := g.Shutdown(context.Background())

	assert.NoError(t, shutdownErr)
	assert.Equal(t, 0, db.Stats().OpenConnections)
}

//  Integration tests (require real infrastructure)
// Run with: go test -run Integration ./gofi/

func TestIntegrationAddCache(t *testing.T) {
	t.Skip("requires Redis — run with real infrastructure")

	g := newTestInstance()
	b := g.AddCache()

	assert.Implements(t, (*Builder)(nil), b)
	assert.NotNil(t, g.cacheConn)
}

func TestIntegrationAddLockerCache(t *testing.T) {
	t.Skip("requires Redis — run with real infrastructure")

	g := newTestInstance()
	b := g.AddLockerCache()

	assert.Implements(t, (*Builder)(nil), b)
}

func TestIntegrationAddDatabase(t *testing.T) {
	t.Skip("requires PostgreSQL — run with real infrastructure")

	g := newTestInstance()
	b := g.AddDatabase()

	assert.Implements(t, (*Builder)(nil), b)
	assert.NotNil(t, g.databaseConn)
}

//  New() public constructor

func TestNewPublicFunctionReturnsBuilder(t *testing.T) {
	b := New("test-via-new")
	assert.NotNil(t, b)
	assert.Implements(t, (*Builder)(nil), b)
}

//  AddLockerCache (no-op) ──

func TestAddLockerCacheIsNoOp(t *testing.T) {
	g := newTestInstance()
	b := g.AddLockerCache()
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

//  AddSession ──

func TestAddSessionOCICacheType(t *testing.T) {
	g := newTestInstance()
	g.env.CacheType = string(environment.OCI_CACHE)
	b := g.AddSession()
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

func TestAddSessionUnknownCacheType(t *testing.T) {
	g := newTestInstance()
	g.env.CacheType = "unknown"
	b := g.AddSession()
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

func TestAddSessionWithCustomConfig(t *testing.T) {
	g := newTestInstance()
	g.env.CacheType = string(environment.OCI_CACHE)
	cfg := session.DefaultSessionConfig()
	b := g.AddSession(cfg)
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

func TestAddSessionWithNilConfigPointer(t *testing.T) {
	g := newTestInstance()
	g.env.CacheType = "unknown"
	var cfg *session.Config
	b := g.AddSession(cfg)
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

//  NewHttpServer with explicit config

func TestNewHttpServerWithExplicitConfig(t *testing.T) {
	g := newTestInstance()
	cfg := &netx.WSConfig{}
	b := g.NewHttpServer(":8080", cfg)
	assert.Implements(t, (*Builder)(nil), b)
	assert.NotNil(t, g.httpServer)
}

//  Handlers / Use / UseAuth

func TestHandlersWithMockHttpServer(t *testing.T) {
	g := newTestInstance()
	g.httpServer = &mockHttpServer{}
	b := g.Handlers()
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

func TestUseWithMockHttpServer(t *testing.T) {
	g := newTestInstance()
	g.httpServer = &mockHttpServer{}
	b := g.Use()
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

func TestUseAuthWithMockHttpServer(t *testing.T) {
	g := newTestInstance()
	g.httpServer = &mockHttpServer{}
	b := g.UseAuth(nil)
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
}

//  ListenAndServe ─

// TestListenAndServeWithHttpServer covers the httpServer != nil branch, which
// delegates immediately to the mock and returns.
func TestListenAndServeWithHttpServer(t *testing.T) {
	g := newTestInstance()
	g.httpServer = &mockHttpServer{} // ListenAndServe() is a no-op, returns immediately
	g.ListenAndServe()
}

// TestListenAndServeWithoutHttpServerReceivesSignal covers the signal-wait path.
// It spawns ListenAndServe in a goroutine and sends SIGTERM to unblock it.
func TestListenAndServeWithoutHttpServerReceivesSignal(t *testing.T) {
	g := newTestInstance()

	done := make(chan struct{})
	go func() {
		g.ListenAndServe()
		close(done)
	}()

	// Allow the goroutine to reach signal.Notify before sending the signal.
	time.Sleep(30 * time.Millisecond)

	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe did not return after SIGTERM")
	}
}

//  Shutdown branches ─

// TestShutdownWithHttpServer covers the httpServer != nil log line.
func TestShutdownWithHttpServer(t *testing.T) {
	g := newTestInstance()
	g.httpServer = &mockHttpServer{}
	err := g.Shutdown(context.Background())
	assert.NoError(t, err)
}

// TestShutdownCacheCloseError covers the error-log branch when Close() fails.
func TestShutdownCacheCloseError(t *testing.T) {
	g := newTestInstance()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6399"})
	_ = rdb.Close() // pre-close so the second Close() inside Shutdown returns an error
	g.cacheConn = rdb
	err := g.Shutdown(context.Background())
	assert.NoError(t, err) // Shutdown absorbs the close error
}

// TestShutdownDatabaseCloseError covers the error-log branch when DB Close() fails.
func TestShutdownDatabaseCloseError(t *testing.T) {
	g := newTestInstance()
	db, openErr := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	assert.NoError(t, openErr)
	db.Close() // pre-close so the second Close() inside Shutdown returns an error
	g.databaseConn = db
	err := g.Shutdown(context.Background())
	assert.NoError(t, err) // Shutdown absorbs the close error
}

//  AddCache

// TestAddCacheReturnsBuilder covers the AddCache path.
// InstanceRedis creates a client even if Redis is unreachable (ping error is logged only).
func TestAddCacheReturnsBuilder(t *testing.T) {
	g := newTestInstance()
	b := g.AddCache()
	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
	assert.NotNil(t, g.cacheConn)
}

//  AddDatabase (unit-testable paths)

// TestAddDatabasePoolConfigAndConnectionError covers pool/migration setup and the
// connection-error return path using a driver name that is not registered, which
// makes NewConnection fail immediately without a network timeout.
func TestAddDatabasePoolConfigAndConnectionError(t *testing.T) {
	g := newTestInstance()
	g.env.DatabaseDriver = "notregistered_driver_xyz"
	g.env.DatabaseHost = "localhost"
	g.env.DatabaseName = "testdb"
	g.env.DatabaseMaxOpenConns = 10
	g.env.DatabaseMaxIdleConns = 5
	g.env.DatabaseMaxLifetime = 1 * time.Second
	g.env.DatabaseMigration = true

	b := g.AddDatabase()

	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
	assert.Nil(t, g.databaseConn)
}

// TestAddDatabaseDefaultDriverFallback covers the cfg.Driver == "" branch that
// falls back to DriverPostgres. Port 1 guarantees a fast connection-refused error.
func TestAddDatabaseDefaultDriverFallback(t *testing.T) {
	g := newTestInstance()
	g.env.DatabaseDriver = "" // triggers the default-postgres fallback
	g.env.DatabaseHost = "localhost"
	g.env.DatabasePort = 1 // invalid port → immediate connection refused

	b := g.AddDatabase()

	assert.Implements(t, (*Builder)(nil), b)
	assert.Same(t, g, b.(*gofiInstance))
	assert.Nil(t, g.databaseConn)
}

//  With* injection methods ─────────

func TestWithDatabaseSetsConnectionAndReturnsSameInstance(t *testing.T) {
	g := newTestInstance()

	db, err := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	assert.NoError(t, err)
	defer db.Close()

	b := g.WithDatabase(db)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Equal(t, db, g.databaseConn)
}

func TestWithDatabaseNilIsAccepted(t *testing.T) {
	g := newTestInstance()

	b := g.WithDatabase(nil)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Nil(t, g.databaseConn)
}

func TestWithCacheSetsClientAndReturnsSameInstance(t *testing.T) {
	g := newTestInstance()

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6399"})
	defer rdb.Close()

	b := g.WithCache(rdb)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Equal(t, rdb, g.cacheConn)
}

func TestWithCacheNilIsAccepted(t *testing.T) {
	g := newTestInstance()

	b := g.WithCache(nil)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Nil(t, g.cacheConn)
}

func TestWithMessagingSetsConnectionAndReturnsSameInstance(t *testing.T) {
	g := newTestInstance()

	mock := &mockMessaging{}
	b := g.WithMessaging(mock)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Equal(t, mock, g.messagingConn)
}

func TestWithMessagingNilIsAccepted(t *testing.T) {
	g := newTestInstance()

	b := g.WithMessaging(nil)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Nil(t, g.messagingConn)
}

func TestWithObservabilitySetsFieldAndReturnsSameInstance(t *testing.T) {
	g := newTestInstance()

	tele := &obs.Telemetry{}
	b := g.WithObservability(tele)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Equal(t, tele, g.telemetry)
}

func TestWithObservabilityNilIsAccepted(t *testing.T) {
	g := newTestInstance()

	b := g.WithObservability(nil)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Nil(t, g.telemetry)
}

func TestWithHttpServerSetsServerAndReturnsSameInstance(t *testing.T) {
	g := newTestInstance()

	mock := &mockHttpServer{}
	b := g.WithHttpServer(mock)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Equal(t, mock, g.httpServer)
}

func TestWithHttpServerNilIsAccepted(t *testing.T) {
	g := newTestInstance()

	b := g.WithHttpServer(nil)

	assert.Same(t, g, b.(*gofiInstance))
	assert.Nil(t, g.httpServer)
}

// TestWithMethodsAreChainable verifies that all With* methods can be chained
// in a single expression and always return the same underlying pointer.
func TestWithMethodsAreChainable(t *testing.T) {
	g := newTestInstance()

	db, err := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	assert.NoError(t, err)
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6399"})
	defer rdb.Close()

	mock := &mockMessaging{}
	srv := &mockHttpServer{}
	tele := &obs.Telemetry{}

	result := g.
		WithDatabase(db).
		WithCache(rdb).
		WithMessaging(mock).
		WithObservability(tele).
		WithHttpServer(srv).
		Build()

	assert.NotNil(t, result)
	assert.Same(t, g, result.(*gofiInstance))
	assert.Equal(t, db, g.databaseConn)
	assert.Equal(t, rdb, g.cacheConn)
	assert.Equal(t, mock, g.messagingConn)
	assert.Equal(t, tele, g.telemetry)
	assert.Equal(t, srv, g.httpServer)
}

// TestWithMethodsExposeViaService verifies that values injected via With*
// are accessible through the Service accessors after Build().
func TestWithMethodsExposeViaService(t *testing.T) {
	g := newTestInstance()

	db, err := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	assert.NoError(t, err)
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6399"})
	defer rdb.Close()

	mock := &mockMessaging{}
	srv := &mockHttpServer{}

	svc := g.
		WithDatabase(db).
		WithCache(rdb).
		WithMessaging(mock).
		WithHttpServer(srv).
		Build()

	assert.Equal(t, db, svc.Database())
	assert.Equal(t, rdb, svc.Cache())
	assert.Equal(t, mock, svc.Messaging())
	assert.Equal(t, srv, svc.HttpServer())
}

//  AddObservability success path ──────

// TestAddObservabilityWithEndpointSetSucceeds verifies the happy path.
// obs.Init uses lazy gRPC — it succeeds without a real OTLP server.
func TestAddObservabilityWithEndpointSetSucceeds(t *testing.T) {
	g := newTestInstance()
	g.env.OtelExporterOTLPEndpoint = "localhost:4317"

	b := g.AddObservability()

	assert.Same(t, g, b.(*gofiInstance))
	assert.NotNil(t, g.telemetry)
}

func TestShutdownWithActiveTelemetryNilProvidersNoError(t *testing.T) {
	g := newTestInstance()
	g.env.OtelExporterOTLPEndpoint = "localhost:4317"
	g.AddObservability()
	assert.NotNil(t, g.telemetry)

	err := g.Shutdown(context.Background())

	assert.NoError(t, err)
}

//  brokerFromEnv

func TestBrokerFromEnvUnknownTypeReturnsError(t *testing.T) {
	_, err := brokerFromEnv(&environment.Environment{}, "not_a_real_broker", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown BrokerType")
}

func TestBrokerFromEnvRedisReturnsNonNilBroker(t *testing.T) {
	g := newTestInstance()
	g.env.CacheURI = "localhost:6399"

	broker, err := brokerFromEnv(g.env, msq.BrokerRedis, "")

	assert.NoError(t, err)
	assert.NotNil(t, broker)
}

func TestBrokerFromEnvKafkaReturnsNonNilBroker(t *testing.T) {
	broker, err := brokerFromEnv(&environment.Environment{}, msq.BrokerKafka, "")

	assert.NoError(t, err)
	assert.NotNil(t, broker)
}

func TestBrokerFromEnvSQSReturnsErrorWhenNoSession(t *testing.T) {
	_, err := brokerFromEnv(&environment.Environment{}, msq.BrokerSQS, "")

	assert.Error(t, err)
}

func TestBrokerFromEnvOCIReturnsErrorWhenNoCredentials(t *testing.T) {
	_, err := brokerFromEnv(&environment.Environment{}, msq.BrokerOCI, "")

	assert.Error(t, err)
}

//  AddMessaging with pre-built broker

func TestAddMessagingWithPreBuiltBrokerSetsConnectionAndReturnsBuilder(t *testing.T) {
	g := newTestInstance()
	mock := &mockMessaging{}

	b := g.AddMessaging(msq.Config{Broker: mock})

	assert.Same(t, g, b.(*gofiInstance))
	assert.NotNil(t, g.messagingConn)
}

func TestAddMessagingWithBrokerSetupSuccess(t *testing.T) {
	g := newTestInstance()
	mock := &mockBrokerWithSetup{setupErr: nil}

	b := g.AddMessaging(msq.Config{Broker: mock})

	assert.Same(t, g, b.(*gofiInstance))
	assert.True(t, mock.setupCalled)
	assert.NotNil(t, g.messagingConn)
}

func TestAddMessagingWithBrokerSetupFailureReturnsBuilderWithNilConn(t *testing.T) {
	g := newTestInstance()
	mock := &mockBrokerWithSetup{setupErr: fmt.Errorf("setup failed")}

	b := g.AddMessaging(msq.Config{Broker: mock})

	assert.Same(t, g, b.(*gofiInstance))
	assert.True(t, mock.setupCalled)
	assert.Nil(t, g.messagingConn)
}

func TestAddMessagingWithExplicitBrokerTypeAndCustomOnEvent(t *testing.T) {
	g := newTestInstance()
	mock := &mockMessaging{}
	eventCalled := false

	b := g.AddMessaging(msq.Config{
		Broker: mock,
		OnEvent: func(_ context.Context, _ msq.BrokerEvent) {
			eventCalled = true
		},
	})

	assert.Same(t, g, b.(*gofiInstance))
	assert.NotNil(t, g.messagingConn)
	assert.False(t, eventCalled) // OnEvent not triggered during build
}

//  Shutdown with telemetry

func TestShutdownWithNilTelemetryFieldsDoesNotPanic(t *testing.T) {
	g := newTestInstance()
	g.telemetry = &obs.Telemetry{} // zero value — all providers nil

	err := g.Shutdown(context.Background())

	assert.NoError(t, err)
}

//  Mocks

type mockMessaging struct{}

func (m *mockMessaging) NewProducer() (msq.Producer, error)             { return nil, nil }
func (m *mockMessaging) NewConsumer(cfg msq.ConsumeConfig) msq.Consumer { return nil }

type mockBrokerWithSetup struct {
	setupCalled bool
	setupErr    error
}

func (m *mockBrokerWithSetup) NewProducer() (msq.Producer, error)             { return nil, nil }
func (m *mockBrokerWithSetup) NewConsumer(cfg msq.ConsumeConfig) msq.Consumer { return nil }
func (m *mockBrokerWithSetup) Setup(ctx context.Context) error {
	m.setupCalled = true
	return m.setupErr
}

type mockHttpServer struct{}

func (m *mockHttpServer) ListenAndServe()                            {}
func (m *mockHttpServer) AddHandlers(handlers ...netx.RouterHandler) {}
func (m *mockHttpServer) Use(middleware ...netx.Middleware)          {}
func (m *mockHttpServer) UseAuth(auth netx.Middleware)               {}
