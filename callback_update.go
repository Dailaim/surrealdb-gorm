package surrealdb

import (
	"reflect"
	"time"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
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

			db.Statement.AddClauseIfNotExists(clause.Set{})
			db.Statement.AddClauseIfNotExists(clause.Where{})

			if db.Statement.Schema != nil {
				now := time.Now()
				if field := db.Statement.Schema.LookUpField("UpdatedAt"); field != nil {
					rv := db.Statement.ReflectValue
					for rv.Kind() == reflect.Ptr {
						rv = rv.Elem()
					}
					if rv.Kind() == reflect.Struct {
						if rv.CanAddr() {
							field.Set(db.Statement.Context, db.Statement.ReflectValue, now)
						}
					} else if rv.Kind() == reflect.Map {
						if destMap, ok := db.Statement.Dest.(map[string]interface{}); ok {
							destMap[field.DBName] = now
						}
					}
				}
			}

			if set, ok := db.Statement.Clauses["SET"]; ok {
				if _, ok := set.Expression.(clause.Set); ok {
					var assignments []clause.Assignment

					if destMap, ok := db.Statement.Dest.(map[string]interface{}); ok {
						for k, v := range destMap {
							if k == "id" {
								continue
							}
							assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: k}, Value: v})
						}
					} else if db.Statement.Schema != nil {
						destValue := reflect.ValueOf(db.Statement.Dest)
						for destValue.Kind() == reflect.Ptr {
							destValue = destValue.Elem()
						}

						if destValue.Kind() == reflect.Struct {
							now := time.Now()
							for _, field := range db.Statement.Schema.Fields {
								if field.DBName == "" || field.DBName == "id" {
									continue
								}
								if field.Name == "UpdatedAt" {
									assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: now})
									continue
								}
								if val, isZero := field.ValueOf(db.Statement.Context, destValue); !isZero {
									assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: val})
								}
							}
						}
					}

					if len(assignments) > 0 {
						db.Statement.AddClause(clause.Set(assignments))
					}
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
