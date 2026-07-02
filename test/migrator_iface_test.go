package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

type MigModel struct {
	models.BaseModel
	Email string `json:"email" gorm:"uniqueIndex:idx_mig_email"`
	Nick  string `json:"nick"`
}

// TestMigratorInterface exercises the GORM Migrator methods that previously fell
// back to information_schema SQL (rejected by SurrealDB): HasColumn, HasIndex,
// RenameColumn.
func TestMigratorInterface(t *testing.T) {
	db := setupDB(t)
	db.Exec("REMOVE TABLE IF EXISTS mig_models")
	require.NoError(t, db.AutoMigrate(&MigModel{}))
	t.Cleanup(func() { db.Exec("REMOVE TABLE IF EXISTS mig_models") })

	m := db.Migrator()

	// HasColumn accepts both struct field name and DB column name.
	require.True(t, m.HasColumn(&MigModel{}, "Nick"))
	require.True(t, m.HasColumn(&MigModel{}, "nick"))
	require.False(t, m.HasColumn(&MigModel{}, "does_not_exist"))

	// HasIndex finds the unique index created from the struct tag.
	require.True(t, m.HasIndex(&MigModel{}, "idx_mig_email"))
	require.False(t, m.HasIndex(&MigModel{}, "no_such_index"))

	// RenameColumn renames nick -> nickname.
	require.NoError(t, m.RenameColumn(&MigModel{}, "nick", "nickname"))
	require.True(t, m.HasColumn(&MigModel{}, "nickname"))
	require.False(t, m.HasColumn(&MigModel{}, "nick"))
}
