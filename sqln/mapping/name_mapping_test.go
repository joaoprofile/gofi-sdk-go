package mapping

import (
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type person struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
	City string `db:"city"`
}

func TestGetContentList_MapsByColumnName(t *testing.T) {
	rows := rowsFromValues(t, []string{"city", "id", "name"}, [][]driver.Value{{"Recife", int64(7), "Ana"}})
	list, err := GetContentList[person](rows)
	require.NoError(t, err)
	assert.Equal(t, []person{{ID: 7, Name: "Ana", City: "Recife"}}, list)
}

func TestGetContentList_IgnoresExtraColumns(t *testing.T) {
	// Oracle pagination wraps the query and adds an rn column.
	rows := rowsFromValues(t, []string{"ID", "NAME", "CITY", "RN"}, [][]driver.Value{{int64(1), "Bia", "Natal", int64(1)}})
	list, err := GetContentList[person](rows)
	require.NoError(t, err)
	assert.Equal(t, []person{{ID: 1, Name: "Bia", City: "Natal"}}, list)
}

func TestGetContentList_AliasedColumnsStayPositional(t *testing.T) {
	// Legacy queries whose aliases differ from the db tags keep positional mapping.
	rows := rowsFromValues(t, []string{"person_id", "full_name", "town"}, [][]driver.Value{{int64(3), "Caio", "Olinda"}})
	list, err := GetContentList[person](rows)
	require.NoError(t, err)
	assert.Equal(t, []person{{ID: 3, Name: "Caio", City: "Olinda"}}, list)
}

type withTwoIDs struct {
	ID    int64 `db:"id"`
	Owner struct {
		ID int64 `db:"id"`
	} `db:"owner"`
}

func TestGetContentList_AmbiguousTagsStayPositional(t *testing.T) {
	rows := rowsFromValues(t, []string{"id", "id"}, [][]driver.Value{{int64(1), int64(2)}})
	list, err := GetContentList[withTwoIDs](rows)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, int64(1), list[0].ID)
	assert.Equal(t, int64(2), list[0].Owner.ID)
}

func TestGetUnique(t *testing.T) {
	t.Run("row", func(t *testing.T) {
		rows := rowsFromValues(t, []string{"name", "city", "id"}, [][]driver.Value{{"Duda", "Recife", int64(9)}})
		p, err := GetUnique[person](rows)
		require.NoError(t, err)
		assert.Equal(t, &person{ID: 9, Name: "Duda", City: "Recife"}, p)
	})
	t.Run("no rows", func(t *testing.T) {
		empty, err := GetUnique[person](rowsFromValues(t, []string{"id", "name", "city"}, nil))
		require.NoError(t, err)
		assert.Nil(t, empty)
	})
}
