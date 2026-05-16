package surrealdb_test

import (
	"reflect"
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

// schemaFullModel opts-in to SCHEMAFULL
type schemaFullModel struct {
	models.BaseModel
	Name string `json:"name"`
}

func (schemaFullModel) SchemaFull() bool { return true }

// schemalessModel does not opt-in → defaults to SCHEMALESS
type schemalessModel struct {
	models.BaseModel
	Name string `json:"name"`
}

func TestSchemaFullDetection(t *testing.T) {
	schemaFullType := reflect.TypeOf((*models.SchemaFull)(nil)).Elem()

	mtFull := reflect.TypeOf(schemaFullModel{})
	require.True(t, mtFull.Implements(schemaFullType) || reflect.PointerTo(mtFull).Implements(schemaFullType),
		"schemaFullModel should implement SchemaFull")

	mtLess := reflect.TypeOf(schemalessModel{})
	require.False(t, mtLess.Implements(schemaFullType) || reflect.PointerTo(mtLess).Implements(schemaFullType),
		"schemalessModel should NOT implement SchemaFull")
}
