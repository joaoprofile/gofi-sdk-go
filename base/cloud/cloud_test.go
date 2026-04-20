package cloud

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/joaoprofile/gofi/base/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// resetAll resets the cloud singleton, the environment singleton, and the
// provider registry between tests so each test starts with a clean slate.
func resetAll(t *testing.T) {
	t.Helper()
	ResetForTesting()
	resetRegistryForTesting()
	environment.ResetForTesting()
	t.Cleanup(func() {
		ResetForTesting()
		resetRegistryForTesting()
		environment.ResetForTesting()
	})
}

// stubProvider is a controllable in-memory Provider used in adapter tests.
type stubProvider struct {
	bootstrapErr error
	session      any
}

func (s *stubProvider) Bootstrap() error { return s.bootstrapErr }
func (s *stubProvider) GetSession() any  { return s.session }

// ─────────────────────────────────────────────────────────────────────────────
// Factory — RegisterProvider
// ─────────────────────────────────────────────────────────────────────────────

func TestRegisterProvider_DuplicatePanics(t *testing.T) {
	// CLOUD_AWS is already registered via init(); a second registration must panic.
	assert.Panics(t, func() {
		RegisterProvider(environment.CLOUD_AWS, func(cfg environment.CloudConfig) Provider {
			return NewAWS(cfg)
		})
	})
}

func TestRegisterProvider_NewNameSucceeds(t *testing.T) {
	resetAll(t)
	const custom environment.CloudProvider = "custom"
	assert.NotPanics(t, func() {
		RegisterProvider(custom, func(cfg environment.CloudConfig) Provider {
			return &stubProvider{}
		})
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Factory — newProvider
// ─────────────────────────────────────────────────────────────────────────────

func TestNewProvider_AWS(t *testing.T) {
	p, err := newProvider(environment.CloudConfig{Provider: environment.CLOUD_AWS})
	require.NoError(t, err)
	_, ok := p.(*AWS)
	assert.True(t, ok, "expected *AWS")
}

func TestNewProvider_GCP(t *testing.T) {
	p, err := newProvider(environment.CloudConfig{Provider: environment.CLOUD_GCP})
	require.NoError(t, err)
	_, ok := p.(*GCP)
	assert.True(t, ok, "expected *GCP")
}

func TestNewProvider_OCI(t *testing.T) {
	p, err := newProvider(environment.CloudConfig{Provider: environment.CLOUD_OCI})
	require.NoError(t, err)
	_, ok := p.(*OCI)
	assert.True(t, ok, "expected *OCI")
}

func TestNewProvider_NoneReturnsErrNoProvider(t *testing.T) {
	_, err := newProvider(environment.CloudConfig{Provider: environment.CLOUD_NONE})
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoProvider)
}

func TestNewProvider_EmptyReturnsErrNoProvider(t *testing.T) {
	_, err := newProvider(environment.CloudConfig{Provider: ""})
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoProvider)
}

func TestNewProvider_UnknownReturnsUnsupportedError(t *testing.T) {
	_, err := newProvider(environment.CloudConfig{Provider: "azure"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider")
	assert.Contains(t, err.Error(), "azure")
}

func TestNewProvider_CustomRegisteredProvider(t *testing.T) {
	resetAll(t)
	const custom environment.CloudProvider = "custom"
	stub := &stubProvider{session: "custom-session"}
	RegisterProvider(custom, func(_ environment.CloudConfig) Provider { return stub })

	p, err := newProvider(environment.CloudConfig{Provider: custom})
	require.NoError(t, err)
	assert.Equal(t, stub, p)
}

// ─────────────────────────────────────────────────────────────────────────────
// AWS provider
// ─────────────────────────────────────────────────────────────────────────────

func TestAWS_ImplementsProvider(t *testing.T) {
	var _ Provider = NewAWS(environment.CloudConfig{})
}

func TestAWS_Bootstrap_Success(t *testing.T) {
	a := NewAWS(environment.CloudConfig{
		Provider: environment.CLOUD_AWS,
		Region:   "us-east-1",
		Token:    "fake-token",
		Secret:   "fake-secret",
	})
	assert.NoError(t, a.Bootstrap())
}

func TestAWS_GetSession_BeforeBootstrap_ReturnsNil(t *testing.T) {
	a := NewAWS(environment.CloudConfig{Provider: environment.CLOUD_AWS})
	assert.Nil(t, a.GetSession())
}

func TestAWS_GetSession_AfterBootstrap_ReturnsTypedSession(t *testing.T) {
	a := NewAWS(environment.CloudConfig{
		Provider: environment.CLOUD_AWS,
		Region:   "us-east-1",
		Token:    "fake-token",
		Secret:   "fake-secret",
	})
	require.NoError(t, a.Bootstrap())

	raw := a.GetSession()
	require.NotNil(t, raw)
	_, ok := raw.(*session.Session)
	assert.True(t, ok, "GetSession must return *session.Session")
}

// ─────────────────────────────────────────────────────────────────────────────
// GCP provider
// ─────────────────────────────────────────────────────────────────────────────

func TestGCP_ImplementsProvider(t *testing.T) {
	var _ Provider = NewGCP(environment.CloudConfig{})
}

func TestGCP_Bootstrap_ReturnsNil(t *testing.T) {
	assert.NoError(t, NewGCP(environment.CloudConfig{}).Bootstrap())
}

func TestGCP_GetSession_ReturnsNil(t *testing.T) {
	assert.Nil(t, NewGCP(environment.CloudConfig{}).GetSession())
}

// ─────────────────────────────────────────────────────────────────────────────
// OCI provider
// ─────────────────────────────────────────────────────────────────────────────

func TestOCI_ImplementsProvider(t *testing.T) {
	var _ Provider = NewOCI(environment.CloudConfig{})
}

func TestOCI_Bootstrap_ReturnsNil(t *testing.T) {
	assert.NoError(t, NewOCI(environment.CloudConfig{}).Bootstrap())
}

func TestOCI_GetSession_ReturnsNil(t *testing.T) {
	assert.Nil(t, NewOCI(environment.CloudConfig{}).GetSession())
}

// ─────────────────────────────────────────────────────────────────────────────
// TypedAdapter
// ─────────────────────────────────────────────────────────────────────────────

func TestTypedAdapter_NilProvider_ReturnsError(t *testing.T) {
	adapter := NewTypedAdapter[*session.Session](nil)
	_, err := adapter.Session()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no provider")
}

func TestTypedAdapter_NilSession_ReturnsError(t *testing.T) {
	stub := &stubProvider{session: nil}
	adapter := NewTypedAdapter[*session.Session](stub)
	_, err := adapter.Session()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialised")
}

func TestTypedAdapter_TypeMismatch_ReturnsError(t *testing.T) {
	stub := &stubProvider{session: "this-is-a-string-not-a-session"}
	adapter := NewTypedAdapter[*session.Session](stub)
	_, err := adapter.Session()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type mismatch")
}

func TestTypedAdapter_Success(t *testing.T) {
	a := NewAWS(environment.CloudConfig{
		Region: "us-east-1",
		Token:  "t",
		Secret: "s",
	})
	require.NoError(t, a.Bootstrap())

	adapter := NewTypedAdapter[*session.Session](a)
	sess, err := adapter.Session()
	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestTypedAdapter_ImplementsAdapter(t *testing.T) {
	var _ Adapter[*session.Session] = NewTypedAdapter[*session.Session](nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// SessionAs
// ─────────────────────────────────────────────────────────────────────────────

func TestSessionAs_Success(t *testing.T) {
	cfg := environment.CloudConfig{
		Provider: environment.CLOUD_AWS,
		Region:   "us-east-1",
		Token:    "t",
		Secret:   "s",
	}
	c, err := newCloud(cfg)
	require.NoError(t, err)

	sess, err := SessionAs[*session.Session](c)
	require.NoError(t, err)
	assert.NotNil(t, sess)
}

func TestSessionAs_TypeMismatch_ReturnsError(t *testing.T) {
	cfg := environment.CloudConfig{
		Provider: environment.CLOUD_AWS,
		Region:   "us-east-1",
		Token:    "t",
		Secret:   "s",
	}
	c, err := newCloud(cfg)
	require.NoError(t, err)

	// string is not the correct type for an AWS session.
	_, err = SessionAs[string](c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type mismatch")
}

// ─────────────────────────────────────────────────────────────────────────────
// newCloud
// ─────────────────────────────────────────────────────────────────────────────

func TestNewCloud_AWS_Success(t *testing.T) {
	c, err := newCloud(environment.CloudConfig{
		Provider: environment.CLOUD_AWS,
		Region:   "us-east-1",
		Token:    "t",
		Secret:   "s",
	})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewCloud_GCP_Success(t *testing.T) {
	c, err := newCloud(environment.CloudConfig{Provider: environment.CLOUD_GCP})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewCloud_OCI_Success(t *testing.T) {
	c, err := newCloud(environment.CloudConfig{Provider: environment.CLOUD_OCI})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewCloud_UnknownProvider_ReturnsError(t *testing.T) {
	c, err := newCloud(environment.CloudConfig{Provider: "unknown"})
	assert.Nil(t, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider")
}

func TestNewCloud_NoProvider_ReturnsErrNoProvider(t *testing.T) {
	_, err := newCloud(environment.CloudConfig{Provider: environment.CLOUD_NONE})
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoProvider)
}

func TestNewCloud_BootstrapError_WrapsError(t *testing.T) {
	resetAll(t)
	const failing environment.CloudProvider = "failing"
	RegisterProvider(failing, func(_ environment.CloudConfig) Provider {
		return &stubProvider{bootstrapErr: errors.New("dial timeout")}
	})

	c, err := newCloud(environment.CloudConfig{Provider: failing})
	assert.Nil(t, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bootstrap failed")
	assert.Contains(t, err.Error(), "dial timeout")
}

// ─────────────────────────────────────────────────────────────────────────────
// Cloud.GetSession
// ─────────────────────────────────────────────────────────────────────────────

func TestCloud_GetSession_DelegatesToProvider(t *testing.T) {
	c, err := newCloud(environment.CloudConfig{
		Provider: environment.CLOUD_AWS,
		Region:   "us-east-1",
		Token:    "t",
		Secret:   "s",
	})
	require.NoError(t, err)

	raw := c.GetSession()
	require.NotNil(t, raw)
	_, ok := raw.(*session.Session)
	assert.True(t, ok)
}

// ─────────────────────────────────────────────────────────────────────────────
// Package-level GetSession
// ─────────────────────────────────────────────────────────────────────────────

func TestGetSession_NilWhenNotInitialised(t *testing.T) {
	ResetForTesting()
	t.Cleanup(ResetForTesting)
	assert.Nil(t, GetSession())
}

func TestGetSession_ReturnsSessionAfterInjection(t *testing.T) {
	ResetForTesting()
	t.Cleanup(ResetForTesting)

	c, err := newCloud(environment.CloudConfig{
		Provider: environment.CLOUD_AWS,
		Region:   "us-east-1",
		Token:    "t",
		Secret:   "s",
	})
	require.NoError(t, err)
	instance = c // inject directly into the singleton

	assert.NotNil(t, GetSession())
}

// ─────────────────────────────────────────────────────────────────────────────
// Instance singleton
// ─────────────────────────────────────────────────────────────────────────────

func TestInstance_UnknownProvider_ReturnsError(t *testing.T) {
	resetAll(t)
	t.Setenv("CLOUD_PROVIDER", "azure")

	c, err := Instance()
	assert.Nil(t, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider")
}

func TestInstance_NoProvider_ReturnsErrNoProvider(t *testing.T) {
	resetAll(t)
	t.Setenv("CLOUD_PROVIDER", "none")

	c, err := Instance()
	assert.Nil(t, c)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoProvider)
}

func TestInstance_AWS_Success(t *testing.T) {
	resetAll(t)
	t.Setenv("CLOUD_PROVIDER", "aws")
	t.Setenv("CLOUD_REGION", "us-east-1")
	t.Setenv("CLOUD_TOKEN", "fake-token")
	t.Setenv("CLOUD_SECRET", "fake-secret")

	c, err := Instance()
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestInstance_IsSingleton(t *testing.T) {
	resetAll(t)
	t.Setenv("CLOUD_PROVIDER", "aws")
	t.Setenv("CLOUD_REGION", "us-east-1")
	t.Setenv("CLOUD_TOKEN", "fake-token")
	t.Setenv("CLOUD_SECRET", "fake-secret")

	c1, err1 := Instance()
	c2, err2 := Instance()
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Same(t, c1, c2, "Instance must return the same pointer on every call")
}

func TestInstance_ErrorIsSticky(t *testing.T) {
	resetAll(t)
	t.Setenv("CLOUD_PROVIDER", "bad-provider")

	_, err1 := Instance()
	_, err2 := Instance()
	require.Error(t, err1)
	require.Error(t, err2)
	assert.Equal(t, err1, err2, "same error must be returned on every call after failure")
}

func TestResetForTesting_AllowsReinit(t *testing.T) {
	resetAll(t)
	t.Setenv("CLOUD_PROVIDER", "bad-provider")
	_, err := Instance()
	require.Error(t, err)

	// Reset and reinitialise with a valid provider.
	ResetForTesting()
	environment.ResetForTesting()
	t.Setenv("CLOUD_PROVIDER", "aws")
	t.Setenv("CLOUD_REGION", "us-east-1")
	t.Setenv("CLOUD_TOKEN", "tok")
	t.Setenv("CLOUD_SECRET", "sec")

	c, err2 := Instance()
	require.NoError(t, err2)
	assert.NotNil(t, c)
}
