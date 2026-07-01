package surrealdb

import (
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	localModels "github.com/dailaim/surrealdb-gorm/models"
	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

// hasDeletedAt checks whether the given value (expected to be a struct or pointer
// to struct) contains a field named "DeletedAt" at any nesting level (embedded).
func hasDeletedAt(v interface{}) bool {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return false
	}
	return lookUpDeletedAt(rv.Type())
}

func lookUpDeletedAt(rt reflect.Type) bool {
	if rt.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Name == "DeletedAt" {
			return true
		}
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if lookUpDeletedAt(field.Type) {
				return true
			}
		}
	}
	return false
}

func DeleteCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}

	dialector := db.Dialector.(*Dialector)

	// NOTE: We intentionally do NOT call db.Statement.AddClause on
	// db.Statement.Schema.DeleteClauses here (unlike GORM's default delete
	// callback).  The DeleteClauses for soft-delete models contain
	// SoftDeleteDeleteClause whose ModifyStatement implementation immediately
	// calls stmt.Build(), writing SQL into stmt.SQL before we have a chance to
	// choose our own code path.  If we let that happen, the subsequent
	// db.Statement.SQL.Len() == 0 guard below is always false and we fall
	// through to a second stmt.Build() call, producing a SQL string with two
	// consecutive WHERE clauses that SurrealDB rejects.
	//
	// For the direct-SDK soft-delete path (the normal case where db.Delete(&p)
	// is called with a known record ID) we never need the clause builder at
	// all.  For the WHERE-based fallback we add the necessary clauses manually.

	// Detect an edge model being deleted directly.
	rv := db.Statement.ReflectValue
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.IsValid() && rv.CanAddr() {
		if edge, ok := rv.Addr().Interface().(localModels.EdgeRelation); ok {
			if model, ok := db.Statement.Model.(TypesM.Identifiable); ok && model.GetID() != nil {
				recID := model.GetID()

				// If the edge model has DeletedAt and is not Unscoped, perform soft-delete.
				if hasDeletedAt(db.Statement.Model) && !db.Statement.Unscoped {
					_, err := execTxQuery(db, dialector,
						"UPDATE $id SET deleted_at = time::now(), updated_at = time::now()",
						map[string]interface{}{"id": &recID.RecordID})
					if err != nil {
						db.AddError(err)
						return
					}
					db.RowsAffected = 1
					_ = edge
					return
				}

				// Hard delete for edges without DeletedAt or when Unscoped.
				results, err := execTxQuery(db, dialector,
					"DELETE $id",
					map[string]interface{}{"id": &recID.RecordID})
				if err != nil {
					db.AddError(err)
					return
				}
				if len(*results) > 0 && (*results)[0].Status != "OK" {
					db.AddError(fmt.Errorf("surrealdb delete edge error: %v", (*results)[0]))
					return
				}
				db.RowsAffected = 1
				_ = edge
				return
			}
		}
	}

	if db.Statement.SQL.Len() == 0 {
		db.Statement.SQL.Grow(100)

		if db.Statement.Schema != nil && db.Statement.Schema.LookUpField("DeletedAt") != nil && !db.Statement.Unscoped {
			// Use a direct native SurrealQL query for soft-delete. GORM's clause
			// builder (SoftDeleteDeleteClause + WHERE) generates a malformed
			// statement with two consecutive WHERE keywords that SurrealDB rejects.
			//
			// When db.Delete(&p) is called without db.Model(&p), Statement.Model is
			// nil — fall back to Statement.Dest.
			var softIdent TypesM.Identifiable
			if m, ok := db.Statement.Model.(TypesM.Identifiable); ok {
				softIdent = m
			} else if d, ok := db.Statement.Dest.(TypesM.Identifiable); ok {
				softIdent = d
			}
			if softIdent != nil {
				if id := softIdent.GetID(); id != nil {
					_, qErr := execTxQuery(db, dialector,
						"UPDATE $id SET deleted_at = time::now(), updated_at = time::now()",
						map[string]interface{}{"id": &id.RecordID})
					if qErr != nil {
						db.AddError(qErr)
					} else {
						db.RowsAffected = 1
					}
					return
				}
			}
			// WHERE-based soft delete fallback (no direct ID — e.g. db.Where(...).Delete)
			// Fall through to the clause-based builder below.
			db.Statement.AddClauseIfNotExists(clause.Update{})
			db.Statement.AddClause(clause.Set{
				{Column: clause.Column{Name: "deleted_at"}, Value: time.Now()},
			})
			db.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
		} else {
			// Hard delete: use direct ID if the model exposes one.
			// Also check Dest (when db.Delete(&p) is called without db.Model(&p)).
			var hardIdent TypesM.Identifiable
			if m, ok := db.Statement.Model.(TypesM.Identifiable); ok {
				hardIdent = m
			} else if d, ok := db.Statement.Dest.(TypesM.Identifiable); ok {
				hardIdent = d
			}
			if hardIdent != nil {
				if id := hardIdent.GetID(); id != nil {
					results, err := execTxQuery(db, dialector,
						"DELETE $id",
						map[string]interface{}{"id": &id.RecordID})
					if err != nil {
						db.AddError(err)
						return
					}
					if len(*results) > 0 && (*results)[0].Status != "OK" {
						db.AddError(fmt.Errorf("surrealdb hard delete error: %v", (*results)[0]))
						return
					}
					db.RowsAffected = 1
					return
				}
			}

			// Fallback: build clause-based DELETE (e.g. db.Where(...).Delete)
			db.Statement.AddClauseIfNotExists(clause.Delete{})
			db.Statement.AddClauseIfNotExists(clause.From{})
			db.Statement.BuildClauses = []string{"DELETE", "FROM", "WHERE"}
		}

		db.Statement.AddClauseIfNotExists(clause.Where{})
	}

	db.Statement.Build(db.Statement.BuildClauses...)
	executeSQL(db)
}
