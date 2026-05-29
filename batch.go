package surrealdb

import (
	"fmt"
	"reflect"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
	"gorm.io/gorm"
)

// ============================================================================
// Batch / Bulk Insert helpers
// ============================================================================

// CreateMany performs a native SurrealDB batch INSERT for a slice of records.
// It avoids N individual CREATE queries by building one INSERT with an array.
// The model slice must contain elements of the same type.
func CreateMany(db *gorm.DB, models interface{}) error {
	if db.Error != nil {
		return db.Error
	}

	val := reflect.ValueOf(models)
	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return fmt.Errorf("CreateMany expects a slice, got %T", models)
	}
	if val.Len() == 0 {
		return nil
	}

	elemType := val.Type().Elem()
	for elemType.Kind() == reflect.Ptr {
		elemType = elemType.Elem()
	}

	// Build array of objects
	objects := make([]map[string]interface{}, 0, val.Len())
	for i := 0; i < val.Len(); i++ {
		elemVal := val.Index(i)
		for elemVal.Kind() == reflect.Ptr {
			elemVal = elemVal.Elem()
		}

		obj := make(map[string]interface{})
		for j := 0; j < elemVal.NumField(); j++ {
			field := elemVal.Type().Field(j)
			if field.PkgPath != "" { // unexported
				continue
			}
			fieldVal := elemVal.Field(j)
			if !fieldVal.IsValid() {
				continue
			}

			// Skip zero-value pointers unless they are explicitly set
			if fieldVal.Kind() == reflect.Ptr && fieldVal.IsNil() {
				continue
			}

			// Convert to SDK-compatible value
			v := fieldVal.Interface()
			sdkVal := TypesM.ToSDKValue(v)
			obj[field.Name] = sdkVal
		}
		objects = append(objects, obj)
	}

	// Derive table name from first element
	var tableName string
	firstElem := val.Index(0)
	for firstElem.Kind() == reflect.Ptr {
		firstElem = firstElem.Elem()
	}
	if tn, ok := firstElem.Addr().Interface().(interface{ TableName() string }); ok {
		tableName = tn.TableName()
	} else {
		// Fallback to GORM naming strategy
		ns := db.NamingStrategy
		tableName = ns.TableName(elemType.Name())
	}

	sql := fmt.Sprintf("INSERT INTO `%s` %v", tableName, objects)
	return db.Exec(sql).Error
}
