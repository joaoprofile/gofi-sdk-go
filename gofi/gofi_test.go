package gofi

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//  Compile-time interface compliance
// These lines fail at compile time if gofiInstance stops satisfying its contracts.

var _ Builder = (*gofiInstance)(nil)
var _ Service = (*gofiInstance)(nil)

// newTestInstance creates a gofiInstance with a preset environment, so Build
// does not read the process environment. Safe for unit tests.
func newTestInstance() *gofiInstance {
	logging.NewLogger("test") // required before any method that logs
	return &gofiInstance{
		serviceName: "test-service",
		env:         &environment.Environment{},
	}
}

//  Fakes

// journal records, in order, what the fake components did.
type journal struct {
	mu     sync.Mutex
	events []string
}

func (j *journal) add(e string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = append(j.events, e)
}

func (j *journal) list() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.events...)
}

type fakeComponent struct {
	name     string
	stage    Stage
	startErr error
	log      *journal
}

func (f *fakeComponent) Name() string { return f.name }
func (f *fakeComponent) Stage() Stage { return f.stage }
func (f *fakeComponent) Start(_ context.Context, rt *Runtime) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.log.add("start " + f.name)
	rt.OnClose(func(context.Context) error {
		f.log.add("close " + f.name)
		return nil
	})
	return nil
}

// fakeRunner serves until Stop, or returns runErr right away when set.
type fakeRunner struct {
	fakeComponent
	runErr  error
	stopped chan struct{}
	once    sync.Once
	running chan struct{}
}

func newFakeRunner(name string, log *journal) *fakeRunner {
	return &fakeRunner{
		fakeComponent: fakeComponent{name: name, stage: StageServer, log: log},
		stopped:       make(chan struct{}),
		running:       make(chan struct{}),
	}
}

func (r *fakeRunner) Run() error {
	close(r.running)
	if r.runErr != nil {
		return r.runErr
	}
	<-r.stopped
	r.log.add("run returned " + r.name)
	return nil
}

func (r *fakeRunner) Stop(context.Context) error {
	r.once.Do(func() { close(r.stopped) })
	return nil
}

func mustBuild(t *testing.T, g *gofiInstance) Service {
	t.Helper()
	svc, err := g.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return svc
}

//  New / With

func TestNewPublicFunctionReturnsBuilder(t *testing.T) {
	b := New("test-via-new")
	assert.NotNil(t, b)
	assert.Implements(t, (*Builder)(nil), b)
}

func TestNewHasNoSideEffects(t *testing.T) {
	b := New("no-side-effects").(*gofiInstance)
	assert.Nil(t, b.env, "environment is loaded by Build")
	assert.Nil(t, b.rt)
}

func TestNewKeepsDefaultTransportTLSVerification(t *testing.T) {
	New("test-tls")
	tlsCfg := http.DefaultTransport.(*http.Transport).TLSClientConfig
	assert.False(t, tlsCfg != nil && tlsCfg.InsecureSkipVerify, "New must not disable TLS verification globally")
}

func TestWithReturnsSameInstance(t *testing.T) {
	g := newTestInstance()
	b := g.With(&fakeComponent{name: "a", log: &journal{}})
	assert.Same(t, g, b.(*gofiInstance))
}

//  Build

func TestBuildReturnsSameUnderlyingInstance(t *testing.T) {
	g := newTestInstance()
	svc := mustBuild(t, g)
	assert.Same(t, g, svc.(*gofiInstance))
}

func TestBuildStartsByStageAndShutdownClosesInReverse(t *testing.T) {
	log := &journal{}
	g := newTestInstance()
	g.With(
		&fakeComponent{name: "messaging", stage: StageMessaging, log: log},
		&fakeComponent{name: "database", stage: StageDatabase, log: log},
		&fakeComponent{name: "observability", stage: StageObservability, log: log},
		&fakeComponent{name: "database-2", stage: StageDatabase, log: log},
	)
	mustBuild(t, g)
	require.NoError(t, g.Shutdown(context.Background()))

	assert.Equal(t, []string{
		"start observability", "start database", "start database-2", "start messaging",
		"close messaging", "close database-2", "close database", "close observability",
	}, log.list())
}

func TestBuildReturnsMisuseBeforeAnyStart(t *testing.T) {
	log := &journal{}
	g := newTestInstance()
	g.With(&fakeComponent{name: "database", stage: StageDatabase, log: log}, nil)

	svc, err := g.Build()
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, errNilComponent)
	assert.Empty(t, log.list(), "no component starts when the chain is misused")
}

func TestBuildFailureRollsBackStartedComponents(t *testing.T) {
	log := &journal{}
	g := newTestInstance()
	g.With(
		&fakeComponent{name: "observability", stage: StageObservability, log: log},
		&fakeComponent{name: "messaging", stage: StageMessaging, startErr: errors.New("setup failed"), log: log},
	)

	svc, err := g.Build()
	assert.Nil(t, svc)
	assert.ErrorContains(t, err, "messaging: setup failed")
	assert.Equal(t, []string{"start observability", "close observability"}, log.list())
}

// checkingComponent reports fixed insecure transports.
type checkingComponent struct {
	fakeComponent
	found []InsecureTransport
}

func (c *checkingComponent) InsecureTransports(*environment.Environment) []InsecureTransport {
	return c.found
}

func newInsecureInstance(appEnv, allow string, log *journal) *gofiInstance {
	g := newTestInstance()
	g.env = &environment.Environment{
		AppEnvironment: appEnv, AllowInsecureTransport: allow,
		BucketProvider: "s3", BucketEndpoint: "http://minio:9000",
	}
	g.With(&checkingComponent{
		fakeComponent: fakeComponent{name: "database", stage: StageDatabase, log: log},
		found:         []InsecureTransport{{Resource: "database", Setting: "DATABASE_SSL_MODE", Detail: "sslmode disable"}},
	})
	return g
}

func TestBuildRefusesInsecureTransportInProdAndStage(t *testing.T) {
	for _, appEnv := range []string{"prod", "stage"} {
		log := &journal{}
		svc, err := newInsecureInstance(appEnv, "", log).Build()
		assert.Nil(t, svc)
		require.ErrorIs(t, err, core.ErrInsecureTransport, appEnv)
		assert.ErrorContains(t, err, "database: sslmode disable")
		assert.ErrorContains(t, err, "bucket: endpoint minio:9000 uses http", "every refusal is joined")
		assert.Empty(t, log.list(), "no component starts")
	}
}

func TestBuildAllowsInsecureTransportWithTheHatch(t *testing.T) {
	log := &journal{}
	_, err := newInsecureInstance("prod", "database", log).Build()
	assert.ErrorContains(t, err, "bucket:", "only the listed resources are allowed")
	assert.NotContains(t, err.Error(), "database:")

	svc, err := newInsecureInstance("prod", "database,bucket", log).Build()
	require.NoError(t, err)
	require.NoError(t, svc.Shutdown(context.Background()))
	assert.Equal(t, []string{"start database", "close database"}, log.list())
}

func TestBuildDoesNotCheckTransportInDev(t *testing.T) {
	for _, appEnv := range []string{"", "dev", "test"} {
		_, err := newInsecureInstance(appEnv, "", &journal{}).Build()
		assert.NoError(t, err, appEnv)
	}
}

func TestWithRedactKeys(t *testing.T) {
	g := New("svc", WithRedactKeys("card"), WithRedactKeys("cpf")).(*gofiInstance)
	assert.Equal(t, []string{"card", "cpf"}, g.redactKeys)
	g.env = &environment.Environment{}
	mustBuild(t, g)
}

func TestBuildTwiceIsRejected(t *testing.T) {
	g := newTestInstance()
	mustBuild(t, g)
	_, err := g.Build()
	assert.ErrorIs(t, err, errAlreadyBuilt)
}

func TestBuildOverridesAppNameWithServiceName(t *testing.T) {
	g := newTestInstance()
	env := mustBuild(t, g).Environment()
	assert.Equal(t, "test-service", env.AppName)
}

//  Service: accessors and Shutdown

func TestEnvironmentAccessorReturnsSamePointer(t *testing.T) {
	g := newTestInstance()
	env := &environment.Environment{AppName: "my-service"}
	g.env = env
	assert.Same(t, env, mustBuild(t, g).Environment())
}

func TestShutdownBeforeBuildDoesNotPanic(t *testing.T) {
	assert.NoError(t, newTestInstance().Shutdown(context.Background()))
}

func TestShutdownWithoutListenAndServeClosesResources(t *testing.T) {
	log := &journal{}
	g := newTestInstance()
	g.With(&fakeComponent{name: "cache", stage: StageCache, log: log})
	mustBuild(t, g)

	require.NoError(t, g.Shutdown(context.Background()))
	assert.Equal(t, []string{"start cache", "close cache"}, log.list())
	assert.NoError(t, g.ListenAndServe(), "ListenAndServe after Shutdown returns at once")
}

//  ListenAndServe

func TestListenAndServeWithoutRunnersStopsOnShutdown(t *testing.T) {
	g := newTestInstance()
	mustBuild(t, g)
	done := make(chan error, 1)
	go func() { done <- g.ListenAndServe() }()

	require.Eventually(t, func() bool {
		g.lifeMu.Lock()
		defer g.lifeMu.Unlock()
		return g.stop != nil
	}, time.Second, 5*time.Millisecond)
	assert.NoError(t, g.Shutdown(context.Background()))

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
}

func TestShutdownStopsRunnersThenClosesResources(t *testing.T) {
	log := &journal{}
	g := newTestInstance()
	runner := newFakeRunner("http", log)
	g.With(runner, &fakeComponent{name: "database", stage: StageDatabase, log: log})
	mustBuild(t, g)

	done := make(chan error, 1)
	go func() { done <- g.ListenAndServe() }()
	<-runner.running
	require.NoError(t, g.Shutdown(context.Background()))
	require.NoError(t, <-done)

	assert.Equal(t, []string{
		"start database", "start http", "run returned http", "close http", "close database",
	}, log.list())
}

func TestListenAndServeReturnsRunnerErrorAndStopsTheOthers(t *testing.T) {
	log := &journal{}
	g := newTestInstance()
	failing := newFakeRunner("grpc", log)
	failing.runErr = errors.New("listen failed")
	other := newFakeRunner("http", log)
	g.With(failing, other)
	mustBuild(t, g)

	err := g.ListenAndServe()
	assert.ErrorContains(t, err, "listen failed")
	assert.Contains(t, log.list(), "run returned http", "the other runner must be stopped")
}

//  Runtime

func TestRuntimeSharedOpensOnce(t *testing.T) {
	rt := NewRuntime(&environment.Environment{})
	calls := 0
	open := func() (any, error) { calls++; return "client", nil }

	v1, err := rt.Shared("k", open)
	require.NoError(t, err)
	v2, err := rt.Shared("k", open)
	require.NoError(t, err)
	assert.Equal(t, "client", v1)
	assert.Equal(t, v1, v2)
	assert.Equal(t, 1, calls)
}

func TestRuntimeSharedDoesNotStoreFailures(t *testing.T) {
	rt := NewRuntime(&environment.Environment{})
	_, err := rt.Shared("k", func() (any, error) { return nil, errors.New("down") })
	assert.ErrorContains(t, err, "down")

	v, err := rt.Shared("k", func() (any, error) { return "up", nil })
	require.NoError(t, err)
	assert.Equal(t, "up", v)
}

func TestRuntimeHealthChecksAndCloseErrors(t *testing.T) {
	rt := NewRuntime(&environment.Environment{})
	rt.AddHealthCheck("db", func(context.Context) error { return nil })
	require.Len(t, rt.HealthChecks(), 1)
	assert.Equal(t, "db", rt.HealthChecks()[0].Name)

	rt.OnClose(func(context.Context) error { return errors.New("close failed") })
	assert.ErrorContains(t, rt.Close(context.Background()), "close failed")
	assert.NoError(t, rt.Close(context.Background()), "closers run once")
}
