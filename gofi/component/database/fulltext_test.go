package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

func TestFullTextPolicyFromEnv(t *testing.T) {
	tests := []struct {
		policy, app string
		want        fts.Policy
		err         string
	}{
		{"", "", fts.Verify, ""},
		{"verify", "prod", fts.Verify, ""},
		{" Trust ", "prod", fts.Trust, ""},
		{"ensure", "dev", fts.Ensure, ""},
		{"ensure", "test", fts.Ensure, ""},
		{"ensure", "prod", 0, "not allowed in prod"},
		{"ensure", "stage", 0, "not allowed in stage"},
		{"create", "dev", 0, `DATABASE_FULLTEXT_POLICY="create"`},
	}
	for _, tt := range tests {
		got, err := fullTextPolicy(&environment.Environment{DatabaseFullTextPolicy: tt.policy, AppEnvironment: tt.app})
		if tt.err != "" {
			assert.ErrorContains(t, err, tt.err, "%q in %q", tt.policy, tt.app)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tt.want, got, "%q in %q", tt.policy, tt.app)
	}
}

var componentIndex = mustIndex()

func mustIndex() *fts.Index {
	idx, err := fts.NewIndex("products", fts.Spec{Name: "ftx_products", Columns: []fts.Column{{Name: "name"}}})
	if err != nil {
		panic(err)
	}
	return idx
}

// start runs New on the fake driver, whose dialect has no full-text search:
// any index checked at startup fails it.
func start(t *testing.T, policy string, registered []*fts.Index, opts ...Option) error {
	registerFakeDriver()
	rt := gofi.NewRuntime(&environment.Environment{
		DatabaseDriver: fakeDriverName, DatabaseHost: "primary", DatabaseFullTextPolicy: policy,
	})
	c := New(opts...)
	c.registered = func() []*fts.Index { return registered }
	err := c.Start(context.Background(), rt)
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return err
}

func TestStartChecksRegisteredIndexes(t *testing.T) {
	err := start(t, "", []*fts.Index{componentIndex})
	assert.ErrorIs(t, err, fts.ErrUnsupported)
	assert.ErrorContains(t, err, "full-text indexes")

	assert.NoError(t, start(t, "", nil), "nothing declared, nothing checked")
	assert.NoError(t, start(t, "trust", []*fts.Index{componentIndex}))
}

func TestWithFullTextReplacesRegistered(t *testing.T) {
	assert.NoError(t, start(t, "", []*fts.Index{componentIndex}, WithFullText()),
		"an empty WithFullText checks nothing")
	assert.ErrorIs(t, start(t, "", nil, WithFullText(componentIndex)), fts.ErrUnsupported)
}

func TestNewUsesFtsRegistered(t *testing.T) {
	assert.NotNil(t, New().registered)
}

func TestQueryLogRefusedInCloud(t *testing.T) {
	for env, ok := range map[string]bool{"": true, "dev": true, "test": true, "stage": false, "prod": false} {
		err := checkQueryLog(&environment.Environment{DatabaseLogQueries: true, AppEnvironment: env})
		assert.Equal(t, ok, err == nil, env)
	}
	assert.NoError(t, checkQueryLog(&environment.Environment{AppEnvironment: "prod"}), "off is always fine")
}
