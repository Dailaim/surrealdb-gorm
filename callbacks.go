package surrealdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dailaim/surrealdb-gorm/clauses"
)

func RegisterCallbacks(db *gorm.DB) {
	// Create
	db.Callback().Create().Replace("gorm:create", CreateCallback)

	// Update
	db.Callback().Update().Replace("gorm:update", UpdateCallback)

	// Delete
	db.Callback().Delete().Replace("gorm:delete", DeleteCallback)

	// Query
	db.Callback().Query().Before("gorm:query").Register("surreal:handle_preload", handlePreloadAsFetch)

	db.Callback().Query().Replace("gorm:query", QueryCallback)
}

func handlePreloadAsFetch(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	// Convert Preloads to FETCH
	if len(db.Statement.Preloads) > 0 {
		var fields []string
		for name := range db.Statement.Preloads {
			// Handle nested paths (e.g., "Next.Next")
			parts := strings.Split(name, ".")
			var dbParts []string

			// We try to walk the schema if possible, but for Link[T] generic relations,
			// GORM might not have the full Relationship Schema loaded on the field.
			// We start with the current Statement Schema.
			currentSchema := db.Statement.Schema

			for _, part := range parts {
				mapped := part
				// Try lookup in current schema
				if currentSchema != nil {
					if field := currentSchema.LookUpField(part); field != nil {
						mapped = field.DBName
						// Attempt to switch schema for next part if relation exists
						if field.Schema != nil {
							currentSchema = field.Schema
						} else {
							// Check if valid Relation (rare for simple structural structs without tags)
							// If not found, we lose context for next parts.
							// We can nullify currentSchema to stop trying LookUpField
							// or we can try to guess if it's a struct field.
							// For now, fallback to NamingStrategy or just part.

							// If we lost schema track (e.g. Link[T]), we rely on NamingStrategy
							currentSchema = nil
						}
					}
				}

				// Final Fallback: use NamingStrategy to guess DBName (usually snake_case)
				// if we couldn't resolve it via Schema or if Schema was nil.
				if mapped == part {
					mapped = db.NamingStrategy.ColumnName("", part)
				}

				dbParts = append(dbParts, mapped)
			}

			// Expand prefixes to ensure all intermediate nodes are fetched
			// path: next.next.next
			// -> next
			// -> next.next
			// -> next.next.next

			// Rebuild clean path from dbParts
			var currentPath string
			for i, dbPart := range dbParts {
				if i == 0 {
					currentPath = dbPart
				} else {
					currentPath = currentPath + "." + dbPart
				}
				fields = append(fields, currentPath)
			}
		}

		// Deduplicate fields
		seen := make(map[string]bool)
		var uniqueFields []string
		for _, f := range fields {
			if !seen[f] {
				seen[f] = true
				uniqueFields = append(uniqueFields, f)
			}
		}

		if len(uniqueFields) > 0 {
			db.Statement.AddClause(clauses.Fetch{Fields: uniqueFields})
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
	if reflectValue.Kind() == reflect.Slice || reflectValue.Kind() == reflect.Array {
		// Batch insert logic
		// For simplicity, we assume generic Insert works or we iterate.
		// models.Table just wraps string.
		// Batch insert logic
		// For simplicity, we assume generic Insert works or we iterate.
		// models.Table just wraps string.
		table := models.Table(db.Statement.Table)

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
		var whatRecord *models.RecordID

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

		// Handle Timestamps
		if model, ok := db.Statement.Model.(TypesM.LinkVal); ok {
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
			created, err = surrealdb.Create[interface{}](db.Statement.Context, dialector.Conn, models.Table(whatTable), createData)
		}

		if err != nil {
			db.AddError(err)
			return
		}

		// The SDK takes `data any`. If data is pointer, it reads it. It returns new object.

		// Optimally we map `created` back to `db.Statement.Dest`
		if created != nil {
			// Unwrap array if needed
			var dataToUnmarshal interface{} = created
			val := reflect.ValueOf(created)
			if val.Kind() == reflect.Pointer {
				val = val.Elem()
			}
			if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
				if val.Len() > 0 {
					dataToUnmarshal = val.Index(0).Interface()
				}
			} else {
				dataToUnmarshal = created
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
		db.Statement.AddClause(clause.Select{Expression: clause.Expr{SQL: "*"}})
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
		if _, ok := db.Statement.Vars[0].(*models.RecordID); ok {
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
			if model, ok := db.Statement.Model.(TypesM.LinkVal); ok {
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
	if db.Error == nil {
		if db.Statement.Schema != nil {
			for _, c := range db.Statement.Schema.DeleteClauses {
				db.Statement.AddClause(c)
			}
		}

		if db.Statement.SQL.Len() == 0 {
			db.Statement.SQL.Grow(100)

			// Soft Delete check
			if db.Statement.Schema != nil && db.Statement.Schema.LookUpField("DeletedAt") != nil && !db.Statement.Unscoped {
				db.Statement.AddClauseIfNotExists(clause.Update{}) // Switch to UPDATE

				// Handle RecordID for soft delete update too if needed
				if model, ok := db.Statement.Model.(TypesM.LinkVal); ok {
					// Overwrite the UPDATE clause usually added by AddClauseIfNotExists(clause.Update{}) above?
					// AddClauseIfNotExists won't add if exists. GORM doesn't default add UPDATE clause before this?
					// Actually DeleteCallback construction is manual here too.

					// Let's replace the clause if provided.
					// Actually we just added clause.Update{}.
					// Ideally we should do same logic as UpdateCallback for Soft Delete target.

					// Re-doing the AddClause logic:
					// Check existing clauses map?
					// No, we are building it now.

					// Note: The previous code just did `db.Statement.AddClauseIfNotExists(clause.Update{})`.
					// This meant Soft Delete uses `UPDATE table`.
					// Using `UPDATE table` is fine for SurrealDB IF `executeSQL` patched it.
					// But we are removing `executeSQL` patch.
					// SO WE MUST FIX DELETE CALLBACK TOO if it uses UPDATE for Soft Delete.

					// Let's fix Soft Delete Update target:
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
}

func executeSQL(db *gorm.DB) {
	dialector := db.Dialector.(*Dialector)
	sql := db.Statement.SQL.String()

	// Hack: Remove table prefixes from SQL
	if db.Statement.Table != "" {
		quotedTable := fmt.Sprintf("`%s`.", db.Statement.Table)
		sql = strings.ReplaceAll(sql, quotedTable, "")
		// Also handle "table." without backticks just in case if users write raw sql
		sql = strings.ReplaceAll(sql, fmt.Sprintf("%s.", db.Statement.Table), "")
	}

	// Hack: Remove IS NULL check for Soft Delete compatibility
	sql = strings.ReplaceAll(sql, " AND (`deleted_at` IS NULL)", "")

	// Filter out "id" from UPDATE SET clause (SurrealDB error: specified record conflict)
	if strings.HasPrefix(strings.TrimSpace(sql), "UPDATE") {
		// Clean up soft delete checks for surrealdb specific syntax (IS NONE)
		whatTable := db.Statement.Table
		sql = strings.ReplaceAll(sql, "`deleted_at` IS NULL", "(`deleted_at` IS NULL OR `deleted_at` IS NONE)")
		sql = strings.ReplaceAll(sql, fmt.Sprintf("`%s`.`deleted_at` IS NULL", whatTable), fmt.Sprintf("(`%s`.`deleted_at` IS NULL OR `%s`.`deleted_at` IS NONE)", whatTable, whatTable))
	}

	vars := db.Statement.Vars

	// Map slice vars to map for SurrealDB
	params := make(map[string]interface{})

	for i, v := range vars {
		// Handle RecordID manually if needed
		if rid, ok := v.(*models.RecordID); ok {
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

	// Debug params
	// fmt.Printf("DEBUG QUERY: %s\nPARAMS: %+v\n", sql, params)
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

			bytes, err := json.Marshal(dataToUnmarshal)
			if err == nil {
				// Debug JSON
				// fmt.Printf("DEBUG SELECT JSON: %s\n", string(bytes))

				// Try Manual Map if Schema is available to respect GORM tags (DBName)
				if db.Statement.Schema != nil && destVal.Kind() == reflect.Struct {
					var resultMap map[string]interface{}
					if err := json.Unmarshal(bytes, &resultMap); err == nil {
						for _, field := range db.Statement.Schema.Fields {
							if val, ok := resultMap[field.DBName]; ok {
								fieldVal := db.Statement.ReflectValue.FieldByIndex(field.StructField.Index)
								if fieldVal.CanAddr() {
									valBytes, err := json.Marshal(val)
									if err == nil {
										if err := json.Unmarshal(valBytes, fieldVal.Addr().Interface()); err != nil {
											// Ignore error
										}
									}
								}
							}
						}
						return
					}
				}

				if err := json.Unmarshal(bytes, db.Statement.Dest); err != nil {
					// fmt.Printf("DEBUG EXECUTE SQL UNMARSHAL ERROR: %v\n", err)
					db.AddError(err)
				}
			}
		}
	}
}
