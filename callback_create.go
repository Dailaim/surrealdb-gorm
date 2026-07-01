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

	localModels "github.com/dailaim/surrealdb-gorm/models"
	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

func CreateCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	if db.DryRun {
		return
	}

	dialector := db.Dialector.(*Dialector)
	if dialector.Conn == nil {
		db.AddError(errors.New("surrealdb connection not initialized"))
		return
	}

	// Trace the create to GORM's logger so it shows under db.Debug() like the
	// query/update/delete paths. Creates go through the SDK, not executeSQL, so
	// we synthesize a descriptive statement.
	begin := time.Now()
	defer func() {
		db.Logger.Trace(db.Statement.Context, begin, func() (string, int64) {
			return fmt.Sprintf("CREATE `%s`", db.Statement.Table), db.RowsAffected
		}, db.Error)
	}()

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

			extraData := make(map[string]interface{})
			hasTimestamps := false
			if db.Statement.Schema != nil {
				hasTimestamps = db.Statement.Schema.LookUpField("CreatedAt") != nil
				skipFields := map[string]bool{"id": true, "in": true, "out": true,
					"created_at": true, "updated_at": true, "deleted_at": true}
				for _, field := range db.Statement.Schema.Fields {
					if field.DBName == "" || skipFields[field.DBName] {
						continue
					}
					if field.StructField.Anonymous {
						continue
					}
					val, isZero := field.ValueOf(db.Statement.Context, reflectValue)
					if !isZero {
						extraData[field.DBName] = val
					}
				}
			}

			// If timestamps are present, use native RELATE with time::now() instead
			// of InsertRelation which ignores extra fields like created_at.
			if hasTimestamps {
				params := make(map[string]interface{})
				setParts := []string{"created_at = time::now()", "updated_at = time::now()"}
				i := 0
				for k, v := range extraData {
					paramKey := fmt.Sprintf("p%d", i)
					params[paramKey] = TypesM.ToSDKValue(v)
					setParts = append(setParts, fmt.Sprintf("%s = $%s", k, paramKey))
					i++
				}
				sql := fmt.Sprintf("RELATE %s -> %s -> %s SET %s",
					inID.String(), db.Statement.Table, outID.String(),
					strings.Join(setParts, ", "))
				var results *[]surrealdb.QueryResult[interface{}]
				var err error
				if txConn, ok := db.Statement.ConnPool.(*SurrealTx); ok {
					results, err = surrealdb.Query[interface{}](db.Statement.Context, txConn.SDKTx(), sql, params)
				} else {
					results, err = surrealdb.Query[interface{}](db.Statement.Context, dialector.Conn, sql, params)
				}
				if err != nil {
					db.AddError(err)
					return
				}
				if len(*results) > 0 && (*results)[0].Status != "OK" {
					db.AddError(fmt.Errorf("relate error: %v", (*results)[0]))
					return
				}
				// Write the created edge (id, in, out, timestamps) back into dest
				// so callers see the populated ID after db.Create(&edge).
				if len(*results) > 0 && (*results)[0].Result != nil {
					val := reflect.ValueOf((*results)[0].Result)
					for val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface {
						if val.IsNil() {
							break
						}
						val = val.Elem()
					}
					var single interface{}
					if val.IsValid() && (val.Kind() == reflect.Slice || val.Kind() == reflect.Array) {
						if val.Len() > 0 {
							single = val.Index(0).Interface()
						}
					} else if val.IsValid() {
						single = val.Interface()
					}
					if single != nil {
						if b, err := json.Marshal(single); err == nil {
							_ = json.Unmarshal(b, db.Statement.Dest)
						}
					}
				}
				db.RowsAffected = 1
				return
			}

			rel := &surrealdb.Relationship{
				In:       inID.RecordID,
				Out:      outID.RecordID,
				Relation: sdkModels.Table(db.Statement.Table),
				Data:     extraData,
			}
			var result *interface{}
			var err error
			if txConn, ok := db.Statement.ConnPool.(*SurrealTx); ok {
				result, err = surrealdb.InsertRelation[interface{}](db.Statement.Context, txConn.SDKTx(), rel)
			} else {
				result, err = surrealdb.InsertRelation[interface{}](db.Statement.Context, dialector.Conn, rel)
			}
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
	if registeredName, isEdge := dialector.FindEdgeTable(db.Statement.Table); isEdge && db.Statement.Schema != nil {
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
			// Collect extra (non-FK, non-timestamp) fields from the struct.
			skipDBNames := map[string]bool{
				"id": true, "in": true, "out": true,
				"created_at": true, "updated_at": true, "deleted_at": true,
			}
			extraData := make(map[string]interface{})
			for _, field := range db.Statement.Schema.Fields {
				if field.DBName == "" || skipDBNames[field.DBName] {
					continue
				}
				v, isZero := field.ValueOf(db.Statement.Context, reflectValue)
				if isZero {
					continue
				}
				if extractRecordID(v) != nil {
					continue // skip FK references
				}
				extraData[field.DBName] = TypesM.ToSDKValue(v)
			}

			hasTimestamps := db.Statement.Schema.LookUpField("CreatedAt") != nil

			params := make(map[string]interface{})
			var setParts []string
			if hasTimestamps {
				setParts = append(setParts, "created_at = time::now()", "updated_at = time::now()")
			}
			i := 0
			for k, v := range extraData {
				paramKey := fmt.Sprintf("p%d", i)
				params[paramKey] = v
				setParts = append(setParts, fmt.Sprintf("%s = $%s", k, paramKey))
				i++
			}

			if len(setParts) > 0 {
				sql := fmt.Sprintf("RELATE %s -> %s -> %s SET %s",
					fkVals[0].String(), registeredName, fkVals[1].String(),
					strings.Join(setParts, ", "))
				var results *[]surrealdb.QueryResult[interface{}]
				var err error
				if txConn, ok := db.Statement.ConnPool.(*SurrealTx); ok {
					results, err = surrealdb.Query[interface{}](db.Statement.Context, txConn.SDKTx(), sql, params)
				} else {
					results, err = surrealdb.Query[interface{}](db.Statement.Context, dialector.Conn, sql, params)
				}
				if err != nil {
					db.AddError(err)
					return
				}
				if len(*results) > 0 && (*results)[0].Status != "OK" {
					db.AddError(fmt.Errorf("relate error: %v", (*results)[0]))
					return
				}
			} else {
				rel := &surrealdb.Relationship{
					In:       *fkVals[0],
					Out:      *fkVals[1],
					Relation: sdkModels.Table(registeredName),
				}
				var err error
				if txConn, ok := db.Statement.ConnPool.(*SurrealTx); ok {
					_, err = surrealdb.InsertRelation[interface{}](db.Statement.Context, txConn.SDKTx(), rel)
				} else {
					_, err = surrealdb.InsertRelation[interface{}](db.Statement.Context, dialector.Conn, rel)
				}
				if err != nil {
					db.AddError(err)
					return
				}
			}
			db.RowsAffected = 1
			return
		}
	}

	if reflectValue.Kind() == reflect.Slice || reflectValue.Kind() == reflect.Array {
		table := sdkModels.Table(db.Statement.Table)

		// Build one SDK-safe map per element instead of a raw json.Marshal of the
		// slice. This mirrors the single-record path: zero-value fields are
		// skipped (so option<datetime> accepts NONE) and every value goes through
		// ToSDKValue so datetimes/decimals are sent as CBOR-native types rather
		// than plain strings (which SurrealDB rejects for typed fields).
		now := time.Now()
		objects := make([]map[string]interface{}, 0, reflectValue.Len())
		for i := 0; i < reflectValue.Len(); i++ {
			elem := reflectValue.Index(i)
			for elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}
			if db.Statement.Schema != nil && elem.CanAddr() {
				if f := db.Statement.Schema.LookUpField("CreatedAt"); f != nil {
					if _, isZero := f.ValueOf(db.Statement.Context, elem); isZero {
						f.Set(db.Statement.Context, elem, now)
					}
				}
				if f := db.Statement.Schema.LookUpField("UpdatedAt"); f != nil {
					if _, isZero := f.ValueOf(db.Statement.Context, elem); isZero {
						f.Set(db.Statement.Context, elem, now)
					}
				}
			}
			obj := make(map[string]interface{})
			if db.Statement.Schema != nil {
				for _, field := range db.Statement.Schema.Fields {
					if field.DBName == "" {
						continue
					}
					val, isZero := field.ValueOf(db.Statement.Context, elem)
					if isZero {
						continue
					}
					obj[field.DBName] = TypesM.ToSDKValue(val)
				}
			}
			objects = append(objects, obj)
		}

		// Route through the interactive transaction if one is open so bulk
		// inserts participate in db.Transaction(...).
		var created *[]interface{}
		var err error
		if txConn, ok := db.Statement.ConnPool.(*SurrealTx); ok {
			created, err = surrealdb.Insert[interface{}](db.Statement.Context, txConn.SDKTx(), table, objects)
		} else {
			created, err = surrealdb.Insert[interface{}](db.Statement.Context, dialector.Conn, table, objects)
		}
		if err != nil {
			db.AddError(err)
			return
		}
		// Write server-assigned fields (ids, timestamps) back into each element.
		// json.Unmarshal into db.Statement.Dest does not work here because in the
		// CreateInBatches path Dest is a (non-pointer) slice value; instead we map
		// each returned record onto its addressable slice element.
		if created != nil {
			recs := *created
			for i := 0; i < len(recs) && i < reflectValue.Len(); i++ {
				elem := reflectValue.Index(i)
				if elem.Kind() == reflect.Pointer {
					if elem.IsNil() {
						continue
					}
					elem = elem.Elem()
				}
				if !elem.CanAddr() {
					continue
				}
				if bb, merr := json.Marshal(recs[i]); merr == nil {
					_ = json.Unmarshal(bb, elem.Addr().Interface())
				}
			}
		}
		db.RowsAffected = int64(len(objects))
		return
	} else {
		var whatTable = db.Statement.Table
		var whatRecord *sdkModels.RecordID

		if db.Statement.Schema != nil {
			now := time.Now()
			if field := db.Statement.Schema.LookUpField("CreatedAt"); field != nil {
				_, isZero := field.ValueOf(db.Statement.Context, db.Statement.ReflectValue)
				if isZero {
					field.Set(db.Statement.Context, db.Statement.ReflectValue, now)
				}
			}
			if field := db.Statement.Schema.LookUpField("UpdatedAt"); field != nil {
				_, isZero := field.ValueOf(db.Statement.Context, db.Statement.ReflectValue)
				if isZero {
					field.Set(db.Statement.Context, db.Statement.ReflectValue, now)
				}
			}
		}

		if model, ok := db.Statement.Model.(TypesM.Identifiable); ok {
			if model.GetID() != nil {
				whatRecord = &model.GetID().RecordID
			}
		}

		var createData interface{} = db.Statement.Dest
		if db.Statement.Schema != nil && db.Statement.ReflectValue.Kind() == reflect.Struct {
			dataMap := make(map[string]interface{})
			for _, field := range db.Statement.Schema.Fields {
				if field.DBName == "" {
					continue
				}
				val, isZero := field.ValueOf(db.Statement.Context, db.Statement.ReflectValue)
				if !isZero {
					dataMap[field.DBName] = TypesM.ToSDKValue(val)
				}
			}
			createData = dataMap
		}

		// Use sdkTx if inside a GORM transaction so the CREATE participates in
		// the open transaction (read-your-own-writes).
		var created *interface{}
		var err error
		if txConn, ok := db.Statement.ConnPool.(*SurrealTx); ok {
			if whatRecord != nil {
				created, err = surrealdb.Update[interface{}](db.Statement.Context, txConn.SDKTx(), *whatRecord, createData)
			} else {
				created, err = surrealdb.Create[interface{}](db.Statement.Context, txConn.SDKTx(), sdkModels.Table(whatTable), createData)
			}
		} else if whatRecord != nil {
			created, err = surrealdb.Update[interface{}](db.Statement.Context, dialector.Conn, *whatRecord, createData)
		} else {
			created, err = surrealdb.Create[interface{}](db.Statement.Context, dialector.Conn, sdkModels.Table(whatTable), createData)
		}

		if err != nil {
			db.AddError(err)
			return
		}

		// Set RowsAffected here (not only at the end) because the result-mapping
		// block below returns early on success; without this, callers like
		// FirstOrCreate see RowsAffected == 0 after a successful create.
		db.RowsAffected = 1

		if created != nil {
			val := reflect.ValueOf(created)
			if val.Kind() == reflect.Pointer {
				val = val.Elem()
			}
			if val.Kind() == reflect.Interface {
				val = val.Elem()
			}
			var dataToUnmarshal interface{}
			if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
				if val.Len() > 0 {
					dataToUnmarshal = val.Index(0).Interface()
				}
			} else if val.IsValid() {
				dataToUnmarshal = val.Interface()
			}

			if db.Statement.Schema != nil && db.Statement.ReflectValue.Kind() == reflect.Struct {
				bytes, err := json.Marshal(dataToUnmarshal)
				if err == nil {
					var resultMap map[string]interface{}
					if err := json.Unmarshal(bytes, &resultMap); err == nil {
						for _, field := range db.Statement.Schema.Fields {
							if val, ok := resultMap[field.DBName]; ok {
								fieldVal := db.Statement.ReflectValue.FieldByIndex(field.StructField.Index)
								if fieldVal.CanAddr() {
									valBytes, err := json.Marshal(val)
									if err == nil {
										_ = json.Unmarshal(valBytes, fieldVal.Addr().Interface())
									}
								}
							}
							// Note: id field also gets mapped here if DBName == "id"
						}
						return
					}
				}
			}

			bytes, err := json.Marshal(dataToUnmarshal)
			if err == nil {
				if err := json.Unmarshal(bytes, db.Statement.Dest); err != nil {
					db.AddError(fmt.Errorf("failed to unmarshal create result: %v", err))
				}
			}
		}
	}

	db.RowsAffected = 1
}
