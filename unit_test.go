package surrealdb

import (
	"strings"
	"testing"
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
