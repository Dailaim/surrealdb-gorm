package surrealdb_test

import (
	"context"
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/surrealdb/surrealdb.go"
)

type OffsetModel struct {
	models.Schemaless
	Name  string
	Index int
}

func TestOffset(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&OffsetModel{})
	surrealdb.Query[interface{}](context.Background(), getDialector(db).Conn, "DELETE offset_models", nil)
	for i := 0; i < 5; i++ {
		db.Create(&OffsetModel{Name: "Test", Index: i})
	}
	var res []OffsetModel
	err := db.Debug().Limit(2).Offset(1).Find(&res).Error
	if err != nil {
		t.Fatalf("Failed with Offset: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("Expected 2, got %d", len(res))
	}
}

func TestOffsetWithOrder(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&OffsetModel{})
	db.Debug().Exec("DELETE FROM offset_models")

	for i := 1; i <= 10; i++ {
		db.Create(&OffsetModel{Name: "Item", Index: i})
	}

	var res []OffsetModel
	err := db.Order("index desc").Limit(3).Offset(2).Find(&res).Error
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("Expected 3, got %d", len(res))
	}
	if res[0].Index != 8 || res[1].Index != 7 || res[2].Index != 6 {
		t.Errorf("Unexpected order: %v", res)
	}
}

func TestOffsetWithFilter(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&OffsetModel{})
	db.Debug().Exec("DELETE FROM offset_models")

	for i := 1; i <= 10; i++ {
		db.Create(&OffsetModel{Name: "Item", Index: i})
	}

	var res []OffsetModel
	// index > 5 means 6,7,8,9,10
	// offset 1 means start at 7
	// limit 2 means 7, 8
	err := db.Debug().Where("index > ?", 5).Order("index asc").Limit(2).Offset(1).Find(&res).Error
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("Expected 2, got %d", len(res))
	}
	if res[0].Index != 7 || res[1].Index != 8 {
		t.Errorf("Unexpected result: %v", res)
	}
}

func TestOffsetOutOfBounds(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&OffsetModel{})
	db.Debug().Exec("DELETE FROM offset_models")

	for i := 1; i <= 5; i++ {
		db.Create(&OffsetModel{Name: "Item", Index: i})
	}

	var res []OffsetModel
	err := db.Debug().Limit(5).Offset(10).Find(&res).Error
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("Expected 0, got %d", len(res))
	}
}
