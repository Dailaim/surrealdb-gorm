package surrealdb_test

import (
	"testing"

	surrealdb "github.com/dailaim/surrealdb-gorm"
	"github.com/stretchr/testify/require"
)

// TestDefineStatements exercises the DEFINE helpers for PARAM, FUNCTION, and
// SEQUENCE (SurrealDB v3+) plus the generic Define escape hatch.
func TestDefineStatements(t *testing.T) {
	db := setupDB(t)
	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// PARAM
	require.NoError(t, m.DefineParam("endpointBase", `"https://example.com"`))
	t.Cleanup(func() { _ = m.RemoveParam("endpointBase") })
	var param []string
	require.NoError(t, db.Raw("RETURN $endpointBase").Scan(&param).Error)

	// FUNCTION
	require.NoError(t, m.DefineFunction("greet", "$name: string", "RETURN 'Hello ' + $name;"))
	t.Cleanup(func() { _ = m.RemoveFunction("greet") })
	var greeting []string
	require.NoError(t, db.Raw("RETURN fn::greet('World')").Scan(&greeting).Error)
	require.Contains(t, greeting, "Hello World")

	// SEQUENCE (v3+)
	require.NoError(t, m.DefineSequence("test_seq", surrealdb.SequenceOptions{Batch: 100, Start: 1}))
	t.Cleanup(func() { _ = m.RemoveSequence("test_seq") })

	// Generic escape hatch: define then remove a param via raw statements.
	require.NoError(t, m.Define(`PARAM $genericProbe VALUE 42`))
	require.NoError(t, m.Remove(`PARAM $genericProbe`))
}
