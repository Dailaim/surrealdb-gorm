package surrealdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dailaim/surrealdb-gorm/clauses"
	localModels "github.com/dailaim/surrealdb-gorm/models"
	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

func RegisterCallbacks(db *gorm.DB) {
	// Create
	db.Callback().Create().Replace("gorm:create", CreateCallback)

	// Update
	db.Callback().Update().Replace("gorm:update", UpdateCallback)

	// Delete — edge association helpers run before the main delete
	db.Callback().Delete().Before("gorm:delete").Register("surreal:edge_assoc_delete", edgeAssocDeleteCallback)
	db.Callback().Delete().Before("gorm:delete").Register("surreal:edge_assoc_replace", edgeAssocReplaceCallback)
	db.Callback().Delete().Replace("gorm:delete", DeleteCallback)

	// Row (used by Association.Count)
	db.Callback().Row().Before("gorm:row").Register("surreal:edge_assoc_count", edgeAssocCountCallback)

	// Query
	db.Callback().Query().Before("gorm:query").Register("surreal:handle_preload", handlePreloadAsFetch)

	db.Callback().Query().Replace("gorm:query", QueryCallback)
}

func handlePreloadAsFetch(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	// Convert Preloads to FETCH or graph-traversal SELECT expressions.
	if len(db.Statement.Preloads) > 0 {
		dialector, _ := db.Dialector.(*Dialector)

		var fetchFields []string
		var graphFields []string

		for name := range db.Statement.Preloads {
			// Check if this is a many2many whose join table is an edge table.
			// If so, emit a graph-traversal SELECT expression instead of FETCH.
			if dialector != nil && db.Statement.Schema != nil {
				if rel, ok := db.Statement.Schema.Relationships.Relations[name]; ok {
					if rel.Type == "many_to_many" && rel.JoinTable != nil {
						// FindEdgeTable handles both exact match and pluralized form so that
						// many2many:wishlist and many2many:wishlists both work.
						if registeredEdge, found := dialector.FindEdgeTable(rel.JoinTable.Table); found {
							relatedTable := ""
							if rel.FieldSchema != nil {
								relatedTable = rel.FieldSchema.Table
							} else {
								relatedTable = db.NamingStrategy.TableName(name)
							}
							// AS alias must match the Go field name (case-insensitive for json.Unmarshal).
							fieldAlias := db.NamingStrategy.ColumnName("", name)

							// Determine traversal direction.
							// In a SurrealDB edge: RELATE in->edge->out.
							// • If the owning model is the "in" side → forward:  ->edge->relatedTable
							// • If the owning model is the "out" side → reverse: <-edge<-relatedTable
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
					continue // skip unmappable parts (e.g. many2many fields with no DBName)
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

		// Deduplicate FETCH fields
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
		// Clear Preloads so GORM doesn't try to load them again
		db.Statement.Preloads = nil
	}
}

func CreateCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}

	dialector := db.Dialector.(*Dialector)
	if dialector.Conn == nil {
		db.AddError(errors.New("surrealdb connection not initialized"))
		return
	}

	if db.Statement.Schema != nil {
		if !db.Statement.Unscoped {
			for _, c := range db.Statement.Schema.CreateClauses {
				db.Statement.AddClause(c)
			}
		}
	}

	reflectValue := db.Statement.ReflectValue

	// Detect Edge models (embed models.Edge[T,U]) and route through InsertRelation
	destVal := reflectValue
	if destVal.Kind() == reflect.Pointer {
		destVal = destVal.Elem()
	}

	// Path A: struct explicitly implements EdgeRelation (e.g. manual db.Create(&Wishlist{...}))
	if destVal.IsValid() && destVal.CanAddr() {
		if edge, ok := destVal.Addr().Interface().(localModels.EdgeRelation); ok {
			inID := edge.EdgeIn()
			outID := edge.EdgeOut()
			if inID == nil || outID == nil {
				db.AddError(errors.New("edge model must have both In and Out IDs set"))
				return
			}

			// Collect extra fields (anything that's not in/out/id/timestamps) into Data.
			extraData := make(map[string]interface{})
			if db.Statement.Schema != nil {
				skipFields := map[string]bool{"id": true, "in": true, "out": true,
					"created_at": true, "updated_at": true, "deleted_at": true}
				for _, field := range db.Statement.Schema.Fields {
					if field.DBName == "" || skipFields[field.DBName] {
						continue
					}
					// Skip embedded/anonymous struct fields (e.g. EdgeSchemaless, Schemaless).
					// Their promoted children are already included as individual fields.
					if field.StructField.Anonymous {
						continue
					}
					val, isZero := field.ValueOf(db.Statement.Context, reflectValue)
					if !isZero {
						extraData[field.DBName] = val
					}
				}
			}

			rel := &surrealdb.Relationship{
				In:       inID.RecordID,
				Out:      outID.RecordID,
				Relation: sdkModels.Table(db.Statement.Table),
				Data:     extraData,
			}
			result, err := surrealdb.InsertRelation[interface{}](db.Statement.Context, dialector.Conn, rel)
			if err != nil {
				db.AddError(err)
				return
			}
			if result != nil {
				val := reflect.ValueOf(result)
				if val.Kind() == reflect.Pointer {
					val = val.Elem()
				}
				if val.Kind() == reflect.Interface {
					val = val.Elem()
				}
				var single interface{}
				if val.Kind() == reflect.Slice && val.Len() > 0 {
					single = val.Index(0).Interface()
				} else if val.IsValid() {
					single = val.Interface()
				}
				if single != nil {
					b, err := json.Marshal(single)
					if err == nil {
						_ = json.Unmarshal(b, db.Statement.Dest)
					}
				}
			}
			return
		}
	}

	// Path B: GORM auto-generated join table struct (Association.Append for many2many edges).
	// The struct is not Wishlist but an anonymous struct with two FK fields.
	// Detect by checking if the target table is a registered edge table.
	if registeredName, isEdge := dialector.FindEdgeTable(db.Statement.Table); isEdge && db.Statement.Schema != nil {
		// Collect the two FK values in schema order — first FK = in, second FK = out.
		var fkVals []*sdkModels.RecordID
		for _, field := range db.Statement.Schema.Fields {
			if field.DBName == "" || field.DBName == "id" {
				continue
			}
			val, isZero := field.ValueOf(db.Statement.Context, reflectValue)
			if isZero {
				continue
			}
			if rid := extractRecordID(val); rid != nil {
				fkVals = append(fkVals, rid)
				if len(fkVals) == 2 {
					break
				}
			}
		}
		if len(fkVals) == 2 {
			rel := &surrealdb.Relationship{
				In:       *fkVals[0],
				Out:      *fkVals[1],
				Relation: sdkModels.Table(registeredName),
			}
			if _, err := surrealdb.InsertRelation[interface{}](db.Statement.Context, dialector.Conn, rel); err != nil {
				db.AddError(err)
				return
			}
			db.RowsAffected = 1
			return
		}
	}

	if reflectValue.Kind() == reflect.Slice || reflectValue.Kind() == reflect.Array {
		// Batch insert logic
		// For simplicity, we assume generic Insert works or we iterate.
		// sdkModels.Table just wraps string.
		// Batch insert logic
		// For simplicity, we assume generic Insert works or we iterate.
		// sdkModels.Table just wraps string.
		table := sdkModels.Table(db.Statement.Table)

		// Force JSON marshaling to generic map/slice to respect custom MarshalJSON (e.g. DeletedAt)
		// because SDK might use CBOR or ignore MarshalJSON for structs.
		var data interface{}
		b, err := json.Marshal(db.Statement.Dest)
		if err != nil {
			db.AddError(err)
			return
		}
		if err := json.Unmarshal(b, &data); err != nil {
			db.AddError(err)
			return
		}

		_, err = surrealdb.Insert[interface{}](db.Statement.Context, dialector.Conn, table, data)
		if err != nil {
			db.AddError(err)
			return
		}
	} else {
		// Single insert
		var whatTable = db.Statement.Table
		var whatRecord *sdkModels.RecordID

		// Handle Timestamps
		if db.Statement.Schema != nil {
			now := time.Now()
			if field := db.Statement.Schema.LookUpField("CreatedAt"); field != nil {
				// If zero, set it
				_, isZero := field.ValueOf(db.Statement.Context, db.Statement.ReflectValue)
				if isZero {
					field.Set(db.Statement.Context, db.Statement.ReflectValue, now)
				}
			}
			if field := db.Statement.Schema.LookUpField("UpdatedAt"); field != nil {
				// Should always update UpdatedAt on create? Yes usually.
				// field.Set(db.Statement.Context, db.Statement.ReflectValue, now)

				// Only if zero? GORM behavior is it updates it.
				_, isZero := field.ValueOf(db.Statement.Context, db.Statement.ReflectValue)
				if isZero {
					field.Set(db.Statement.Context, db.Statement.ReflectValue, now)
				}
			}
		}

		// Handle LinkVal
		if model, ok := db.Statement.Model.(TypesM.Identifiable); ok {
			if model.GetID() != nil {
				whatRecord = &model.GetID().RecordID
			}
		}

		// Prepare data for SurrealDB
		var createData interface{} = db.Statement.Dest

		// If Schema is available, convert struct to map using DBName to respect GORM tags
		if db.Statement.Schema != nil && db.Statement.ReflectValue.Kind() == reflect.Struct {
			dataMap := make(map[string]interface{})
			for _, field := range db.Statement.Schema.Fields {
				if field.DBName == "" {
					continue
				}
				// Get value
				val, isZero := field.ValueOf(db.Statement.Context, db.Statement.ReflectValue)
				// If isZero, we usually skip unless Default Value?
				// GORM usually handles defaults before?
				// If invalid/zero and not nullable?
				// For now simple dump:
				if !isZero {
					dataMap[field.DBName] = val
				}
			}
			createData = dataMap
		}

		var created *interface{}
		var err error
		// fmt.Printf("Dest: %#v\n", db.Statement.Dest)
		// fmt.Printf("Dest TYPE: %#v\n", reflect.TypeOf(db.Statement.Dest))

		// fmt.Printf("\n\n\n\nSchema: %#v\n\n\n\n", db.Statement.Dest)
		// if reflect.ValueOf(db.Statement.Dest).Kind() == reflect.Ptr {
		// 	fmt.Printf("\n\n\n\nSchema: %#v\n\n\n\n", reflect.ValueOf(db.Statement.Dest).Elem().Interface())
		// } else {
		// 	fmt.Printf("\n\n\n\nSchema: %#v\n\n\n\n", db.Statement.Dest)
		// }

		if whatRecord != nil {
			// If ID is specified, treat as Upsert/Update.
			// surrealdb.Update overwrites the record content, matching GORM Save semantics.
			created, err = surrealdb.Update[interface{}](db.Statement.Context, dialector.Conn, *whatRecord, createData)
		} else {
			created, err = surrealdb.Create[interface{}](db.Statement.Context, dialector.Conn, sdkModels.Table(whatTable), createData)
		}

		if err != nil {
			db.AddError(err)
			return
		}

		// The SDK takes `data any`. If data is pointer, it reads it. It returns new object.

		// Optimally we map `created` back to `db.Statement.Dest`
		if created != nil {
			// Unwrap array if needed
			val := reflect.ValueOf(created)
			if val.Kind() == reflect.Pointer {
				val = val.Elem()
			}
			// *interface{} unwraps to Interface kind; unwrap once more to reach concrete value
			if val.Kind() == reflect.Interface {
				val = val.Elem()
			}
			var dataToUnmarshal interface{}
			if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
				if val.Len() > 0 {
					dataToUnmarshal = val.Index(0).Interface()
				}
			} else {
				dataToUnmarshal = val.Interface()
			}

			// If Schema available, manually map back to respect GORM tags (DBName -> Field)
			// because json.Unmarshal expects JSON tags which might differ.
			if db.Statement.Schema != nil && db.Statement.ReflectValue.Kind() == reflect.Struct {
				// dataToUnmarshal should be map[string]interface{} (from JSON/CBOR decode usually)
				// If it is a struct or something else, we fallback.
				// SurrealDB driver (using interface{} generic) returns mostly map[string]interface{}.

				// Helper to convert to map if needed (e.g. if driver returns generic struct or custom type?)
				// Usually it's better to rely on JSON marshal/unmarshal for type conversion,
				// but here we want Key mapping (DBName -> Field).

				// Let's marshall to bytes first to ensure we have a common format (JSON)
				bytes, err := json.Marshal(dataToUnmarshal)
				if err == nil {
					var resultMap map[string]interface{}
					if err := json.Unmarshal(bytes, &resultMap); err == nil {
						// Iterate Schema Fields and populate from resultMap using DBName
						for _, field := range db.Statement.Schema.Fields {
							if val, ok := resultMap[field.DBName]; ok {
								// Found value for DBName.
								// We need to set it to the struct field.
								// Direct field.Set might fail if types mismatch (e.g. map vs struct).
								// We use intermediate JSON marshal/unmarshal to handle type conversion reliably.

								// Get field value (addressable)
								fieldVal := db.Statement.ReflectValue.FieldByIndex(field.StructField.Index)
								if fieldVal.CanAddr() {
									// Marshal the value from map to JSON bytes
									valBytes, err := json.Marshal(val)
									if err == nil {
										// Unmarshal JSON bytes into the field address
										if err := json.Unmarshal(valBytes, fieldVal.Addr().Interface()); err != nil {
											// Fallback: try direct set if Unmarshal failed (unlikely for matched types)
											// field.Set(db.Statement.Context, db.Statement.ReflectValue, val)
											// Log error?
											// fmt.Printf("Error unmarshaling field %s: %v\n", field.Name, err)
										}
									}
								}
							}
						}
						// Return to skip the fallback full-struct unmarshal
						return
					}
				}
			}

			// Fallback to strict JSON unmarshal if Schema not available or above failed
			bytes, err := json.Marshal(dataToUnmarshal)
			if err == nil {
				// Unmarshal into Dest
				if err := json.Unmarshal(bytes, db.Statement.Dest); err != nil {
					db.AddError(fmt.Errorf("failed to unmarshal create result: %v", err))
				}
			}
		}
	}

	db.RowsAffected = 1
}

func QueryCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}

	// Manual Soft Delete check
	if db.Statement.Schema != nil && db.Statement.Schema.LookUpField("DeletedAt") != nil && !db.Statement.Unscoped {
		// Check if map/value conditions already include it?
		// GORM might add it to WHERE if using Find, but we replace callback.
		// We safely add it if not present.
		// clause.Where{Exprs: ...}
		// Easy way: AddClause
		db.Statement.AddClause(clause.Where{
			Exprs: []clause.Expression{
				clause.Expr{SQL: "`deleted_at` IS NULL OR `deleted_at` IS NONE"},
			},
		})
	}

	// Optimization: If querying by ID only, use SELECT * FROM ID
	// Check if WHERE clause contains only ID
	if db.Statement.Schema != nil {
		// GORM parses WHERE conditions.
		// It's hard to check parsed conditions easily here without traversing clauses.
		// However, we can check if primary key is set in Statement.Dest (if it was initialized) or if Vars contains the ID matching PK type.

		// Simpler check: If SQL contains `WHERE "table"."id" = $1` (or similar) and LIMIT 1?
		// Let's rely on standard build first.
	}

	// Ensure default clauses for SELECT if missing
	if len(db.Statement.BuildClauses) == 0 {
		db.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "GROUP BY", "ORDER BY", "LIMIT", "FOR", "INFO", "FETCH"}
	}
	if _, ok := db.Statement.Clauses["SELECT"]; !ok {
		// If there are graph-traversal fields (from many2many edge preloads), include them in SELECT.
		selectSQL := "*"
		if gs, ok := db.Statement.Clauses["GRAPH_SELECT"]; ok {
			if gsExpr, ok := gs.Expression.(clauses.GraphSelect); ok {
				for _, f := range gsExpr.Fields {
					selectSQL += ", " + f
				}
			}
		}
		db.Statement.AddClause(clause.Select{Expression: clause.Expr{SQL: selectSQL}})
	} else if gs, ok := db.Statement.Clauses["GRAPH_SELECT"]; ok {
		// SELECT already set (e.g. by GORM's First) – inject graph expressions into it.
		if gsExpr, ok := gs.Expression.(clauses.GraphSelect); ok && len(gsExpr.Fields) > 0 {
			extra := strings.Join(gsExpr.Fields, ", ")
			selClause := db.Statement.Clauses["SELECT"]
			if expr, ok := selClause.Expression.(clause.Select); ok {
				if sqlExpr, ok := expr.Expression.(clause.Expr); ok {
					expr.Expression = clause.Expr{SQL: sqlExpr.SQL + ", " + extra}
				} else {
					expr.Expression = clause.Expr{SQL: "*, " + extra}
				}
				selClause.Expression = expr
				db.Statement.Clauses["SELECT"] = selClause
			}
		}
	}
	if _, ok := db.Statement.Clauses["FROM"]; !ok && db.Statement.Table != "" {
		db.Statement.AddClause(clause.From{Tables: []clause.Table{{Name: db.Statement.Table}}})
	}

	db.Statement.Build(db.Statement.BuildClauses...)
	if db.Error != nil {
		return
	}

	// Check if we can optimize generated SQL
	// Example: SELECT * FROM `users` WHERE `users`.`id` = $p1 ...
	// Transform to: SELECT * FROM $p1
	// We need to verify if $p1 is indeed a RecordID.

	optimizeFindByID(db)

	executeSQL(db)
}

func optimizeFindByID(db *gorm.DB) {
	if len(db.Statement.Vars) >= 1 {
		// Check if first var is RecordID
		if _, ok := db.Statement.Vars[0].(*sdkModels.RecordID); ok {
			sql := db.Statement.SQL.String()
			// Naive check: does it look like a find by ID?
			// SELECT * FROM `users` WHERE `users`.`id` = $p1 ...
			// We want: SELECT * FROM `users:id` ...

			if db.Statement.Table != "" {
				// Debug SQL inputs
				// fmt.Printf("DEBUG OPTIMIZE INPUT SQL: %s\n", sql)

				quotedTable := fmt.Sprintf("`%s`", db.Statement.Table)
				targetFrom := fmt.Sprintf("FROM %s", quotedTable)
				newFrom := "FROM $p1"

				if strings.Contains(sql, targetFrom) && (strings.Contains(sql, "`id` = $p1") || strings.Contains(sql, "id = $p1")) {
					// Replace FROM
					sql = strings.Replace(sql, targetFrom, newFrom, 1)

					// Remove the ID condition from WHERE
					// Pattern: `?table`?.`?id`? = $p1 AND?

					// We use simple replacements to strip the ID check.

					// 1. "id = $p1 AND " (Leading condition)
					// If we remove this, we must ensure we don't remove "WHERE" if it's "WHERE id = $p1 AND".
					// "WHERE id = $p1 AND " -> "WHERE "
					sql = strings.ReplaceAll(sql, fmt.Sprintf("WHERE %s.`id` = $p1 AND ", quotedTable), "WHERE ")
					sql = strings.ReplaceAll(sql, "WHERE `id` = $p1 AND ", "WHERE ")
					sql = strings.ReplaceAll(sql, "WHERE id = $p1 AND ", "WHERE ")

					// " AND id = $p1" (Trailing or middle condition)
					// Just remove it.
					sql = strings.ReplaceAll(sql, fmt.Sprintf(" AND %s.`id` = $p1", quotedTable), "")
					sql = strings.ReplaceAll(sql, " AND `id` = $p1", "")
					sql = strings.ReplaceAll(sql, " AND id = $p1", "")

					// "id = $p1 AND " (Middle condition without WHERE prefix?)
					// This is dangerous if not prefixed. GORM build order implies "WHERE ... AND ... AND ...".
					// We covered "WHERE id...".
					// If "WHERE other AND id = $p1 AND other", the middle one is covered by " AND id = $p1".
					// If "WHERE other AND id = $p1", covered by " AND id = $p1".
					// If "WHERE id = $p1 AND other", covered by "WHERE id = $p1 AND " -> "WHERE ".

					// 3. "id = $p1" (Lone condition)
					sql = strings.ReplaceAll(sql, fmt.Sprintf("WHERE %s.`id` = $p1", quotedTable), "")
					sql = strings.ReplaceAll(sql, "WHERE `id` = $p1", "")
					sql = strings.ReplaceAll(sql, "WHERE id = $p1", "")
					sql = strings.ReplaceAll(sql, "AND id = $p1 AND ", "AND")

					// Remove ORDER BY `table`.`id` or `id` which is redundant for single record access
					sql = strings.ReplaceAll(sql, fmt.Sprintf("ORDER BY %s.`id`", quotedTable), "")
					sql = strings.ReplaceAll(sql, "ORDER BY `id`", "")

					// Note: If we had "WHERE id = $p1 AND other", #1 handled it -> "WHERE other"
					// If we had "WHERE other AND id = $p1", #2 handled it -> "WHERE other"
					// If we had "WHERE id = $p1", #3 handles it -> "" (Empty string)

					// Verify if we didn't leave a dangling WHERE match that wasn't covered (e.g. funny spacing).
					// This is a naive heuristic. GORM usually builds consistently.
					sql = strings.ReplaceAll(sql, "  ", " ")

					db.Statement.SQL.Reset()
					db.Statement.SQL.WriteString(sql)
				}
			}

		}
	}
}

func UpdateCallback(db *gorm.DB) {
	// If this is a Many2Many update triggered by Association.Append, the model's
	// relationship fields (e.g. Products) are already set in ReflectValue.
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
				// Get the value of the relationship field (e.g. Products slice)
				fieldVal := rel.Field.ReflectValueOf(db.Statement.Context, rv)
				if fieldVal.Kind() == reflect.Pointer {
					fieldVal = fieldVal.Elem()
				}
				if fieldVal.Kind() != reflect.Slice || fieldVal.Len() == 0 {
					continue
				}

				// Get the owner's primary key value (the "in" side)
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

				// Iterate the related records (the "out" side)
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

		// Manual SQL Generation for Update
		if db.Statement.SQL.Len() == 0 {
			db.Statement.SQL.Grow(180)

			// Handle Update Clause (Table vs Record ID)
			handledUpdate := false
			if model, ok := db.Statement.Model.(TypesM.Identifiable); ok {
				// Use explicit record ID
				db.Statement.Clauses["UPDATE"] = clause.Clause{
					Name:       "UPDATE",
					Expression: clause.Expr{SQL: model.GetID().String()},
				}
				handledUpdate = true
			}

			if !handledUpdate {
				db.Statement.AddClauseIfNotExists(clause.Update{})
			}

			db.Statement.AddClauseIfNotExists(clause.Set{})
			db.Statement.AddClauseIfNotExists(clause.Where{})

			if db.Statement.Schema != nil {
				now := time.Now()
				if field := db.Statement.Schema.LookUpField("UpdatedAt"); field != nil {
					// Handle Struct vs Map
					rv := db.Statement.ReflectValue
					for rv.Kind() == reflect.Ptr {
						rv = rv.Elem()
					}

					if rv.Kind() == reflect.Struct {
						// Check if we can set
						// We need to check if the field within the struct is settable.
						// GORM field.Set uses field.ReflectValueOf which returns the field Value.
						// We can't easily check CanSet via field helper without duplicating logic.
						// But we know if 'rv' is not addressable (passed by value), we can't set fields.
						if rv.CanAddr() {
							field.Set(db.Statement.Context, db.Statement.ReflectValue, now)
						}
						// If not addressable, we simply skip setting the struct field.
						// We will ensure it is added to SET clause below.

					} else if rv.Kind() == reflect.Map {
						// Assuming map[string]interface{} as GORM usually uses for Updates
						if destMap, ok := db.Statement.Dest.(map[string]interface{}); ok {
							destMap[field.DBName] = now
						}
					}
				}
			}

			// Manually populate SET clause if empty
			if set, ok := db.Statement.Clauses["SET"]; ok {
				if _, ok := set.Expression.(clause.Set); ok {
					var assignments []clause.Assignment

					if destMap, ok := db.Statement.Dest.(map[string]interface{}); ok {
						for k, v := range destMap {
							// Filter out ID from updates
							if k == "id" {
								continue
							}
							assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: k}, Value: v})
						}
					} else if db.Statement.Schema != nil {
						// Struct case
						destValue := reflect.ValueOf(db.Statement.Dest)
						for destValue.Kind() == reflect.Ptr {
							destValue = destValue.Elem()
						}

						if destValue.Kind() == reflect.Struct {
							now := time.Now() // Use fresh time or consistent? Consistent is better but okay.
							for _, field := range db.Statement.Schema.Fields {
								if field.DBName == "" {
									continue
								}
								// Skip primary key usually? Or allow updating it? GORM usually allows unless restricted.
								// But we shouldn't update ID if it's the matcher.
								if field.DBName == "id" {
									continue
								}

								// Special UpdatedAt handling
								if field.Name == "UpdatedAt" {
									// Always set UpdatedAt
									assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: now})
									continue
								}

								// Check if value is zero (for Updates, we usually skip zero values unless Select/Omit used)
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

func DeleteCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}

	// If this is a direct db.Delete(&edgeRecord) call, handle it via the SDK.
	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema != nil {
		for _, c := range db.Statement.Schema.DeleteClauses {
			db.Statement.AddClause(c)
		}
	}

	// Detect an edge model (implements EdgeRelation) being deleted directly.
	rv := db.Statement.ReflectValue
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.IsValid() && rv.CanAddr() {
		if edge, ok := rv.Addr().Interface().(localModels.EdgeRelation); ok {
			if model, ok := db.Statement.Model.(TypesM.Identifiable); ok && model.GetID() != nil {
				// Hard-delete the edge record by ID
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
				_ = edge // suppress unused warning
				return
			}
		}
	}

	if db.Statement.SQL.Len() == 0 {
		db.Statement.SQL.Grow(100)

		// Soft Delete check
		if db.Statement.Schema != nil && db.Statement.Schema.LookUpField("DeletedAt") != nil && !db.Statement.Unscoped {
			db.Statement.AddClauseIfNotExists(clause.Update{}) // Switch to UPDATE

			if model, ok := db.Statement.Model.(TypesM.Identifiable); ok {
				db.Statement.Clauses["UPDATE"] = clause.Clause{
					Name:       "UPDATE",
					Expression: clause.Expr{SQL: model.GetID().String()},
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

	// Build SQL
	db.Statement.Build(db.Statement.BuildClauses...)

	executeSQL(db)
}

func executeSQL(db *gorm.DB) {
	dialector := db.Dialector.(*Dialector)
	sql := db.Statement.SQL.String()

	// If this is an edge table operation, map the table name to its canonical form
	// (e.g. many2many:wishlist -> wishlists)
	actualTable := db.Statement.Table
	if actualTable != "" {
		if canonical, ok := dialector.FindEdgeTable(actualTable); ok {
			sql = strings.ReplaceAll(sql, fmt.Sprintf("`%s`", actualTable), fmt.Sprintf("`%s`", canonical))
			sql = strings.ReplaceAll(sql, fmt.Sprintf("FROM %s", actualTable), fmt.Sprintf("FROM `%s`", canonical))
			actualTable = canonical
		}
	}

	// Replace standard SQL <> with SurrealDB !=
	sql = strings.ReplaceAll(sql, "<>", "!=")

	// Hack: Rewrite association count queries for edge tables
	if strings.Contains(sql, "SELECT count(*)") && strings.Contains(sql, " JOIN ") {
		parts := strings.Split(sql, " JOIN ")
		if len(parts) > 1 {
			joinPart := strings.TrimSpace(parts[1])
			tableName := strings.Split(joinPart, " ")[0]
			tableName = strings.ReplaceAll(tableName, "`", "")
			
			if canonical, ok := dialector.FindEdgeTable(tableName); ok {
				var ownerField string
				if strings.Contains(sql, ".`in` = ") {
					ownerField = "in"
				} else if strings.Contains(sql, ".`out` = ") {
					ownerField = "out"
				}
				if ownerField != "" {
					idx := strings.Index(sql, ".`"+ownerField+"` = $p")
					if idx != -1 {
						pstart := idx + len(".`"+ownerField+"` = ")
						pend := pstart
						for pend < len(sql) && (sql[pend] == '$' || sql[pend] == 'p' || (sql[pend] >= '0' && sql[pend] <= '9')) {
							pend++
						}
						param := sql[pstart:pend]
						sql = fmt.Sprintf("SELECT count() FROM `%s` WHERE `%s` = %s GROUP ALL", canonical, ownerField, param)
					}
				}
			}
		}
	}

	// Hack: Remove table prefixes from SQL
	if actualTable != "" {
		quotedTable := fmt.Sprintf("`%s`.", actualTable)
		sql = strings.ReplaceAll(sql, quotedTable, "")
		// Also handle "table." without backticks just in case if users write raw sql
		sql = strings.ReplaceAll(sql, fmt.Sprintf("%s.", actualTable), "")
	}

	// Hack: Remove IS NULL check for Soft Delete compatibility
	sql = strings.ReplaceAll(sql, " AND (`deleted_at` IS NULL)", "")

	// Filter out "id" from UPDATE SET clause (SurrealDB error: specified record conflict)
	if strings.HasPrefix(strings.TrimSpace(sql), "UPDATE") {
		// Clean up soft delete checks for surrealdb specific syntax (IS NONE)
		sql = strings.ReplaceAll(sql, "`deleted_at` IS NULL", "(`deleted_at` IS NULL OR `deleted_at` IS NONE)")
		if actualTable != "" {
			sql = strings.ReplaceAll(sql, fmt.Sprintf("`%s`.`deleted_at` IS NULL", actualTable), fmt.Sprintf("(`%s`.`deleted_at` IS NULL OR `%s`.`deleted_at` IS NONE)", actualTable, actualTable))
		}
	}

	vars := db.Statement.Vars

	// Map slice vars to map for SurrealDB
	params := make(map[string]interface{})

	for i, v := range vars {
		// Unwrap *types.RecordID (project wrapper) to the native SDK RecordID
		// so that the SDK serializes it correctly over CBOR/JSON.
		if rid, ok := v.(*TypesM.RecordID); ok && rid != nil {
			native := rid.RecordID
			params[fmt.Sprintf("p%d", i+1)] = &native
			continue
		}
		if rid, ok := v.(TypesM.RecordID); ok {
			native := rid.RecordID
			params[fmt.Sprintf("p%d", i+1)] = &native
			continue
		}

		// Handle RecordID manually if needed
		if rid, ok := v.(*sdkModels.RecordID); ok {
			params[fmt.Sprintf("p%d", i+1)] = rid
			continue
		}

		// Handle json.Marshaler (fix for DeletedAt object persistence)
		if m, ok := v.(json.Marshaler); ok {
			if b, err := m.MarshalJSON(); err == nil {
				var iv interface{}
				if err := json.Unmarshal(b, &iv); err == nil {
					params[fmt.Sprintf("p%d", i+1)] = iv
					continue
				}
			}
		}

		params[fmt.Sprintf("p%d", i+1)] = v
	}

	// Execute
	results, err := surrealdb.Query[interface{}](db.Statement.Context, dialector.Conn, sql, params)
	if err != nil {
		db.AddError(err)
		return
	}

	// bytes, err := json.Marshal(results)
	// if err != nil {
	// 	db.AddError(err)
	// 	return
	// }
	// fmt.Printf("DEBUG RES: %s\n", string(bytes))

	if len(*results) > 0 {
		res := (*results)[0]

		// Debug Result
		// fmt.Printf("DEBUG RESULT: %+v\n", res)

		if res.Status != "OK" {
			db.AddError(fmt.Errorf("surrealdb query error: %v", res))
			return
		}

		// Calculate rows affected
		// SurrealDB returns the array of affected/created/selected records in Result.
		var count int64 = 0
		if res.Result != nil {
			val := reflect.ValueOf(res.Result)
			if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
				count = int64(val.Len())
			} else {
				// Non-empty result (single object?)
				count = 1
			}
		}
		db.RowsAffected = count

		// Unmarshal if Dest provided
		if db.Statement.Dest != nil && count > 0 {
			var dataToUnmarshal interface{} = res.Result

			// Check if Dest is not a slice/array pointer
			destVal := reflect.ValueOf(db.Statement.Dest)
			if destVal.Kind() == reflect.Ptr {
				for destVal.Kind() == reflect.Ptr {
					destVal = destVal.Elem()
				}
			} else {
				// Dest is not a pointer, cannot unmarshal into it
				return
			}

			if destVal.Kind() != reflect.Slice && destVal.Kind() != reflect.Array {
				// Dest is single struct/value.
				// If Result is slice, take first element.
				resVal := reflect.ValueOf(res.Result)
				if resVal.Kind() == reflect.Slice || resVal.Kind() == reflect.Array {
					if resVal.Len() > 0 {
						dataToUnmarshal = resVal.Index(0).Interface()
					}
				}
			}

			// Custom mapping for Count queries where Dest is an integer pointer
			// and SurrealDB returns {"count": X}
			if destVal.CanSet() && (destVal.Kind() == reflect.Int64 || destVal.Kind() == reflect.Int) {
				if m, ok := dataToUnmarshal.(map[string]interface{}); ok {
					if c, ok := m["count"]; ok {
						if cFloat, ok := c.(float64); ok {
							destVal.SetInt(int64(cFloat))
							return
						}
					}
				}
			}

			bytes, err := json.Marshal(dataToUnmarshal)
			if err == nil {
				// Debug JSON
				// fmt.Printf("DEBUG SELECT JSON: %s\n", string(bytes))

				// Step 1: Full json.Unmarshal into Dest – handles json tags, case-insensitive
				// matching, and graph-traversal results (e.g. "products" → Products).
				_ = json.Unmarshal(bytes, db.Statement.Dest)

				// Step 2: Overlay fields that have a custom DBName (gorm column tag) different
				// from their JSON key, so they are correctly populated even when the DB key
				// differs from the json tag.
				if db.Statement.Schema != nil && destVal.Kind() == reflect.Struct {
					var resultMap map[string]interface{}
					if err := json.Unmarshal(bytes, &resultMap); err == nil {
						for _, field := range db.Statement.Schema.Fields {
							if field.DBName == "" {
								continue
							}
							if val, ok := resultMap[field.DBName]; ok {
								fieldVal := db.Statement.ReflectValue.FieldByIndex(field.StructField.Index)
								if fieldVal.CanAddr() {
									valBytes, err := json.Marshal(val)
									if err == nil {
										_ = json.Unmarshal(valBytes, fieldVal.Addr().Interface())
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// --------------------------------------------------------------------------
// Edge Association callbacks
// --------------------------------------------------------------------------

// edgeAssocDeleteCallback handles Association("X").Delete(&y) for edge tables.
// GORM routes association deletes through the delete pipeline with a special
// flag in Statement.Settings ("gorm:association:delete"). We detect this and,
// if the join table is a registered edge, delete the matching edge records via
// DELETE FROM <edgeTable> WHERE in = $in AND out = $out.
func edgeAssocDeleteCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	// GORM sets this key when running association delete
	assocMode, _ := db.Statement.Settings.Load("gorm:association:delete")
	if assocMode == nil {
		return
	}
	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema == nil {
		return
	}
	for _, rel := range db.Statement.Schema.Relationships.Many2Many {
		if rel.JoinTable == nil {
			continue
		}
		registeredEdge, ok := dialector.FindEdgeTable(rel.JoinTable.Table)
		if !ok {
			continue
		}
		// Get owner primary key (in)
		rv := db.Statement.ReflectValue
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
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
		// Collect out IDs from Statement.Dest (the records being disassociated)
		destVal := reflect.ValueOf(db.Statement.Dest)
		for destVal.Kind() == reflect.Pointer {
			destVal = destVal.Elem()
		}
		var outIDs []*sdkModels.RecordID
		if destVal.Kind() == reflect.Slice {
			for i := 0; i < destVal.Len(); i++ {
				elem := destVal.Index(i)
				for elem.Kind() == reflect.Pointer {
					elem = elem.Elem()
				}
				for _, ref := range rel.References {
					if !ref.OwnPrimaryKey && ref.PrimaryValue == "" {
						v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, elem)
						if !isZero {
							if rid := extractRecordID(v); rid != nil {
								outIDs = append(outIDs, rid)
							}
						}
						break
					}
				}
			}
		} else if destVal.IsValid() {
			for _, ref := range rel.References {
				if !ref.OwnPrimaryKey && ref.PrimaryValue == "" {
					v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, destVal)
					if !isZero {
						if rid := extractRecordID(v); rid != nil {
							outIDs = append(outIDs, rid)
						}
					}
					break
				}
			}
		}
		for _, outID := range outIDs {
			results, err := surrealdb.Query[interface{}](
				db.Statement.Context, dialector.Conn,
				fmt.Sprintf("DELETE %s WHERE in = $in AND out = $out", registeredEdge),
				map[string]interface{}{"in": inID, "out": outID},
			)
			if err != nil {
				db.AddError(err)
				return
			}
			if len(*results) > 0 && (*results)[0].Status != "OK" {
				db.AddError(fmt.Errorf("edge assoc delete error: %v", (*results)[0]))
				return
			}
		}
		// Mark handled so DeleteCallback skips normal SQL
		db.Statement.SQL.WriteString("-- edge assoc delete handled")
		return
	}
}

// edgeAssocReplaceCallback handles Association("X").Replace(&y) for edge tables.
// GORM signals a replace via "gorm:association:replace" in Statement.Settings.
// We delete all existing edges for the owner then insert the new ones.
func edgeAssocReplaceCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	assocMode, _ := db.Statement.Settings.Load("gorm:association:replace")
	if assocMode == nil {
		return
	}
	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema == nil {
		return
	}
	for _, rel := range db.Statement.Schema.Relationships.Many2Many {
		if rel.JoinTable == nil {
			continue
		}
		registeredEdge, ok := dialector.FindEdgeTable(rel.JoinTable.Table)
		if !ok {
			continue
		}
		rv := db.Statement.ReflectValue
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
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
		// Delete all existing edges for this owner
		results, err := surrealdb.Query[interface{}](
			db.Statement.Context, dialector.Conn,
			fmt.Sprintf("DELETE %s WHERE in = $in", registeredEdge),
			map[string]interface{}{"in": inID},
		)
		if err != nil {
			db.AddError(err)
			return
		}
		if len(*results) > 0 && (*results)[0].Status != "OK" {
			db.AddError(fmt.Errorf("edge assoc replace (delete phase) error: %v", (*results)[0]))
			return
		}
		// Insert new edges from Statement.Dest
		destVal := reflect.ValueOf(db.Statement.Dest)
		for destVal.Kind() == reflect.Pointer {
			destVal = destVal.Elem()
		}
		var newOutIDs []*sdkModels.RecordID
		if destVal.Kind() == reflect.Slice {
			for i := 0; i < destVal.Len(); i++ {
				elem := destVal.Index(i)
				for elem.Kind() == reflect.Pointer {
					elem = elem.Elem()
				}
				for _, ref := range rel.References {
					if !ref.OwnPrimaryKey && ref.PrimaryValue == "" {
						v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, elem)
						if !isZero {
							if rid := extractRecordID(v); rid != nil {
								newOutIDs = append(newOutIDs, rid)
							}
						}
						break
					}
				}
			}
		}
		for _, outID := range newOutIDs {
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
		db.Statement.SQL.WriteString("-- edge assoc replace handled")
		return
	}
}

// edgeAssocCountCallback handles Association("X").Count() for edge tables.
// GORM routes this through the Row pipeline. We detect the edge table and
// execute SELECT count() FROM <edgeTable> WHERE in = $in GROUP ALL.
func edgeAssocCountCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	_, isAssocCount := db.Statement.Settings.Load("gorm:association:count")
	if !isAssocCount {
		return
	}
	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema == nil {
		return
	}
	for _, rel := range db.Statement.Schema.Relationships.Many2Many {
		if rel.JoinTable == nil {
			continue
		}
		registeredEdge, ok := dialector.FindEdgeTable(rel.JoinTable.Table)
		if !ok {
			continue
		}
		rv := db.Statement.ReflectValue
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
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
			return
		}
		type countResult struct {
			Count int64 `json:"count"`
		}
		results, err := surrealdb.Query[[]countResult](
			db.Statement.Context, dialector.Conn,
			fmt.Sprintf("SELECT count() FROM %s WHERE in = $in GROUP ALL", registeredEdge),
			map[string]interface{}{"in": inID},
		)
		if err != nil {
			db.AddError(err)
			return
		}
		if len(*results) > 0 && (*results)[0].Status == "OK" && len((*results)[0].Result) > 0 {
			db.RowsAffected = (*results)[0].Result[0].Count
		}
		// Write sentinel so gorm:row knows we handled it
		db.Statement.SQL.WriteString("-- edge count handled")
		return
	}
}
