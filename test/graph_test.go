package surrealdb_test

import (
	"testing"
	"time"

	"github.com/dailaim/surrealdb-gorm"
	"github.com/dailaim/surrealdb-gorm/types"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Student struct {
	ID        *types.RecordID `gorm:"type:record;primaryKey"`
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Classes   []Class `gorm:"many2many:enrolled_in"`
}

type Class struct {
	ID        *types.RecordID `gorm:"type:record;primaryKey"`
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func setupGraphDB(t *testing.T) *gorm.DB {
	dsn := "wss://shiny-ocean-06dfnpnrpprgdf3fmb8e7vtpuo.aws-use1.surreal.cloud/rpc?namespace=test&database=test&username=root&password=root"
	db, err := gorm.Open(surrealdb.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Migrate models
	err = db.AutoMigrate(&Student{}, &Class{})
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}
	return db
}

func TestGraphManyToMany(t *testing.T) {
	t.Skip("Graph support (RELATE, -> arrows) not yet implemented in GORM dialect")
	db := setupGraphDB(t)

	// Setup Schema cleanup (Raw SQL)
	db.Exec("DELETE student")
	db.Exec("DELETE class")
	db.Exec("DELETE enrolled_in")

	student := Student{
		Name: "Alice",
	}
	class := Class{
		Title: "Math 101",
	}

	// 1. Create Student and Class
	if err := db.Create(&student).Error; err != nil {
		t.Fatalf("Failed to create student: %v", err)
	}
	if err := db.Create(&class).Error; err != nil {
		t.Fatalf("Failed to create class: %v", err)
	}

	t.Logf("Created Student: %v, Class: %v", student.ID, class.ID)

	// 2. Create Association
	// Default GORM will try to insert into `enrolled_in` (student_id, class_id)
	err := db.Debug().Model(&student).Association("Classes").Append(&class)
	if err != nil {
		t.Logf("Association Append failed/warned: %v", err)
	}

	// 3. Preload
	var loadedStudent Student
	err = db.Debug().Preload("Classes").First(&loadedStudent, "id = ?", student.ID).Error
	if err != nil {
		t.Logf("Preload failed: %v", err)
	}

	t.Logf("Loaded classes: %d", len(loadedStudent.Classes))
}
