package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	assert.Equal(t, uint16(0), DefaultPage)
	assert.Equal(t, uint16(15), DefaultLimit)
	assert.Equal(t, "id", DefaultSortField)
	assert.Equal(t, string(ASC), DefaultSortDirection)
}

// NewPageRequest

func TestNewPageRequest_SetsFields(t *testing.T) {
	order := []Sort{NewSort("name", ASC)}
	pr := NewPageRequest(2, 20, order)
	require.NotNil(t, pr)
	assert.Equal(t, uint16(2), pr.Page)
	assert.Equal(t, uint16(20), pr.Limit)
	assert.Equal(t, order, pr.Order)
}

func TestNewPageRequest_EmptyOrder(t *testing.T) {
	pr := NewPageRequest(0, 15, nil)
	require.NotNil(t, pr)
	assert.Nil(t, pr.Order)
}

// PageRequest.GetOrder

func TestGetOrder_SingleField(t *testing.T) {
	pr := NewPageRequest(0, 15, []Sort{NewSort("name", ASC)})
	assert.Equal(t, "name ASC", pr.GetOrder())
}

func TestGetOrder_MultipleFields(t *testing.T) {
	pr := NewPageRequest(0, 15, []Sort{
		NewSort("name", ASC),
		NewSort("created_at", DESC),
	})
	assert.Equal(t, "name ASC, created_at DESC", pr.GetOrder())
}

func TestGetOrder_EmptyOrder_ReturnsEmptyString(t *testing.T) {
	pr := NewPageRequest(0, 15, nil)
	assert.Equal(t, "", pr.GetOrder())
}

// NewPageRequestFromParams — zero values fall back to defaults

func TestNewPageRequestFromParams_ZeroValues_UsesDefaults(t *testing.T) {
	pr := NewPageRequestFromParams(0, 0, "", "")
	require.NotNil(t, pr)
	assert.Equal(t, DefaultPage, pr.Page)
	assert.Equal(t, DefaultLimit, pr.Limit)
	require.Len(t, pr.Order, 1)
	assert.Equal(t, DefaultSortField, pr.Order[0].Field)
	assert.Equal(t, SortDirection(DefaultSortDirection), pr.Order[0].Direction)
}

// NewPageRequestFromParams — explicit values override defaults

func TestNewPageRequestFromParams_CustomPage(t *testing.T) {
	pr := NewPageRequestFromParams(3, 0, "", "")
	assert.Equal(t, uint16(3), pr.Page)
	assert.Equal(t, DefaultLimit, pr.Limit)
}

func TestNewPageRequestFromParams_CustomLimit(t *testing.T) {
	pr := NewPageRequestFromParams(0, 50, "", "")
	assert.Equal(t, DefaultPage, pr.Page)
	assert.Equal(t, uint16(50), pr.Limit)
}

func TestNewPageRequestFromParams_CustomSortField(t *testing.T) {
	pr := NewPageRequestFromParams(0, 0, "price", "")
	assert.Equal(t, "price", pr.Order[0].Field)
}

func TestNewPageRequestFromParams_CustomSortDirection(t *testing.T) {
	pr := NewPageRequestFromParams(0, 0, "", string(DESC))
	assert.Equal(t, DESC, pr.Order[0].Direction)
}

func TestNewPageRequestFromParams_AllCustom(t *testing.T) {
	pr := NewPageRequestFromParams(2, 30, "created_at", string(DESC))
	assert.Equal(t, uint16(2), pr.Page)
	assert.Equal(t, uint16(30), pr.Limit)
	assert.Equal(t, "created_at", pr.Order[0].Field)
	assert.Equal(t, DESC, pr.Order[0].Direction)
}
