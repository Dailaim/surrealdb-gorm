package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/dailaim/surrealdb-gorm/types"
)

type RelationIdentifiable[T any, U any] interface {
	ConnectionOut() *types.Link[T]
	ConnectionIn() *types.Link[U]
}

type Relation[T any, U any] struct {
	In  *types.Link[T] `gorm:"column:in" json:"in,omitempty"`
	Out *types.Link[U] `gorm:"column:out" json:"out,omitempty"`
}

func (s Relation[T, U]) GormDataType() string {
	return "relation"
}

func (s Relation[T, U]) ConectionOut() *types.Link[U] {
	return s.Out
}

func (s Relation[T, U]) ConectionIn() *types.Link[T] {
	return s.In
}

func (s Relation[T, U]) Value() (driver.Value, error) {
	// Retornamos bytes JSON para que GORM no trate de analizarlo como relación SQL
	return json.Marshal(s)
}

// Scan implementa sql.Scanner
func (s *Relation[T, U]) Scan(value interface{}) error {
	if value == nil {
		*s = Relation[T, U]{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		// Attempt to marshal if it's already an object (like slice of interface)
		// SurrealDB driver sometimes returns []interface{}
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to scan SliceLink: %T, error: %v", value, err)
		}
		bytes = b
	}

	if len(bytes) == 0 {
		*s = Relation[T, U]{}
		return nil
	}

	// Helper to check for basic types array vs object array?
	// Just unmarshal
	return json.Unmarshal(bytes, s)
}

type RelationSchemafull[T any, U any] struct {
	Schemafull
	Relation[T, U]
}

type RelationSchemaless[T any, U any] struct {
	Schemaless
	Relation[T, U]
}
