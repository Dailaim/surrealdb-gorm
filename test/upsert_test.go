package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"gorm.io/gorm/clause"
)

type UpsertModel struct {
	models.Schemaless
	Name string
	Age  int
}

func TestUpsert(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&UpsertModel{})
	model := UpsertModel{Name: "Upsert"}
	db.Create(&model)

	// Try Save
	model.Age = 10
	err := db.Save(&model).Error
	if err != nil {
		t.Fatalf("Failed with Save: %v", err)
	}

	// Try Upsert
	err = db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&model).Error
	if err != nil {
		t.Fatalf("Failed with OnConflict: %v", err)
	}
}
