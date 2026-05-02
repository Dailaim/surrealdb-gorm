package clauses

import (
	"gorm.io/gorm/clause"
)

// GraphSelect holds SurrealDB graph-traversal expressions that are appended to
// the SELECT list.  Each entry is a raw expression, e.g.:
//
//	->wishlist->product AS products
type GraphSelect struct {
	Fields []string
}

func (g GraphSelect) Name() string {
	return "GRAPH_SELECT"
}

func (g GraphSelect) Build(builder clause.Builder) {
	// Nothing – QueryCallback injects these directly into the SELECT expression.
}

func (g GraphSelect) MergeClause(c *clause.Clause) {
	if v, ok := c.Expression.(GraphSelect); ok {
		g.Fields = append(v.Fields, g.Fields...)
	}
	c.Expression = g
}
