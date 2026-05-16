package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm"
	"github.com/stretchr/testify/require"
)

func TestDefineAndRemoveAnalyzer(t *testing.T) {
	db := setupDB(t)

	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// Define a custom analyzer with edge n-grams for autocomplete.
	err := m.DefineEdgeNgramAnalyzer("test_autocomplete", 2, 10)
	require.NoError(t, err)

	// Define a snowball stemmer analyzer.
	err = m.DefineSnowballAnalyzer("test_english", "english")
	require.NoError(t, err)

	// Define a basic analyzer.
	err = m.DefineBasicAnalyzer("test_basic")
	require.NoError(t, err)

	// Cleanup
	err = m.RemoveAnalyzer("test_autocomplete")
	require.NoError(t, err)
	err = m.RemoveAnalyzer("test_english")
	require.NoError(t, err)
	err = m.RemoveAnalyzer("test_basic")
	require.NoError(t, err)
}

func TestDefineAnalyzerOverwrite(t *testing.T) {
	db := setupDB(t)

	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// Create an analyzer
	err := m.DefineAnalyzer("test_overwrite", surrealdb.AnalyzerOptions{
		Tokenizers: []string{"blank"},
		Filters:    []string{"lowercase"},
	})
	require.NoError(t, err)

	// Overwrite it with different options
	err = m.DefineAnalyzer("test_overwrite", surrealdb.AnalyzerOptions{
		Tokenizers: []string{"blank", "camel"},
		Filters:    []string{"lowercase", "ascii"},
		Overwrite:  true,
	})
	require.NoError(t, err)

	// Cleanup
	err = m.RemoveAnalyzer("test_overwrite")
	require.NoError(t, err)
}
