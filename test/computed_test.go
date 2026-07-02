package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

type ComputedModel struct {
	models.BaseModel
	Name string `json:"name"`
	// Computed field: recomputed on every write from `name`.
	NameUpper string `json:"name_upper" gorm:"type:string;value:string::uppercase(name)"`
}

// TestComputedField verifies that a gorm:"value:<expr>" tag produces a
// SurrealDB computed field (DEFINE FIELD ... VALUE ...) via AutoMigrate.
func TestComputedField(t *testing.T) {
	db := setupDB(t)
	db.Exec("REMOVE TABLE IF EXISTS computed_models")
	require.NoError(t, db.AutoMigrate(&ComputedModel{}))
	t.Cleanup(func() { db.Exec("REMOVE TABLE IF EXISTS computed_models") })

	rec := ComputedModel{Name: "hello"}
	require.NoError(t, db.Create(&rec).Error)

	var got ComputedModel
	require.NoError(t, db.First(&got, "id = ?", rec.ID).Error)
	require.Equal(t, "hello", got.Name)
	require.Equal(t, "HELLO", got.NameUpper, "computed field should be string::uppercase(name)")
}
