package surrealdb_test

import (
	"testing"

	"github.com/dailaim/2077/pkg/surrealdb"
)

type Book struct {
	surrealdb.Model
	Title string
}

func (Book) TableName() string {
	return "book"
}

type Person struct {
	surrealdb.Model
	Name string
	Book *Book `json:"book,omitempty" gorm:"-"` // populated by FETCH and used for Create (as link)
}

func (Person) TableName() string {
	return "person"
}

func TestFetch(t *testing.T) {
	db := setupDB(t)

	// Cleanup
	db.Exec("DELETE person")
	db.Exec("DELETE book")

	// Create Book
	book := Book{Title: "SurrealDB Guide"}
	if err := db.Create(&book).Error; err != nil {
		t.Fatalf("Failed to create book: %v", err)
	}

	// Create Person linked to Book
	if book.ID == nil {
		t.Fatal("Book ID is nil")
	}
	// Use raw SQL to create person to ensure we create a proper Record Link
	// We use type::thing() to cast the string ID to a record link.
	// This avoids the complexity of GORM/Driver struct marshalling for Record Links for now.
	// Note: book.ID.String() returns "book:..." which is what we need.
	if err := db.Exec("CREATE person SET name = 'Reader', book = type::thing($1)", book.ID.String()).Error; err != nil {
		t.Fatalf("Failed to create person: %v", err)
	}

	// Retrieve the ID of the created person for the Fetch query
	// We can't easily get it from execution result here with just Exec (generic).
	// So let's query it back or use a fixed ID?
	// Better: Use fixed ID or query it.
	var person Person
	if err := db.First(&person, "name = ?", "Reader").Error; err != nil {
		t.Fatalf("Failed to find created person: %v", err)
	}

	// Fetch without FETCH clause
	// Normal select, book field should be "book:..." string inside the JSON,
	// but Person struct expects *Book.
	// We might fail unmarshal if we used Person struct here on non-fetched result.
	// But let's verify FETCH first.

	// Now Fetch WITH FETCH clause
	var p2 Person
	// We want to verify generated SQL contains "FETCH book" and populated struct.
	// We need to implement the Fetch clause first or just use it here locally to test.
	// I defined Fetch struct above to use it.

	err := db.Debug().Clauses(surrealdb.Fetch{Fields: []string{"book"}}).First(&p2, "id = ?", personInput.ID).Error
	if err != nil {
		t.Fatalf("Failed to query with FETCH: %v", err)
	}

	if p2.Book == nil {
		t.Fatal("Expected p2.Book to be populated")
	}
	if p2.Book.Title != "SurrealDB Guide" {
		t.Errorf("Expected book title 'SurrealDB Guide', got '%s'", p2.Book.Title)
	}
}
