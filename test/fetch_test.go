package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/clauses"
	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/dailaim/surrealdb-gorm/types"
)

type Book struct {
	models.Schemaless
	Title string
}

func (Book) TableName() string {
	return "book"
}

type Person struct {
	models.Schemaless
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

	err = db.Debug().Select("*, book_id AS book").Clauses(clauses.Fetch{Fields: []string{"book"}}).First(&p2, "id = ?", person.ID).Error
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

func TestPreloadFetch(t *testing.T) {
	var err error
	db := setupDB(t)

	db.AutoMigrate(&Book{}, &Person{})

	// Create Book
	book := Book{Title: "SurrealDB Guide"}
	if err := db.Create(&book).Error; err != nil {
		t.Fatalf("Failed to create book: %v", err)
	}

	var person Person
	person.Book.ID = book.ID
	person.Name = "Reader"

	if err := db.Create(&person).Error; err != nil {
		t.Fatalf("Failed to create person: %v", err)
	}

	// Test Preload
	var p3 Person
	// Preload "Book" should generate FETCH book (using DBName)
	err = db.Debug().Preload("Book").First(&p3, "id = ?", person.ID).Error
	if err != nil {
		t.Fatalf("Failed query with Preload: %v", err)
	}

	if p3.Book.Data == nil {
		t.Fatal("Expected p3.Book to be populated via Preload")
	}

	if p3.Book.Data.Title != "SurrealDB Guide" {
		t.Errorf("Expected book title 'SurrealDB Guide', got '%s'", p3.Book.Data.Title)
	}
}

// ---------------------------------------------------------------------------------------------------------------------

type Node struct {
	models.Schemaless
	Name string           `gorm:"column:name" json:"aaaa"`
	Next types.Link[Node] `json:"next,omitempty"`
}

func (Node) TableName() string { return "Node" }

func TestDeepPreload(t *testing.T) {
	var err error
	db := setupDB(t)

	db.AutoMigrate(&Node{})

	// Create deeply nested data
	l1 := Node{Name: "Root"}
	if err := db.Create(&l1).Error; err != nil {
		t.Fatalf("Failed L1: %v", err)
	}

	l2 := Node{Name: "Level 2", Next: types.Link[Node]{ID: l1.ID}}
	if err := db.Create(&l2).Error; err != nil {
		t.Fatalf("Failed L2: %v", err)
	}

	l3 := Node{Name: "Level 3", Next: types.Link[Node]{ID: l2.ID}}
	if err := db.Create(&l3).Error; err != nil {
		t.Fatalf("Failed L3: %v", err)
	}

	l4 := Node{Name: "Level 4", Next: types.Link[Node]{ID: l3.ID}}
	if err := db.Create(&l4).Error; err != nil {
		t.Fatalf("Failed L4: %v", err)
	}

	l5 := Node{Name: "Level 5", Next: types.Link[Node]{ID: l4.ID}}
	if err := db.Create(&l5).Error; err != nil {
		t.Fatalf("Failed L5: %v", err)
	}

	// Test Deep Preload
	var result Node
	err = db.Debug().
		Preload("Next.Next.Next.Next").
		First(&result, "id = ?", l5.ID).Error

	if err != nil {
		t.Fatalf("Deep Preload failed: %v", err)
	}

	if result.Next.Data == nil {
		t.Fatal("L1->L2 missing")
	}
	if result.Next.Data.Next.Data == nil {
		t.Fatal("L2->L3 missing")
	}
	if result.Next.Data.Next.Data.Next.Data == nil {
		t.Fatal("L3->L4 missing")
	}
	if result.Next.Data.Next.Data.Next.Data.Next.Data == nil {
		t.Fatal("L4->L5 missing")
	}

	if result.Next.Data.Next.Data.Next.Data.Next.Data.Name != "Root" {
		t.Errorf("Expected 'Root', got '%s'", result.Next.Data.Next.Data.Next.Data.Next.Data.Name)
	}
}

func TestDeepFilter(t *testing.T) {
	var err error
	db := setupDB(t)

	db.AutoMigrate(&Node{})

	// Create deeply nested data
	l5 := Node{Name: "Target"}
	if err := db.Create(&l5).Error; err != nil {
		t.Fatalf("Failed L5: %v", err)
	}
	l4 := Node{Name: "Level 4", Next: types.Link[Node]{ID: l5.ID}}
	if err := db.Create(&l4).Error; err != nil {
		t.Fatalf("Failed L4: %v", err)
	}
	l3 := Node{Name: "Level 3", Next: types.Link[Node]{ID: l4.ID}}
	if err := db.Create(&l3).Error; err != nil {
		t.Fatalf("Failed L3: %v", err)
	}
	l2 := Node{Name: "Level 2", Next: types.Link[Node]{ID: l3.ID}}
	if err := db.Create(&l2).Error; err != nil {
		t.Fatalf("Failed L2: %v", err)
	}
	l1 := Node{Name: "RootMatch", Next: types.Link[Node]{ID: l2.ID}}
	if err := db.Create(&l1).Error; err != nil {
		t.Fatalf("Failed L1: %v", err)
	}

	// Create another one that shouldn't match
	l5b := Node{Name: "Other"}
	db.Create(&l5b)
	l4b := Node{Name: "Level 4b", Next: types.Link[Node]{ID: l5b.ID}}
	db.Create(&l4b)
	l3b := Node{Name: "Level 3b", Next: types.Link[Node]{ID: l4b.ID}}
	db.Create(&l3b)
	l2b := Node{Name: "Level 2b", Next: types.Link[Node]{ID: l3b.ID}}
	db.Create(&l2b)
	l1b := Node{Name: "RootNoMatch", Next: types.Link[Node]{ID: l2b.ID}}
	db.Create(&l1b)

	// Test Deep Filter
	// WHERE next.next.next.next.name = 'Target'
	// We use "Next" in Go because that's our struct name, but we might need to handle casing.
	// GORM likely quotes "Next" if we pass it as string keys in map, but explicit String SQL works best?
	// db.Where("next.next.next.next.name = ?", "Target")

	var result Node
	// Use explicit lower case for SurrealDB fields or rely on GORM unquoting?
	// Using "Next.Next..." in Where string is passed directly usually.
	err = db.Debug().
		Preload("Next.Next.Next.Next").
		Where("next.next.next.next.name = ?", "Target").
		First(&result).Error

	if err != nil {
		t.Fatalf("Deep Filter failed: %v", err)
	}

	if result.Name != "RootMatch" {
		t.Errorf("Expected 'RootMatch', got '%s'", result.Name)
	}
}
