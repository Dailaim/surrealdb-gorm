package surrealdb

import (
	"gorm.io/gorm"
)

// RegisterCallbacks registers SurrealDB-specific GORM callbacks.
func RegisterCallbacks(db *gorm.DB) {
	db.Callback().Create().Replace("gorm:create", CreateCallback)
	db.Callback().Update().Replace("gorm:update", UpdateCallback)

	db.Callback().Delete().Before("gorm:delete").Register("surreal:edge_assoc_delete", edgeAssocDeleteCallback)
	db.Callback().Delete().Before("gorm:delete").Register("surreal:edge_assoc_replace", edgeAssocReplaceCallback)
	db.Callback().Delete().Replace("gorm:delete", DeleteCallback)

	db.Callback().Row().Before("gorm:row").Register("surreal:edge_assoc_count", edgeAssocCountCallback)

	db.Callback().Query().Before("gorm:query").Register("surreal:handle_preload", handlePreloadAsFetch)
	db.Callback().Query().Replace("gorm:query", QueryCallback)
	db.Callback().Raw().Replace("gorm:raw", RawCallback)
}
