package surrealdb_test

import (
	"testing"
	"time"

	"github.com/dailaim/surrealdb-gorm"
	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

func TestChangefeed(t *testing.T) {
	db := setupDB(t)

	type CFUser struct {
		models.BaseModel
		Name string `json:"name"`
	}

	const tableName = "cf_users"
	db.Exec("REMOVE TABLE IF EXISTS `" + tableName + "`")

	err := db.Table(tableName).AutoMigrate(&CFUser{})
	require.NoError(t, err)

	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}

	// Enable changefeed with 1 hour retention.
	err = m.SetTableChangefeed(tableName, "1h", false)
	require.NoError(t, err)

	// Create a record.
	user := CFUser{Name: "CFTest"}
	err = db.Table(tableName).Create(&user).Error
	require.NoError(t, err)

	// Note: SHOW CHANGES is not reliably testable via GORM Scan because
	// the SurrealDB driver does not return sql.Rows for this statement.
	// The SQL generation is tested separately via ShowChangesSQL.
	sql := surrealdb.ShowChangesSQL(tableName, time.Now().UTC().Add(-time.Minute), 10)
	require.Contains(t, sql, "SHOW CHANGES FOR TABLE")
	require.Contains(t, sql, "SINCE")
	t.Logf("Generated SQL: %s", sql)

	// Disable changefeed.
	err = m.DropTableChangefeed(tableName)
	require.NoError(t, err)

	// Cleanup
	db.Exec("REMOVE TABLE IF EXISTS `" + tableName + "`")
}
