package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
)

type OffsetModel struct {
	models.Schemaless
	Name string
}

func TestOffset(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&OffsetModel{})
	for i := 0; i < 5; i++ {
		db.Create(&OffsetModel{Name: "Test"})
	}
	var res []OffsetModel
	err := db.Limit(2).Offset(1).Find(&res).Error
	if err != nil {
		t.Fatalf("Failed with Offset: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("Expected 2, got %d", len(res))
	}
}
