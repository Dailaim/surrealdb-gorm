package surrealdb

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

// normalizeWS collapses runs of whitespace to a single space and trims, so
// tests assert on the semantic result rather than cosmetic spacing.
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Pure unit tests for internal helpers — no database required.

func TestBuildVectorIndexParams(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"dimension=4", " DIMENSION 4"},
		{"dimension=768 dist=cosine", " DIMENSION 768 DIST cosine"},
		{"dimension=4 dist=euclidean efc=150 m=12", " DIMENSION 4 DIST euclidean EFC 150 M 12"},
		{"type=F32 dimension=3 capacity=40", " DIMENSION 3 TYPE F32 CAPACITY 40"}, // reordered to SurrealDB order
		{"unknown=x dimension=2", " DIMENSION 2"},                                 // unknown keys ignored
	}
	for _, c := range cases {
		if got := buildVectorIndexParams(c.in); got != c.want {
			t.Errorf("buildVectorIndexParams(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRewriteExecSQL(t *testing.T) {
	cases := []struct{ in, want string }{
		// DELETE FROM -> DELETE
		{"DELETE FROM `users` WHERE `id` = $p1", "DELETE `users` WHERE `id` = $p1"},
		// UPDATE: strip `id` = ... from SET, keep the rest
		{"UPDATE users SET `id` = $p1, `name` = $p2 WHERE x = $p3", "UPDATE users SET `name` = $p2 WHERE x = $p3"},
		// UPDATE with id in the middle
		{"UPDATE users SET name = $p1, id = $p2 WHERE x", "UPDATE users SET name = $p1 WHERE x"},
		// Unaffected statements pass through
		{"SELECT * FROM users", "SELECT * FROM users"},
	}
	for _, c := range cases {
		if got := normalizeWS(rewriteExecSQL(c.in)); got != c.want {
			t.Errorf("rewriteExecSQL(%q) =\n  %q\nwant\n  %q", c.in, got, c.want)
		}
	}
}

func TestOptimizeFindByIDList(t *testing.T) {
	rid := func(s string) *TypesM.RecordID { r, _ := TypesM.ParseRecordID(s); return r }

	newStmt := func(sql string, vars ...interface{}) *gorm.DB {
		stmt := &gorm.Statement{Table: "users", Vars: vars}
		stmt.SQL.WriteString(sql)
		return &gorm.DB{Statement: stmt}
	}

	// id IN (...) with RecordID vars → direct record access, soft-delete kept.
	db := newStmt(
		"SELECT * FROM `users` WHERE `id` IN ($p1,$p2) AND (`deleted_at` IS NULL OR `deleted_at` IS NONE)",
		rid("users:a"), rid("users:b"),
	)
	optimizeFindByIDList(db)
	if got := normalizeWS(db.Statement.SQL.String()); got != "SELECT * FROM $p1, $p2 WHERE (`deleted_at` IS NULL OR `deleted_at` IS NONE)" {
		t.Errorf("id-list rewrite = %q", got)
	}

	// Non-RecordID vars must NOT be rewritten.
	db = newStmt("SELECT * FROM `users` WHERE `id` IN ($p1,$p2)", "a", "b")
	optimizeFindByIDList(db)
	if got := db.Statement.SQL.String(); got != "SELECT * FROM `users` WHERE `id` IN ($p1,$p2)" {
		t.Errorf("non-recordid should be untouched, got %q", got)
	}

	// No id-IN predicate → untouched.
	db = newStmt("SELECT * FROM `users` WHERE `name` = $p1", "x")
	optimizeFindByIDList(db)
	if got := db.Statement.SQL.String(); got != "SELECT * FROM `users` WHERE `name` = $p1" {
		t.Errorf("no id-in should be untouched, got %q", got)
	}
}
