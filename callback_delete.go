package surrealdb

import (
	"fmt"
	"reflect"
	"time"

	"github.com/surrealdb/surrealdb.go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	localModels "github.com/dailaim/surrealdb-gorm/models"
	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

func DeleteCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}

	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema != nil {
		for _, c := range db.Statement.Schema.DeleteClauses {
			db.Statement.AddClause(c)
		}
	}

	// Detect an edge model being deleted directly.
	rv := db.Statement.ReflectValue
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.IsValid() && rv.CanAddr() {
		if edge, ok := rv.Addr().Interface().(localModels.EdgeRelation); ok {
			if model, ok := db.Statement.Model.(TypesM.Identifiable); ok && model.GetID() != nil {
				recID := model.GetID()
				results, err := surrealdb.Query[interface{}](
					db.Statement.Context, dialector.Conn,
					"DELETE $id",
					map[string]interface{}{"id": &recID.RecordID},
				)
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
			db.Statement.AddClauseIfNotExists(clause.Update{})

			if model, ok := db.Statement.Model.(TypesM.Identifiable); ok {
				if id := model.GetID(); id != nil {
					db.Statement.Clauses["UPDATE"] = clause.Clause{
						Name:       "UPDATE",
						Expression: clause.Expr{SQL: id.String()},
					}
				}
			}

			db.Statement.AddClause(clause.Set{
				{Column: clause.Column{Name: "deleted_at"}, Value: time.Now()},
			})
			db.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
		} else {
			db.Statement.AddClauseIfNotExists(clause.Delete{})
			db.Statement.AddClauseIfNotExists(clause.From{})
			db.Statement.BuildClauses = []string{"DELETE", "FROM", "WHERE"}
		}

		db.Statement.AddClauseIfNotExists(clause.Where{})
	}

	db.Statement.Build(db.Statement.BuildClauses...)
	executeSQL(db)
}
