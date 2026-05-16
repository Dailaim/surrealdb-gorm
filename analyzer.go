package surrealdb

import (
	"fmt"
	"strings"
)

// AnalyzerOptions describes the configuration for a SurrealDB full-text analyzer.
// See: https://surrealdb.com/docs/surrealql/statements/define/analyzer
type AnalyzerOptions struct {
	Function   string   // Optional user-defined function fn::xxx to pre-process input
	Tokenizers []string // e.g. "blank", "camel", "class", "punct"
	Filters    []string // e.g. "lowercase", "snowball(english)", "edgengram(1,3)"
	Comment    string
	Overwrite  bool
}

// DefineAnalyzer creates or replaces a SurrealDB full-text analyzer.
func (m Migrator) DefineAnalyzer(name string, opts AnalyzerOptions) error {
	var parts []string

	if opts.Overwrite {
		parts = append(parts, "OVERWRITE")
	} else {
		parts = append(parts, "IF NOT EXISTS")
	}

	parts = append(parts, fmt.Sprintf("`%s`", name))

	if opts.Function != "" {
		parts = append(parts, fmt.Sprintf("FUNCTION %s", opts.Function))
	}

	if len(opts.Tokenizers) > 0 {
		parts = append(parts, fmt.Sprintf("TOKENIZERS %s", strings.Join(opts.Tokenizers, ",")))
	}

	if len(opts.Filters) > 0 {
		parts = append(parts, fmt.Sprintf("FILTERS %s", strings.Join(opts.Filters, ",")))
	}

	if opts.Comment != "" {
		parts = append(parts, fmt.Sprintf("COMMENT %q", opts.Comment))
	}

	sql := fmt.Sprintf("DEFINE ANALYZER %s", strings.Join(parts, " "))
	return m.DB.Exec(sql).Error
}

// RemoveAnalyzer drops an existing analyzer.
func (m Migrator) RemoveAnalyzer(name string) error {
	return m.DB.Exec(fmt.Sprintf("REMOVE ANALYZER IF EXISTS `%s`", name)).Error
}

// ============================================================================
// Pre-built analyzer helpers for common full-text search patterns.
// ============================================================================

// DefineBasicAnalyzer creates a simple blank-tokenized, lowercase-filtered analyzer.
func (m Migrator) DefineBasicAnalyzer(name string) error {
	return m.DefineAnalyzer(name, AnalyzerOptions{
		Tokenizers: []string{"blank"},
		Filters:    []string{"lowercase"},
	})
}

// DefineEdgeNgramAnalyzer creates an autocomplete-friendly analyzer using edge n-grams.
// min and max control the prefix lengths (e.g. 2,10).
func (m Migrator) DefineEdgeNgramAnalyzer(name string, min, max int) error {
	return m.DefineAnalyzer(name, AnalyzerOptions{
		Tokenizers: []string{"blank", "camel"},
		Filters:    []string{"lowercase", fmt.Sprintf("edgengram(%d,%d)", min, max)},
	})
}

// DefineSnowballAnalyzer creates a stemmed-search analyzer for a given language.
// Supported languages depend on the SurrealDB build; common ones: english, spanish, french, german.
func (m Migrator) DefineSnowballAnalyzer(name, language string) error {
	return m.DefineAnalyzer(name, AnalyzerOptions{
		Tokenizers: []string{"blank", "class"},
		Filters:    []string{"lowercase", fmt.Sprintf("snowball(%s)", language)},
	})
}

// DefineNgramAnalyzer creates an n-gram analyzer for fuzzy / substring matching.
// min and max are the gram sizes (e.g. 2,3).
func (m Migrator) DefineNgramAnalyzer(name string, min, max int) error {
	return m.DefineAnalyzer(name, AnalyzerOptions{
		Tokenizers: []string{"blank"},
		Filters:    []string{"lowercase", fmt.Sprintf("ngram(%d,%d)", min, max)},
	})
}

// DefineASCIIAnalyzer creates an analyzer that normalizes to ASCII (strips accents).
func (m Migrator) DefineASCIIAnalyzer(name string) error {
	return m.DefineAnalyzer(name, AnalyzerOptions{
		Tokenizers: []string{"blank", "camel", "class"},
		Filters:    []string{"ascii", "lowercase"},
	})
}
