package surrealdb

import (
	"reflect"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

func UpdateCallback(db *gorm.DB) {
	// Save edge join tables NOW before building the UPDATE SQL.
	if db.Statement.Schema != nil && db.Error == nil {
		dialector := db.Dialector.(*Dialector)
		rv := db.Statement.ReflectValue
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.Struct {
			for _, rel := range db.Statement.Schema.Relationships.Many2Many {
				if rel.JoinTable == nil {
					continue
				}
				registeredEdge, ok := dialector.FindEdgeTable(rel.JoinTable.Table)
				if !ok {
					continue
				}
				fieldVal := rel.Field.ReflectValueOf(db.Statement.Context, rv)
				if fieldVal.Kind() == reflect.Pointer {
					fieldVal = fieldVal.Elem()
				}
				if fieldVal.Kind() != reflect.Slice || fieldVal.Len() == 0 {
					continue
				}

				var inID *sdkModels.RecordID
				for _, ref := range rel.References {
					if ref.OwnPrimaryKey {
						v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, rv)
						if !isZero {
							inID = extractRecordID(v)
						}
						break
					}
				}
				if inID == nil {
					continue
				}

				for i := 0; i < fieldVal.Len(); i++ {
					elem := fieldVal.Index(i)
					for elem.Kind() == reflect.Pointer {
						elem = elem.Elem()
					}
					var outID *sdkModels.RecordID
					for _, ref := range rel.References {
						if !ref.OwnPrimaryKey && ref.PrimaryValue == "" {
							v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, elem)
							if !isZero {
								outID = extractRecordID(v)
							}
							break
						}
					}
					if outID == nil {
						continue
					}
					rel2 := &surrealdb.Relationship{
						In:       *inID,
						Out:      *outID,
						Relation: sdkModels.Table(registeredEdge),
					}
					if _, err := surrealdb.InsertRelation[interface{}](db.Statement.Context, dialector.Conn, rel2); err != nil {
						db.AddError(err)
						return
					}
				}
			}
		}
	}

	if db.Error == nil {
		if db.Statement.Schema != nil {
			for _, c := range db.Statement.Schema.UpdateClauses {
				db.Statement.AddClause(c)
			}
		}

		if db.Statement.SQL.Len() == 0 {
			db.Statement.SQL.Grow(180)

			handledUpdate := false
			if model, ok := db.Statement.Model.(TypesM.Identifiable); ok {
				if id := model.GetID(); id != nil {
					db.Statement.Clauses["UPDATE"] = clause.Clause{
						Name:       "UPDATE",
						Expression: clause.Expr{SQL: id.String()},
					}
					handledUpdate = true
				}
			}

			if !handledUpdate {
				db.Statement.AddClauseIfNotExists(clause.Update{})
			}

			db.Statement.AddClauseIfNotExists(clause.Where{})

			// Delegate SET assignment computation to GORM's ConvertToAssignments.
			// This handles map/struct dests, auto-update timestamps (updated_at),
			// select/omit columns, and auto-update time fields correctly — avoiding
			// the subtle bugs in our previous manual implementation.
			if _, ok := db.Statement.Clauses["SET"]; !ok {
				if set := callbacks.ConvertToAssignments(db.Statement); len(set) != 0 {
					db.Statement.AddClause(set)
				} else {
					return // nothing to update
				}
			}

			db.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
		}

		if !db.Statement.Unscoped && db.Statement.Schema != nil {
			for _, c := range db.Statement.Schema.UpdateClauses {
				db.Statement.AddClause(c)
			}
		}

		db.Statement.Build(db.Statement.BuildClauses...)
		executeSQL(db)
	}
}
