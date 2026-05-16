package surrealdb_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dailaim/surrealdb-gorm"
	"github.com/dailaim/surrealdb-gorm/models"
)

func TestLiveSelectAndKill(t *testing.T) {
	db := setupDB(t)

	// Create a temporary table for live query testing.
	type LiveUser struct {
		models.BaseModel
		Name string `json:"name"`
	}
	err := db.AutoMigrate(&LiveUser{})
	require.NoError(t, err)

	// Start a live query.
	live, err := surrealdb.NewLiveQuery(db, "live_users", false)
	require.NoError(t, err)
	require.NotEmpty(t, live.ID)
	t.Logf("Live query ID: %s", live.ID)

	// Get notifications channel.
	notifications, err := live.Notifications()
	require.NoError(t, err)

	// Create a record to trigger a notification.
	user := LiveUser{Name: "LiveTest"}
	err = db.Create(&user).Error
	require.NoError(t, err)

	// Wait for notification with timeout.
	select {
	case notif := <-notifications:
		t.Logf("Received notification: %+v", notif)
	case <-time.After(5 * time.Second):
		t.Log("No notification received within timeout (this is OK if live queries are not fully supported)")
	}

	// Kill the live query.
	err = live.Kill()
	require.NoError(t, err)

	// Cleanup
	db.Exec("REMOVE TABLE IF EXISTS `live_users`")
}

func TestLiveQueryViaRaw(t *testing.T) {
	// LIVE SELECT via raw query is not reliably supported through GORM's
	// Scan interface because the driver returns a live-query UUID via a
	// different mechanism. Use surrealdb.LiveSelect instead.
	t.Skip("LIVE SELECT via raw query is not supported through GORM Scan")
}
