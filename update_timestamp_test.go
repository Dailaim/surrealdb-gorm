package surrealdb_test

import (
	"testing"
	"time"
)

func TestUpdateTimestamp(t *testing.T) {
	db := setupDB(t)

	user := User{Name: "TimestampTest", Age: 25}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	initialUpdatedAt := user.UpdatedAt
	t.Logf("Initial UpdatedAt: %v", initialUpdatedAt)

	if initialUpdatedAt.IsZero() {
		t.Error("Expected initial UpdatedAt to be set")
	}

	// Sleep to ensure time difference
	time.Sleep(2 * time.Second)

	// Update user
	if err := db.Model(&user).Update("age", 26).Error; err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	// Fetch updated user to check UpdatedAt
	var updatedUser User
	if err := db.First(&updatedUser, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated user: %v", err)
	}

	t.Logf("New UpdatedAt: %v", updatedUser.UpdatedAt)

	if updatedUser.UpdatedAt.Equal(initialUpdatedAt) {
		t.Errorf("UpdatedAt was not updated! Initial: %v, New: %v", initialUpdatedAt, updatedUser.UpdatedAt)
	}

	if !updatedUser.UpdatedAt.After(initialUpdatedAt) {
		t.Errorf("New UpdatedAt should be after initial UpdatedAt")
	}
}
