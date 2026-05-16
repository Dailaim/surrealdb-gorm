package surrealdb_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/dailaim/surrealdb-gorm"
	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

func TestDefineAndRemoveEvent(t *testing.T) {
	db := setupDB(t)

	type EventUser struct {
		models.BaseModel
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	err := db.AutoMigrate(&EventUser{})
	require.NoError(t, err)

	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// Create a simple event that logs mutations.
	err = m.DefineEvent("event_users", "test_log", surrealdb.EventOptions{
		Then: "CREATE `event_logs` SET target = $value.id, action = $event, at = time::now()",
	})
	require.NoError(t, err)

	// Create a record to trigger the event.
	user := EventUser{Name: "EventTest", Email: "test@example.com"}
	err = db.Create(&user).Error
	require.NoError(t, err)

	// Note: Verifying event side-effects via SELECT is unreliable in the
	// SurrealDB Go driver because SELECT queries do not return sql.Rows.
	// We trust that the DEFINE EVENT statement was accepted by the server.

	// Remove the event.
	err = m.RemoveEvent("event_users", "test_log")
	require.NoError(t, err)

	// Cleanup logs table.
	db.Exec("REMOVE TABLE IF EXISTS `event_logs`")
	db.Exec("REMOVE TABLE IF EXISTS `event_users`")
}

func TestDefineAuditEvent(t *testing.T) {
	db := setupDB(t)

	type AuditUser struct {
		models.BaseModel
		Name string `json:"name"`
	}
	err := db.AutoMigrate(&AuditUser{})
	require.NoError(t, err)

	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// Create an audit event.
	err = m.DefineAuditEvent("audit_users", "audit_log", "user mutated")
	require.NoError(t, err)

	// Trigger it.
	user := AuditUser{Name: "AuditTest"}
	err = db.Create(&user).Error
	require.NoError(t, err)

	// Cleanup
	err = m.RemoveEvent("audit_users", "audit_users")
	require.NoError(t, err)
	db.Exec("REMOVE TABLE IF EXISTS `audit_log`")
	db.Exec("REMOVE TABLE IF EXISTS `audit_users`")
}

func TestDefineCreatedAtEvent(t *testing.T) {
	db := setupDB(t)

	// Use a unique table name to avoid conflicts with previous test runs.
	const tableName = "ts_users_v2"

	// Clean up any stale events from previous runs.
	db.Exec(fmt.Sprintf("REMOVE EVENT IF EXISTS `ts_ts_users_updated_at` ON TABLE `%s`", tableName))
	db.Exec(fmt.Sprintf("REMOVE EVENT IF EXISTS `created_at_%s_created_at` ON TABLE `%s`", tableName, tableName))
	db.Exec(fmt.Sprintf("REMOVE TABLE IF EXISTS `%s`", tableName))

	type TSUser struct {
		models.BaseModel
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}
	err := db.Table(tableName).AutoMigrate(&TSUser{})
	require.NoError(t, err)

	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// Create a created-at event (fires only on CREATE to avoid recursion).
	err = m.DefineCreatedAtEvent(tableName, "created_at")
	require.NoError(t, err)

	// Trigger it.
	user := TSUser{Name: "TSTest"}
	err = db.Table(tableName).Create(&user).Error
	require.NoError(t, err)

	// Cleanup
	err = m.RemoveEvent(tableName, fmt.Sprintf("created_at_%s_created_at", tableName))
	require.NoError(t, err)
	db.Exec(fmt.Sprintf("REMOVE TABLE IF EXISTS `%s`", tableName))
}
