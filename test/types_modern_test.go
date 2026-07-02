package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

type ModernTypes struct {
	models.BaseModel
	// Literal/enum union type, enforced by the schema.
	Status string `json:"status" gorm:"type:'active'|'inactive'|'pending'"`
}

// TestModernSchemaTypes verifies that modern SurrealDB schema types are usable
// through the gorm `type:` tag, which DataTypeOf honors verbatim. Here: a
// literal (enum) union type that the schema enforces.
//
// Note: `set<T>` is not exercised because SurrealDB does not auto-coerce a plain
// array to a set on assignment; and the v3 `file` type has no SDK model yet.
func TestModernSchemaTypes(t *testing.T) {
	db := setupDB(t)
	db.Exec("REMOVE TABLE IF EXISTS modern_types")
	require.NoError(t, db.AutoMigrate(&ModernTypes{}))
	t.Cleanup(func() { db.Exec("REMOVE TABLE IF EXISTS modern_types") })

	rec := ModernTypes{Status: "active"}
	require.NoError(t, db.Create(&rec).Error)
	require.NotNil(t, rec.ID)

	var got ModernTypes
	require.NoError(t, db.First(&got, "id = ?", rec.ID).Error)
	require.Equal(t, "active", got.Status)

	// A value outside the literal union must be rejected by the schema.
	bad := ModernTypes{Status: "nonsense"}
	require.Error(t, db.Create(&bad).Error, "literal type must reject an out-of-set value")
}
