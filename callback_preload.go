package surrealdb

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/dailaim/surrealdb-gorm/clauses"
)

// handlePreloadAsFetch converts Preloads to FETCH or graph-traversal SELECT expressions.
func handlePreloadAsFetch(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	if len(db.Statement.Preloads) == 0 {
		return
	}

	dialector, _ := db.Dialector.(*Dialector)

	var fetchFields []string
	var graphFields []string

	for name := range db.Statement.Preloads {
		// Check if this is a many2many whose join table is an edge table.
		if dialector != nil && db.Statement.Schema != nil {
			if rel, ok := db.Statement.Schema.Relationships.Relations[name]; ok {
				if rel.Type == "many_to_many" && rel.JoinTable != nil {
					if registeredEdge, found := dialector.FindEdgeTable(rel.JoinTable.Table); found {
						relatedTable := ""
						if rel.FieldSchema != nil {
							relatedTable = rel.FieldSchema.Table
						} else {
							relatedTable = db.NamingStrategy.TableName(name)
						}
						fieldAlias := db.NamingStrategy.ColumnName("", name)

						forward := true
						if rel.FieldSchema != nil && db.Statement.Schema != nil {
							for _, ref := range rel.References {
								if ref.OwnPrimaryKey {
									if ref.ForeignKey != nil && ref.ForeignKey.DBName == "out" {
										forward = false
									}
									break
								}
							}
						}

						var expr string
						if forward {
							expr = fmt.Sprintf("->%s->%s AS %s", registeredEdge, relatedTable, fieldAlias)
						} else {
							expr = fmt.Sprintf("<-%s<-%s AS %s", registeredEdge, relatedTable, fieldAlias)
						}
						graphFields = append(graphFields, expr)
						continue
					}
				}
			}
		}

		// Regular preload → FETCH
		parts := strings.Split(name, ".")
		var dbParts []string
		currentSchema := db.Statement.Schema

		for _, part := range parts {
			mapped := part
			if currentSchema != nil {
				if field := currentSchema.LookUpField(part); field != nil && field.DBName != "" {
					mapped = field.DBName
					if field.Schema != nil {
						currentSchema = field.Schema
					} else {
						currentSchema = nil
					}
				}
			}
			if mapped == part {
				mapped = db.NamingStrategy.ColumnName("", part)
			}
			if mapped == "" {
				continue
			}
			dbParts = append(dbParts, mapped)
		}

		var currentPath string
		for i, dbPart := range dbParts {
			if i == 0 {
				currentPath = dbPart
			} else {
				currentPath = currentPath + "." + dbPart
			}
			fetchFields = append(fetchFields, currentPath)
		}
	}

	seen := make(map[string]bool)
	var uniqueFetch []string
	for _, f := range fetchFields {
		if !seen[f] {
			seen[f] = true
			uniqueFetch = append(uniqueFetch, f)
		}
	}

	if len(uniqueFetch) > 0 {
		db.Statement.AddClause(clauses.Fetch{Fields: uniqueFetch})
	}
	if len(graphFields) > 0 {
		db.Statement.AddClause(clauses.GraphSelect{Fields: graphFields})
	}
	db.Statement.Preloads = nil
}
