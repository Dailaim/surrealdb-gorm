package surrealdb

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	gormSchema "gorm.io/gorm/schema"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

// ============================================================================
// Batch / Bulk Insert helpers
// ============================================================================

// CreateMany performs a native SurrealDB batch INSERT for a slice of records.
// It avoids N individual CREATE queries by using the SDK's Insert with an array.
// The models argument must be a pointer to a slice: &[]User{...}.
func CreateMany(db *gorm.DB, models interface{}) error {
	if db.Error != nil {
		return db.Error
	}

	val := reflect.ValueOf(models)
	for val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return fmt.Errorf("CreateMany expects a slice, got %T", models)
	}
	if val.Len() == 0 {
		return nil
	}

	dialector, ok := db.Dialector.(*Dialector)
	if !ok || dialector.Conn == nil {
		return fmt.Errorf("surrealdb connection not initialized")
	}

	elemType := val.Type().Elem()
	for elemType.Kind() == reflect.Ptr {
		elemType = elemType.Elem()
	}

	// Parse schema for the element type to get correct DB column names.
	s, err := gormSchema.Parse(reflect.New(elemType).Interface(), &sync.Map{}, db.NamingStrategy)
	if err != nil {
		return fmt.Errorf("CreateMany: schema parse error: %w", err)
	}

	// Derive table name (respect custom TableName() if defined).
	tableName := s.Table
	if tn, ok := reflect.New(elemType).Interface().(interface{ TableName() string }); ok {
		tableName = tn.TableName()
	}

	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}

	// Build slice of SDK-safe maps using DBName (not struct field name).
	objects := make([]map[string]interface{}, 0, val.Len())
	for i := 0; i < val.Len(); i++ {
		elemVal := val.Index(i)
		for elemVal.Kind() == reflect.Ptr {
			elemVal = elemVal.Elem()
		}
		obj := make(map[string]interface{})
		for _, field := range s.Fields {
			if field.DBName == "" {
				continue
			}
			fVal, isZero := field.ValueOf(ctx, elemVal)
			if isZero {
				continue
			}
			obj[field.DBName] = TypesM.ToSDKValue(fVal)
		}
		objects = append(objects, obj)
	}

	table := sdkModels.Table(tableName)
	_, err = surrealdb.Insert[interface{}](ctx, dialector.Conn, table, objects)
	return err
}
