package surrealdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
				timestampSQL := "created_at = time::now(), updated_at = time::now()"
				var setParts []string
				for k, v := range extraData {
					setParts = append(setParts, fmt.Sprintf("%s = %v", k, v))
				}
				var setClause string
				if len(setParts) > 0 {
					setClause = fmt.Sprintf("%s, %s", timestampSQL, setParts[0])
					for i := 1; i < len(setParts); i++ {
						setClause += ", " + setParts[i]
					}
				} else {
					setClause = timestampSQL
				}
				sql := fmt.Sprintf("RELATE %s -> %s -> %s SET %s",
					inID.String(), db.Statement.Table, outID.String(), setClause)
				if err := db.Exec(sql).Error; err != nil {
					db.AddError(err)
					return
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
			// Detect if the edge table schema has timestamps.
			hasTimestamps := db.Statement.Schema.LookUpField("CreatedAt") != nil
			if hasTimestamps {
				sql := fmt.Sprintf("RELATE %s -> %s -> %s SET created_at = time::now(), updated_at = time::now()",
					fkVals[0].String(), registeredName, fkVals[1].String())
				if err := db.Exec(sql).Error; err != nil {
					db.AddError(err)
					return
				}
				db.RowsAffected = 1
				return
			}

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
		table := sdkModels.Table(db.Statement.Table)
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

		var created *interface{}
		var err error
		if whatRecord != nil {
			created, err = surrealdb.Update[interface{}](db.Statement.Context, dialector.Conn, *whatRecord, createData)
		} else {
			created, err = surrealdb.Create[interface{}](db.Statement.Context, dialector.Conn, sdkModels.Table(whatTable), createData)
		}

		if err != nil {
			db.AddError(err)
			return
		}

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
