package msq_test

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/msq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_UnregisteredTypeNamesImport(t *testing.T) {
	_, err := msq.Open(context.Background(), msq.ProviderConfig{Type: "not_a_real_broker"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `import _ "github.com/joaoprofile/gofi-sdk-go/msq/provider/not_a_real_broker"`)
}

func TestOpen_RequiresType(t *testing.T) {
	_, err := msq.Open(context.Background(), msq.ProviderConfig{})
	assert.ErrorContains(t, err, "provider type is not set")
}

func TestOpen_UsesRegisteredOpener(t *testing.T) {
	const bt msq.BrokerType = "test-registry"
	var got msq.ProviderConfig
	msq.Register(bt, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
		got = cfg
		return nil, nil
	})
	_, err := msq.Open(context.Background(), msq.ProviderConfig{Type: bt, Exchange: "ex"})
	require.NoError(t, err)
	assert.Equal(t, "ex", got.Exchange)
}
