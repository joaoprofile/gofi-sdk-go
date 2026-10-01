package sqln

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/pagination"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPagedQuery_ZeroLimitUsesDefault(t *testing.T) {
	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")
	pr := pagination.NewPageRequest(0, 0, []Sort{NewSort("id", ASC)})

	var res *Page[scalarItem]
	var err error
	assert.NotPanics(t, func() {
		res, err = Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr).ExecutePagedQuery(db)
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(DefaultLimit), res.Size)
}

func TestPagedQuery_NumberOfElementsIsContentLength(t *testing.T) {
	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")
	pr := pagination.NewPageRequest(0, 10, []Sort{NewSort("id", ASC)})

	res, err := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr).ExecutePagedQuery(db)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(res.Content)), res.NumberOfElements)
}
