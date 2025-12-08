package surrealdb_test

import (
	"fmt"
	"testing" // Ensure time is imported
	"time"

	"gorm.io/gorm/logger"

	"github.com/dailaim/surrealdb-gorm"
	"gorm.io/gorm"
)

type User struct {
	surrealdb.Model
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func (u *User) BeforeDelete(tx *gorm.DB) (err error) {
	tx.Statement.DB.Logger.Info(tx.Statement.Context, "BeforeDelete called for ID: %v", u.ID)
	return
}

func setupDB(t *testing.T) *gorm.DB {
	dsn := "wss://shiny-ocean-06dfnpnrpprgdf3fmb8e7vtpuo.aws-use1.surreal.cloud/rpc?namespace=test&database=test&username=root&password=root"
	db, err := gorm.Open(surrealdb.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// AutoMigrate once per test run? or per test?
	// It's fast now.
	err = db.AutoMigrate(&User{})
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify ConnPool type
	if _, ok := db.ConnPool.(*surrealdb.Dialector); !ok {
		t.Fatalf("db.ConnPool is NOT *surrealdb.Dialector! It is: %T", db.ConnPool)
	}

	return db
}

func TestCreate(t *testing.T) {
	db := setupDB(t)

	user := User{Name: "Jinzhu", Age: 18}
	db.Create(&user)

	if user.ID == nil {
		t.Errorf("Expected ID to be populated")
	} else {
		t.Logf("Created user with ID: %v", user.ID)

		if user.CreatedAt.IsZero() {
			t.Errorf("Expected CreatedAt to be set")
		}
	}
}

func TestFind(t *testing.T) {
	db := setupDB(t)

	// Seed
	user := User{Name: "FindMe", Age: 20}
	db.Create(&user)

	var users []User
	result := db.Debug().Find(&users)
	if result.Error != nil {
		t.Fatalf("Failed to query: %v", result.Error)
	}
	if len(users) == 0 {
		t.Errorf("Expected users to be found")
	}
}

func TestUpdate(t *testing.T) {
	db := setupDB(t)

	user := User{Name: "UpdateMe", Age: 18}
	db.Create(&user)

	// Update
	// user.Age = 20
	result := db.Debug().Model(&user).Update("age", 99)
	if result.Error != nil {
		t.Errorf("Failed to update: %v", result.Error)
	}

	var u User
	err := db.First(&u, "id = ?", user.ID).Error
	if err != nil {
		t.Errorf("Failed to find updated user: %v", err)
	}
	fmt.Printf("%#v", u)

	if u.Age != 99 {
		t.Errorf("Expected age to be 99, got %d", u.Age)
	}
}

func TestDelete(t *testing.T) {
	db := setupDB(t)

	// Seed
	user := User{Name: "DeleteMe", Age: 30}
	db.Create(&user)

	t.Logf("User ID after create: %v", user.ID)
	if user.ID == nil {
		t.Fatal("User ID is nil after create!")
	}

	time.Sleep(2 * time.Second)

	// println("ID", user.ID.String())
	// Delete
	result := db.Delete(&user)
	if result.Error != nil {
		t.Fatalf("Failed to delete: %v", result.Error)
	}
	t.Logf("User ID after delete: %v", user.ID)
	if user.ID == nil {
		t.Fatal("User ID is nil after delete!")
	}

	// Verify Soft Delete
	// 1. Should not be found via normal Find
	var u User
	res := db.First(&u, "id = ?", user.ID)

	if u.ID != nil {
		t.Errorf("Expected user to be soft deleted and not found, but got: %v", u)
	}

	if res.Error == nil && res.RowsAffected != 0 {
		t.Errorf("Expected error (RecordNotFound) or 0 rows for soft deleted user, got: %v", u)
	}

	// 2. Should be found via Unscoped
	uUnscoped := new(User)
	res = db.Unscoped().First(&uUnscoped, "id = ?", user.ID)
	if res.Error != nil {
		t.Errorf("Expected user to be found with Unscoped, but got error: %v", res.Error)
	}
	if !uUnscoped.DeletedAt.Valid {
		t.Errorf("Expected DeletedAt to be valid (soft deleted)")
	}
}

func TestRawExec(t *testing.T) {
	db := setupDB(t)

	user := User{Name: "ExecMe", Age: 40}
	db.Create(&user)

	// Verify raw Exec
	err := db.Exec("UPDATE users SET age = 100 WHERE id = ?", user.ID).Error
	if err != nil {
		t.Logf("Exec error: %v", err)
	}
}
