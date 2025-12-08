package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/dailaim/surrealdb-gorm/types"
)

type Tag struct {
	models.Schemaless
	Name string
}

type Article struct {
	models.Schemaless
	Title string
	Tags  types.SliceLink[Tag] `json:"tags,omitempty"`
}

func TestArrayLink(t *testing.T) {
	db := setupDB(t)

	db.AutoMigrate(&Tag{}, &Article{})

	// Create Tags
	tag1 := Tag{Name: "Go"}
	tag2 := Tag{Name: "SurrealDB"}
	db.Create(&tag1)
	db.Create(&tag2)

	// Create Article with Tags
	article := Article{
		Title: "Integration Guide",
		Tags: types.SliceLink[Tag]{
			{ID: tag1.ID},
			{ID: tag2.ID},
		},
	}
	if err := db.Create(&article).Error; err != nil {
		t.Fatalf("Failed to create article: %v", err)
	}

	// 1. Fetch Article and verify Tags IDs are present
	var fetched Article
	if err := db.First(&fetched, "id = ?", article.ID).Error; err != nil {
		t.Fatalf("Failed to fetch article: %v", err)
	}

	if len(fetched.Tags) != 2 {
		t.Fatalf("Expected 2 tags, got %d", len(fetched.Tags))
	} else {
		t.Logf("Fetched tags: %v", fetched.Tags)
	}

	// Check IDs
	if fetched.Tags[0].ID == nil || fetched.Tags[1].ID == nil {
		t.Fatal("Expected Tag IDs to be populated")
	}

	// 2. Fetch with Preload/Fetch to see if full objects are loaded
	// This might fail if []Link[T] doesn't handle JSON unmarshalling of array of objects correctly?
	// Actually types.Link[T].UnmarshalJSON handles single object or string.
	// But []Link[T] relies on json.Unmarshal iterating the array and calling UnmarshalJSON on each element.
	// This SHOULD work for JSON.
	var fetchedExploded Article
	// We need to use "Tags" in Preload. GORM/SurrealDB callbacks turn Preload into FETCH clause.
	// FETCH tags
	if err := db.Preload("Tags").First(&fetchedExploded, "id = ?", article.ID).Error; err != nil {
		t.Fatalf("Failed to fetch article with Preload: %v", err)
	}

	if len(fetchedExploded.Tags) != 2 {
		t.Fatalf("Preload: Expected 2 tags, got %d", len(fetchedExploded.Tags))
	}

	if fetchedExploded.Tags[0].Data == nil {
		t.Error("Preload: Expected Tag 1 data to be loaded")
	} else if fetchedExploded.Tags[0].Data.Name == "" {
		t.Error("Preload: Expected Tag 1 name to be loaded")
	}

	if fetchedExploded.Tags[1].Data == nil {
		t.Error("Preload: Expected Tag 2 data to be loaded")
	}
}
