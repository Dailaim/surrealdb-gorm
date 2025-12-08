package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm"
)

type UserUpdate struct {
	surrealdb.Model
	Name string
	Age  int
}

func (UserUpdate) TableName() string {
	return "user_updates"
}

func TestUpdateRepro(t *testing.T) {
	db := setupDB(t)

	// Cleanup
	db.Exec("DELETE user_updates")

	user := UserUpdate{Name: "Original", Age: 20}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 1. Update using Save (Full update)
	user.Name = "Updated"
	user.Age = 25
	if err := db.Save(&user).Error; err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify
	var found UserUpdate
	if err := db.First(&found, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("First failed: %v", err)
	}

	if found.Name != "Updated" {
		t.Errorf("Update failed (Save). Got %s, want Updated", found.Name)
	}

	// 2. Update using Updates (Partial)
	if err := db.Model(&user).Updates(UserUpdate{Name: "Partial"}).Error; err != nil {
		t.Fatalf("Updates failed: %v", err)
	}

	if err := db.First(&found, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("First failed: %v", err)
	}
	if found.Name != "Partial" {
		t.Errorf("Partial update failed. Got %s, want Partial", found.Name)
	}

	// 3. Update using Update (Single column)
	if err := db.Model(&user).Update("age", 30).Error; err != nil {
		t.Fatalf("Update column failed: %v", err)
	}

	db.First(&found, "id = ?", user.ID)
	if found.Age != 30 {
		t.Errorf("Single column update failed. Got %d, want 30", found.Age)
	}

	// 4. Update using Schemaless (Table)
	if err := db.Table("user_updates").Where("id = ?", user.ID).Update("name", "Schemaless").Error; err != nil {
		t.Fatalf("Schemaless update failed: %v", err)
	}

	if err := db.First(&found, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("First failed: %v", err)
	}
	if found.Name != "Schemaless" {
		t.Errorf("Schemaless update failed. Got %s, want Schemaless", found.Name)
	}
}
