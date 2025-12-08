package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm"
	"github.com/dailaim/surrealdb-gorm/types"
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
	Book types.Link[Book] `json:"book,omitempty"`
}

func TestFetch(t *testing.T) {
	var err error
	db := setupDB(t)

	db.AutoMigrate(&Book{}, &Person{})

	// Create Book
	book := Book{Title: "SurrealDB Guide"}
	if err := db.Create(&book).Error; err != nil {
		t.Fatalf("Failed to create book: %v", err)
	}

	// Use raw SQL to create person to ensure we create a proper Record Link
	// We use type::thing() to cast the string ID to a record link.
	// This avoids the complexity of GORM/Driver struct marshalling for Record Links for now.
	// Note: book.ID.String() returns "book:..." which is what we need.
	var person Person

	person.Book.ID = book.ID
	person.Name = "Reader"

	if err := db.Debug().Create(&person).Error; err != nil {
		t.Fatalf("Failed to create person: %v", err)
	}

	// Fetch without FETCH clause
	// Normal select, book field should be "book:..." string inside the JSON,
	// but Person struct expects *Book.
	// We might fail unmarshal if we used Person struct here on non-fetched result.
	// But let's verify FETCH first.

	// Now Fetch WITH FETCH clause
	t.Logf("Fetching Person with ID: %s", person.ID)
	var p2 Person
	// We want to verify generated SQL contains "FETCH book" and populated struct.
	// We need to implement the Fetch clause first or just use it here locally to test.
	// I defined Fetch struct above to use it.

	err = db.Debug().Select("*, book_id AS book").Clauses(surrealdb.Fetch{Fields: []string{"book"}}).First(&p2, "id = ?", person.ID).Error
	if err != nil {
		t.Fatalf("Failed to query with FETCH: %v", err)
	}

	if p2.Book.Data == nil {
		t.Fatal("Expected p2.Book to be populated")
	}

	if p2.Book.ID == nil {
		t.Fatal("Expected p2.Book.ID to be populated")
	}

	if p2.Book.Data.Title != "SurrealDB Guide" {
		t.Errorf("Expected book title 'SurrealDB Guide', got '%s'", p2.Book.Data.Title)
	}
}
