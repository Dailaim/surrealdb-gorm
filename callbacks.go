package surrealdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func RegisterCallbacks(db *gorm.DB) {
	// Create
	db.Callback().Create().Replace("gorm:create", CreateCallback)

	// Update
	db.Callback().Update().Replace("gorm:update", UpdateCallback)

	// Delete
	db.Callback().Delete().Replace("gorm:delete", DeleteCallback)

	// Query
	db.Callback().Query().Replace("gorm:query", QueryCallback)
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

		// We can check if the model has a "ID" field that is *models.RecordID and not nil

		// We can check if the model has a "ID" field that is *models.RecordID and not nil
		if db.Statement.Schema != nil {
			field := db.Statement.Schema.LookUpField("ID")
			if field != nil && field.FieldType == reflect.TypeOf(&models.RecordID{}) {
				// Get value
				val, isZero := field.ValueOf(db.Statement.Context, db.Statement.ReflectValue)
				if !isZero {
					if rid, ok := val.(*models.RecordID); ok && rid != nil {
						whatRecord = rid
					}
				}
			}
		}

		var created *interface{}
		var err error

		// Force JSON marshaling to generic map to respect custom MarshalJSON
		var data interface{}
		b, errMarshal := json.Marshal(db.Statement.Dest)
		if errMarshal != nil {
			db.AddError(errMarshal)
			return
		}
		if err := json.Unmarshal(b, &data); err != nil {
			db.AddError(err)
			return
		}

		// Sanitize data: remove 'id' field to prevent "specific record has been specified" error
		// when using Update/Create with an ID in the URL.
		if m, ok := data.(map[string]interface{}); ok {
			delete(m, "id")
			data = m
		}

		if whatRecord != nil {
			// If ID is specified, treat as Upsert/Update.
			// surrealdb.Update overwrites the record content, matching GORM Save semantics.
			created, err = surrealdb.Update[interface{}](db.Statement.Context, dialector.Conn, *whatRecord, data)
		} else {
			created, err = surrealdb.Create[interface{}](db.Statement.Context, dialector.Conn, models.Table(whatTable), data)
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
		// GORM standard processor seems to fail or bypass ExecContext for SurrealDB.
		// We will build the UPDATE statement manually from the clauses.

		if db.Statement.SQL.Len() == 0 {
			db.Statement.SQL.Grow(180)
			db.Statement.AddClauseIfNotExists(clause.Update{})
			db.Statement.AddClauseIfNotExists(clause.Set{})
			db.Statement.AddClauseIfNotExists(clause.Where{})

			// Manually populate SET clause if empty
			if set, ok := db.Statement.Clauses["SET"]; ok {
				if _, ok := set.Expression.(clause.Set); ok {
					// Check if assignments are empty?
					// Simpler: iterate Dest if it's a Map or Struct and generated assignments
					var assignments []clause.Assignment

					if destMap, ok := db.Statement.Dest.(map[string]interface{}); ok {
						for k, v := range destMap {
							assignments = append(assignments, clause.Assignment{Column: clause.Column{Name: k}, Value: v})
						}
					} else if db.Statement.Schema != nil {
						// Struct case
						destValue := reflect.ValueOf(db.Statement.Dest)
						for destValue.Kind() == reflect.Ptr {
							destValue = destValue.Elem()
						}

						if destValue.Kind() == reflect.Struct {
							for _, field := range db.Statement.Schema.Fields {
								if field.DBName == "" {
									continue
								}
								// Skip primary key usually? Or allow updating it? GORM usually allows unless restricted.
								// But we shouldn't update ID if it's the matcher.
								if field.DBName == "id" {
									continue
								}

								// Check if value is zero (for Updates, we usually skip zero values unless Select/Omit used)
								// For simplicity here, we skip zero values to match generic Updates behavior.
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

		// Sanity Check: If SQL contains `SET` with no assignments?
		// execution will handle it or DB returns error.

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
	sql = strings.ReplaceAll(sql, " AND `deleted_at` IS NULL", "")

	// Filter out "id" from UPDATE SET clause (SurrealDB error: specified record conflict)
	if strings.HasPrefix(strings.TrimSpace(sql), "UPDATE") {
		lowerSql := strings.ToLower(sql)
		setIdx := strings.Index(lowerSql, " set ")
		whereIdx := strings.LastIndex(lowerSql, " where ") // LastIndex to be safe? usually one WHERE

		if setIdx != -1 && (whereIdx == -1 || setIdx < whereIdx) {
			beforeSet := sql[:setIdx+5] // "UPDATE ... SET "
			var setPart string
			var afterSet string

			if whereIdx != -1 {
				setPart = sql[setIdx+5 : whereIdx]
				afterSet = sql[whereIdx:]
			} else {
				setPart = sql[setIdx+5:]
				afterSet = ""
			}

			// Remove id assignments in setPart
			// Patterns:
			// 1. `"id" = $pX, `
			// 2. `, "id" = $pX`
			// 3. `"id" = $pX`

			// Naive split by comma?
			// "col" = val. val might contain comma? $pX usually doesn't.
			// But if generic driver, val could be string literal? No, using bind vars $pX.
			// So splitting by comma is relatively safe IF we trust GORM param binding.

			parts := strings.Split(setPart, ",")
			var cleanParts []string
			for _, p := range parts {
				trimmed := strings.TrimSpace(p)
				// Check if targets atomic ID
				if strings.HasPrefix(trimmed, "`id` =") || strings.HasPrefix(trimmed, "id =") {
					continue
				}
				cleanParts = append(cleanParts, p)
			}
			newSetPart := strings.Join(cleanParts, ", ")
			sql = beforeSet + newSetPart + " " + afterSet
		}
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

				if err := json.Unmarshal(bytes, db.Statement.Dest); err != nil {
					// fmt.Printf("DEBUG EXECUTE SQL UNMARSHAL ERROR: %v\n", err)
					db.AddError(err)
				}
			}
		}
	}
}
