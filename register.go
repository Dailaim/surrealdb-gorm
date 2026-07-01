package surrealdb

import (
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

// RegisterCallbacks registers SurrealDB-specific GORM callbacks.
//
// We do NOT call callbacks.RegisterDefaultCallbacks because it registers
// association-handling and preload callbacks that generate SQL incompatible
// with SurrealDB. Instead we manually register only the lifecycle-hook
// callbacks (BeforeCreate, AfterCreate, etc.) so that models can implement
// BeforeSave, AfterFind, etc. normally.
func RegisterCallbacks(db *gorm.DB) {
	// ── Create ──────────────────────────────────────────────────────────────
	db.Callback().Create().Register("gorm:before_create", callbacks.BeforeCreate)
	db.Callback().Create().Replace("gorm:create", CreateCallback)
	db.Callback().Create().After("gorm:create").Register("gorm:after_create", callbacks.AfterCreate)

	// ── Update ──────────────────────────────────────────────────────────────
	// SetupUpdateReflectValue ensures db.Statement.ReflectValue points to the
	// Model (not Dest) so that BeforeUpdate's callMethod can find the struct.
	db.Callback().Update().Register("gorm:setup_reflect_value", callbacks.SetupUpdateReflectValue)
	db.Callback().Update().After("gorm:setup_reflect_value").Register("gorm:before_update", callbacks.BeforeUpdate)
	db.Callback().Update().After("gorm:before_update").Replace("gorm:update", UpdateCallback)
	db.Callback().Update().After("gorm:update").Register("gorm:after_update", callbacks.AfterUpdate)

	// ── Delete ──────────────────────────────────────────────────────────────
	db.Callback().Delete().Register("gorm:before_delete", callbacks.BeforeDelete)
	db.Callback().Delete().After("gorm:before_delete").Register("surreal:edge_assoc_delete", edgeAssocDeleteCallback)
	db.Callback().Delete().After("surreal:edge_assoc_delete").Register("surreal:edge_assoc_replace", edgeAssocReplaceCallback)
	db.Callback().Delete().After("surreal:edge_assoc_replace").Register("gorm:delete", DeleteCallback)
	db.Callback().Delete().After("gorm:delete").Register("gorm:after_delete", callbacks.AfterDelete)

	// ── Row (used internally by GORM for association counts, etc.) ───────────
	db.Callback().Row().Register("surreal:edge_assoc_count", edgeAssocCountCallback)
	db.Callback().Row().After("surreal:edge_assoc_count").Register("gorm:row", RowCallback)

	// ── Query ────────────────────────────────────────────────────────────────
	db.Callback().Query().Register("surreal:handle_preload", handlePreloadAsFetch)
	db.Callback().Query().After("surreal:handle_preload").Register("gorm:query", QueryCallback)
	db.Callback().Query().After("gorm:query").Register("gorm:after_query", callbacks.AfterQuery)

	// ── Raw ──────────────────────────────────────────────────────────────────
	db.Callback().Raw().Register("gorm:raw", RawCallback)
}
