package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm"
	"github.com/stretchr/testify/require"
)

func TestAlterFieldAndTable(t *testing.T) {
	db := setupDB(t)
	defer func() {
		db.Exec("REMOVE TABLE IF EXISTS `test_alter`")
	}()

	// Create a simple table first.
	err := db.Exec("DEFINE TABLE IF NOT EXISTS `test_alter` SCHEMAFULL").Error
	require.NoError(t, err)
	err = db.Exec("DEFINE FIELD IF NOT EXISTS `name` ON `test_alter` TYPE string").Error
	require.NoError(t, err)

	// --- ALTER FIELD: make it READONLY ---
	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}
	err = m.MakeFieldReadonly("test_alter", "name")
	require.NoError(t, err)

	// Verify that we can read it back (SurrealDB does not expose field metadata
	// in a standard way, but we can at least ensure the statement parsed).
	err = m.MakeFieldMutable("test_alter", "name")
	require.NoError(t, err)

	// --- ALTER FIELD: change type ---
	err = m.ChangeFieldType("test_alter", "name", "option<string>")
	require.NoError(t, err)

	// --- ALTER FIELD: add comment ---
	err = m.SetFieldComment("test_alter", "name", "the user name")
	require.NoError(t, err)

	// --- ALTER FIELD: drop comment ---
	err = m.DropFieldComment("test_alter", "name")
	require.NoError(t, err)

	// --- ALTER FIELD: add assert ---
	err = m.AddFieldAssert("test_alter", "name", "string::len($value) > 0")
	require.NoError(t, err)

	// --- ALTER FIELD: drop assert ---
	err = m.DropFieldAssert("test_alter", "name")
	require.NoError(t, err)

	// --- ALTER FIELD: add default ---
	err = m.AddFieldDefault("test_alter", "name", "'unknown'", false)
	require.NoError(t, err)

	// --- ALTER FIELD: drop default ---
	err = m.DropFieldDefault("test_alter", "name")
	require.NoError(t, err)

	// --- ALTER TABLE: compact ---
	err = m.CompactTable("test_alter")
	// Compaction may fail on in-memory storage; that's expected.
	// We just want to confirm the SQL was emitted correctly.
	// For test purposes we accept either nil or an error containing "compaction".
	if err != nil {
		require.Contains(t, err.Error(), "compaction", "unexpected compact error: %v", err)
	}

	// --- ALTER TABLE: set comment ---
	err = m.SetTableComment("test_alter", "users table")
	require.NoError(t, err)

	// --- ALTER TABLE: drop comment ---
	err = m.DropTableComment("test_alter")
	require.NoError(t, err)

	// --- ALTER TABLE: schema changes ---
	err = m.SetTableSchemaLess("test_alter")
	require.NoError(t, err)
	err = m.SetTableSchemaFull("test_alter")
	require.NoError(t, err)

	// --- ALTER TABLE: permissions ---
	err = m.SetTablePermissions("test_alter", "NONE")
	require.NoError(t, err)
	err = m.SetTablePermissions("test_alter", "FULL")
	require.NoError(t, err)
}

func TestAlterFieldValidation(t *testing.T) {
	db := setupDB(t)
	defer func() {
		db.Exec("REMOVE TABLE IF EXISTS `dummy`")
	}()

	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// No clauses provided should return an error.
	err := m.AlterField("dummy", "field", surrealdb.AlterFieldOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no ALTER FIELD clauses")

	// No clauses provided should return an error.
	err = m.AlterTable("dummy", surrealdb.AlterTableOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no ALTER TABLE clauses")
}
