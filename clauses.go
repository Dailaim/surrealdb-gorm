package surrealdb

import (
	"gorm.io/gorm/clause"
)

// Fetch clause for SurrealDB
type Fetch struct {
	Fields []string
}

// Name returns the clause name
func (f Fetch) Name() string {
	return "FETCH"
}

// Build builds the FETCH clause
func (f Fetch) Build(builder clause.Builder) {
	for i, field := range f.Fields {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(field)
	}
}

// MergeClause merges multiple FETCH clauses
func (f Fetch) MergeClause(c *clause.Clause) {
	if v, ok := c.Expression.(Fetch); ok {
		f.Fields = append(f.Fields, v.Fields...)
	}
	c.Expression = f
}
