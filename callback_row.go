package surrealdb

import (
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

// RowCallback backs db.Row() and db.Rows(). Now that QueryContext/QueryRowContext
// return real *sql.Row / *sql.Rows (see sqldriver.go), this executes the built
// statement and stores the result in Statement.Dest — mirroring GORM's default
// RowQuery, with one addition: it yields to edgeAssocCountCallback, which already
// produced the result for many2many association counts.
func RowCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}

	// Edge association count is fully handled upstream by edgeAssocCountCallback.
	if _, isAssocCount := db.Statement.Settings.Load("gorm:association:count"); isAssocCount {
		return
	}

	// db.Raw(...) has already populated Statement.SQL; for db.Model(...).Rows()
	// build the SELECT here. (Raw is the primary supported path; built SELECTs do
	// not receive the SurrealQL rewrites that executeSQL applies.)
	if db.Statement.SQL.Len() == 0 {
		callbacks.BuildQuerySQL(db)
	}
	if db.DryRun || db.Error != nil {
		return
	}

	sql := db.Statement.SQL.String()
	if isRows, ok := db.Get("rows"); ok && isRows.(bool) {
		db.Statement.Settings.Delete("rows")
		db.Statement.Dest, db.Error = db.Statement.ConnPool.QueryContext(
			db.Statement.Context, sql, db.Statement.Vars...)
	} else {
		db.Statement.Dest = db.Statement.ConnPool.QueryRowContext(
			db.Statement.Context, sql, db.Statement.Vars...)
	}
}
