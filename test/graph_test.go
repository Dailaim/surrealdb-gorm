package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
)

type Buyer struct {
	models.Schemaless
	Name string
}

type Wishlist struct {
	models.RelationSchemaless
}

type Product struct {
	models.Schemaless
	Name string
}

func TestGraphManyToMany(t *testing.T) {
	db := setupDB(t)

	db.AutoMigrate(&Buyer{}, &Wishlist{}, &Product{})

	buyer := Buyer{
		Name: "Alice",
	}
	// cart := Cart{}
	product := Product{
		Name: "Product 1",
	}

	// 1. Create Buyer and Class
	if err := db.Create(&buyer).Error; err != nil {
		t.Fatalf("Failed to create buyer: %v", err)
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("Failed to create product: %v", err)
	}

	// t.Logf("Created Student: %v, Product: %v", student.ID, product.ID)

	// // 2. Create Association
	// // Default GORM will try to insert into `enrolled_in` (student_id, class_id)
	// err := db.Debug().Model(&student).Association("Classes").Append(&class)
	// if err != nil {
	// 	t.Logf("Association Append failed/warned: %v", err)
	// }

	// // 3. Preload
	// var loadedStudent Student
	// err = db.Debug().Preload("Classes").First(&loadedStudent, "id = ?", student.ID).Error
	// if err != nil {
	// 	t.Logf("Preload failed: %v", err)
	// }

	// t.Logf("Loaded classes: %d", len(loadedStudent.Classes))
}
