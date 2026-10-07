package groveset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/grove"
)

type row struct {
	grove.BaseModel `grove:"table:rows"`
	ID              string  `grove:"id,pk"`
	Name            string  `grove:"name,notnull"`
	Note            *string `grove:"note"`
	Total           int64   `grove:"total,scanonly"`
	Version         int64   `grove:"version,notnull"`
	Hidden          string  `grove:"-"`
}

func TestColumnsMatchTheModelUpdate(t *testing.T) {
	cols, err := Columns(&row{ID: "r1", Name: "n", Version: 7, Total: 3, Hidden: "h"}, "version")
	require.NoError(t, err)
	require.Len(t, cols, 2)
	assert.Equal(t, Column{Name: "name", Value: "n"}, cols[0])
	assert.Equal(t, "note", cols[1].Name)
	assert.Nil(t, cols[1].Value, "a nil pointer is set as it is")

	cols, err = Columns(&row{})
	require.NoError(t, err)
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	assert.Equal(t, []string{"name", "note", "version"}, names, "no primary key, scanonly or skipped field")
}

func TestColumnsRefusesANilModel(t *testing.T) {
	_, err := Columns((*row)(nil))
	require.Error(t, err)
}
