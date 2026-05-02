package surrealdb_test

import (
	"testing"

	"gorm.io/gorm"
)

func TestIdiomaticDelete(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&OffsetModel{})
	db.Create(&OffsetModel{Name: "Item", Index: 1})

	err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&OffsetModel{}).Error
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var count int64
	db.Model(&OffsetModel{}).Count(&count)
	if count != 0 {
		t.Fatalf("Expected 0 records, got %d", count)
	}
}
