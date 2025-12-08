package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm"
)

type CustomTagModel struct {
	surrealdb.Model
	MyField string `gorm:"column:custom_db_name" json:"myField"`
}

func (CustomTagModel) TableName() string {
	return "custom_tag_models"
}

func TestCreateWithGormTags(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&CustomTagModel{})

	model := CustomTagModel{
		MyField: "TestValue",
	}

	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("Failed to create: %v", err)
	}

	// 1. Verify ID is populated
	if model.ID == nil {
		t.Fatal("ID not populated")
	}

	// 2. Query Raw to see what column name was used
	// We expect "custom_db_name"
	var result map[string]interface{}
	var results []map[string]interface{}
	// We SELECT * FROM the record
	// Note: GORM Find with map dest might not work perfectly with SurrealDB dialect if Scan is not fully implemented.
	// But let's try Find instead of Raw().Scan().
	err := db.Table("custom_tag_models").Where("id = ?", model.ID).Find(&results).Error
	if err != nil {
		t.Fatalf("Failed to query raw: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("No results found")
	}
	result = results[0]

	t.Logf("Raw Result: %v", result)

	if _, ok := result["custom_db_name"]; !ok {
		// It probably used "myField" (from json tag) or "MyField"
		t.Errorf("Expected column 'custom_db_name' to exist in DB, but it was missing. Keys found: %v", result)
	}

	if val, ok := result["myField"]; ok {
		t.Errorf("Found 'myField' in DB, which means GORM tag was ignored in favor of JSON tag. Value: %v", val)
	}
}
