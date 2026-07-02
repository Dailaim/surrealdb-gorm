package surrealdb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

type RecModel struct {
	models.BaseModel
	N int `json:"n"`
}

// TestReconnectAfterDrop verifies the auto-reconnecting WebSocket connection
// recovers after the server goes away and comes back. It is gated behind an env
// var because it requires restarting the database out-of-band during the test
// window (e.g. `podman compose restart surrealdb`).
func TestReconnectAfterDrop(t *testing.T) {
	if os.Getenv("SURREALDB_RECONNECT_TEST") == "" {
		t.Skip("set SURREALDB_RECONNECT_TEST=1 and restart the DB during the run")
	}
	db := setupDB(t)
	require.NoError(t, db.AutoMigrate(&RecModel{}))
	require.NoError(t, db.Create(&RecModel{N: 1}).Error)

	deadline := time.Now().Add(40 * time.Second)
	var sawFailure, recovered bool
	for time.Now().Before(deadline) {
		var count int64
		// Per-query timeout so a frozen server doesn't hang the loop.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := db.WithContext(ctx).Model(&RecModel{}).Count(&count).Error
		cancel()
		switch {
		case err != nil:
			sawFailure = true
			t.Logf("query failed (expected while DB is down): %v", err)
		case sawFailure:
			recovered = true
			t.Logf("recovered after reconnect: count=%d", count)
		}
		if recovered {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.True(t, sawFailure, "expected a failure while the DB was restarting")
	require.True(t, recovered, "expected the connection to recover after reconnect")
}
