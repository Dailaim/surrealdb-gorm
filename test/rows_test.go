package surrealdb_test

import (
	"testing"
)

// TestRawRows exercises the database/sql/driver layer: db.Raw(...).Rows() must
// return a real *sql.Rows we can iterate and scan from.
func TestRawRows(t *testing.T) {
	db := setupDB(t)

	// Seed known rows.
	db.Create(&User{Name: "RowsAlice", Age: 31})
	db.Create(&User{Name: "RowsBob", Age: 42})

	t.Run("scalar column iteration", func(t *testing.T) {
		rows, err := db.Raw("SELECT name FROM users WHERE name = ?", "RowsAlice").Rows()
		if err != nil {
			t.Fatalf("Rows() failed: %v", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("Columns() failed: %v", err)
		}
		if len(cols) != 1 || cols[0] != "name" {
			t.Fatalf("expected columns [name], got %v", cols)
		}

		var got []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			got = append(got, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows.Err: %v", err)
		}
		if len(got) == 0 {
			t.Fatal("expected at least one row")
		}
		for _, n := range got {
			if n != "RowsAlice" {
				t.Fatalf("expected only RowsAlice, got %q", n)
			}
		}
	})

	t.Run("scan into struct via ScanRows", func(t *testing.T) {
		rows, err := db.Raw("SELECT name, age FROM users WHERE age >= ? ORDER BY age", 31).Rows()
		if err != nil {
			t.Fatalf("Rows() failed: %v", err)
		}
		defer rows.Close()

		var users []User
		for rows.Next() {
			var u User
			if err := db.ScanRows(rows, &u); err != nil {
				t.Fatalf("ScanRows failed: %v", err)
			}
			users = append(users, u)
		}
		if len(users) < 2 {
			t.Fatalf("expected >=2 users, got %d", len(users))
		}
		if users[0].Name == "" || users[0].Age == 0 {
			t.Fatalf("expected populated fields, got %+v", users[0])
		}
	})
}
