package surrealdb

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

func executeSQL(db *gorm.DB) {
	dialector := db.Dialector.(*Dialector)
	sql := db.Statement.SQL.String()

	// Map table name to canonical form
	actualTable := db.Statement.Table
	if actualTable != "" {
		if canonical, ok := dialector.FindEdgeTable(actualTable); ok {
			sql = strings.ReplaceAll(sql, fmt.Sprintf("`%s`", actualTable), fmt.Sprintf("`%s`", canonical))
			sql = strings.ReplaceAll(sql, fmt.Sprintf("FROM %s", actualTable), fmt.Sprintf("FROM `%s`", canonical))
			actualTable = canonical
		}
	}

	// Replace <> with !=
	sql = strings.ReplaceAll(sql, "<>", "!=")

	// Rewrite count queries
	if strings.Contains(sql, "SELECT count(*)") || strings.Contains(sql, "SELECT count(1)") {
		sql = strings.ReplaceAll(sql, "count(*)", "count()")
		sql = strings.ReplaceAll(sql, "count(1)", "count()")

		if !strings.Contains(strings.ToUpper(sql), "GROUP ALL") {
			sql = sql + " GROUP ALL"
		}

		// Edge table association count rewrite
		if strings.Contains(sql, " JOIN ") {
			if dialector != nil {
				rewritten := false

				// Case 1: FROM table is the edge table
				parts := strings.Split(sql, " FROM `")
				if len(parts) == 2 {
					tablePart := strings.Split(parts[1], "`")[0]
					if canonical, ok := dialector.FindEdgeTable(tablePart); ok {
						var ownerField string
						for _, param := range []string{"in", "out"} {
							if strings.Contains(sql, ".`"+param+"` = $p") {
								ownerField = param
								break
							}
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
								rewritten = true
							}
						}
					}
				}

				// Case 2: edge table is in JOIN clause → graph traversal
				if !rewritten {
					joinRe := regexp.MustCompile("JOIN `([^`]+)` ON")
					matches := joinRe.FindStringSubmatch(sql)
					if len(matches) >= 2 {
						joinTable := matches[1]
						if canonical, ok := dialector.FindEdgeTable(joinTable); ok {
							var ownerField, param string
							for _, field := range []string{"in", "out"} {
								paramRe := regexp.MustCompile(fmt.Sprintf("`%s`\\.`%s` = (\\$p\\d+)", regexp.QuoteMeta(canonical), field))
								pm := paramRe.FindStringSubmatch(sql)
								if len(pm) >= 2 {
									ownerField = field
									param = pm[1]
									break
								}
							}
							if ownerField != "" && param != "" {
								fromRe := regexp.MustCompile("FROM `([^`]+)`")
								fm := fromRe.FindStringSubmatch(sql)
								targetTable := ""
								if len(fm) >= 2 {
									targetTable = fm[1]
								}
								if targetTable != "" {
									var traversal string
									if ownerField == "in" {
										traversal = fmt.Sprintf("%s->%s->%s", param, canonical, targetTable)
									} else {
										traversal = fmt.Sprintf("%s<-%s<-%s", param, canonical, targetTable)
									}
									whereClause := ""
									whereIdx := strings.Index(strings.ToUpper(sql), " WHERE ")
									if whereIdx != -1 {
										end := len(sql)
										groupIdx := strings.Index(strings.ToUpper(sql[whereIdx:]), " GROUP ALL")
										if groupIdx != -1 {
											end = whereIdx + groupIdx
										}
										whereClause = sql[whereIdx:end]
									}
									sql = fmt.Sprintf("SELECT count() FROM %s%s GROUP ALL", traversal, whereClause)
								}
							}
						}
					}
				}
			}
		}
	}

	// Translate OFFSET to START
	sql = strings.ReplaceAll(sql, " OFFSET ", " START ")

	// Remove table prefixes
	if actualTable != "" {
		sql = strings.ReplaceAll(sql, fmt.Sprintf("`%s`.", actualTable), "")
		sql = strings.ReplaceAll(sql, fmt.Sprintf("%s.", actualTable), "")
	}

	// Translate DELETE FROM
	if len(sql) > 12 && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "DELETE FROM ") {
		idx := strings.Index(strings.ToUpper(sql), "DELETE FROM ")
		if idx != -1 {
			sql = sql[:idx] + "DELETE " + strings.TrimSpace(sql[idx+12:])
		}
	}
	// Duplicate block (keep for safety)
	if len(sql) > 12 && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "DELETE FROM ") {
		idx := strings.Index(strings.ToUpper(sql), "DELETE FROM ")
		if idx != -1 {
			sql = sql[:idx] + "DELETE " + strings.TrimSpace(sql[idx+12:])
		}
	}

	// Remove soft-delete IS NULL check
	sql = strings.ReplaceAll(sql, " AND (`deleted_at` IS NULL)", "")

	// SurrealDB soft delete syntax fix
	if strings.HasPrefix(strings.TrimSpace(sql), "UPDATE") {
		sql = strings.ReplaceAll(sql, "`deleted_at` IS NULL", "(`deleted_at` IS NULL OR `deleted_at` IS NONE)")
		if actualTable != "" {
			sql = strings.ReplaceAll(sql, fmt.Sprintf("`%s`.`deleted_at` IS NULL", actualTable), fmt.Sprintf("(`%s`.`deleted_at` IS NULL OR `%s`.`deleted_at` IS NONE)", actualTable, actualTable))
		}
	}

	vars := db.Statement.Vars
	params := make(map[string]interface{})
	for i, v := range vars {
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
		if rid, ok := v.(*sdkModels.RecordID); ok {
			params[fmt.Sprintf("p%d", i+1)] = rid
			continue
		}
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

	// Inline LIMIT and START params
	inlineRegex := regexp.MustCompile(`(LIMIT|START)\s+\$p(\d+)`)
	sql = inlineRegex.ReplaceAllStringFunc(sql, func(match string) string {
		parts := strings.Split(match, "$p")
		if len(parts) == 2 {
			paramKey := "p" + parts[1]
			if val, ok := params[paramKey]; ok {
				delete(params, paramKey)
				return fmt.Sprintf("%s %v", strings.TrimSpace(parts[0]), val)
			}
		}
		return match
	})

	// Execute
	results, err := surrealdb.Query[interface{}](db.Statement.Context, dialector.Conn, sql, params)
	if err != nil {
		db.AddError(err)
		return
	}

	if len(*results) > 0 {
		res := (*results)[0]

		if res.Status != "OK" {
			db.AddError(fmt.Errorf("surrealdb query error: %v", res))
			return
		}

		var count int64 = 0
		if res.Result != nil {
			val := reflect.ValueOf(res.Result)
			if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
				count = int64(val.Len())
			} else {
				count = 1
			}
		}
		db.RowsAffected = count

		if db.Statement.Dest != nil && count > 0 {
			var dataToUnmarshal interface{} = res.Result

			destVal := reflect.ValueOf(db.Statement.Dest)
			if destVal.Kind() == reflect.Ptr {
				for destVal.Kind() == reflect.Ptr {
					destVal = destVal.Elem()
				}
			} else {
				return
			}

			if destVal.Kind() != reflect.Slice && destVal.Kind() != reflect.Array {
				resVal := reflect.ValueOf(res.Result)
				if resVal.Kind() == reflect.Slice || resVal.Kind() == reflect.Array {
					if resVal.Len() > 0 {
						dataToUnmarshal = resVal.Index(0).Interface()
					}
				}
			}

			// Custom mapping for Count queries where Dest is an integer pointer
			if destVal.CanSet() && (destVal.Kind() == reflect.Int64 || destVal.Kind() == reflect.Int) {
				if m, ok := dataToUnmarshal.(map[string]interface{}); ok {
					if c, ok := m["count"]; ok {
						switch cv := c.(type) {
						case float64:
							destVal.SetInt(int64(cv))
							return
						case uint64:
							destVal.SetInt(int64(cv))
							return
						case int64:
							destVal.SetInt(cv)
							return
						case json.Number:
							if iv, err := cv.Int64(); err == nil {
								destVal.SetInt(iv)
								return
							}
						}
					}
				}
			}

			bytes, err := json.Marshal(dataToUnmarshal)
			if err == nil {
				_ = json.Unmarshal(bytes, db.Statement.Dest)

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
