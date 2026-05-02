package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/dailaim/surrealdb-gorm/types"
)

type EdgeIdentifiable[T any, U any] interface {
	ConnectionOut() *types.Link[T]
	ConnectionIn() *types.Link[U]
}

// EdgeRelation is a non-generic interface that edge models implement,
// allowing CreateCallback to detect and route through InsertRelation.
type EdgeRelation interface {
	EdgeIn() *types.RecordID
	EdgeOut() *types.RecordID
}

type Edge[T any, U any] struct {
	In  *types.Link[T] `gorm:"column:in" json:"in,omitempty"`
	Out *types.Link[U] `gorm:"column:out" json:"out,omitempty"`
}

func (s Edge[T, U]) GormDataType() string {
	return "Edge"
}

func (s Edge[T, U]) ConectionOut() *types.Link[U] {
	return s.Out
}

func (s Edge[T, U]) ConectionIn() *types.Link[T] {
	return s.In
}

// EdgeIn returns the RecordID of the "in" side of the edge, implementing EdgeRelation.
func (s Edge[T, U]) EdgeIn() *types.RecordID {
	if s.In != nil {
		return s.In.ID
	}
	return nil
}

// EdgeOut returns the RecordID of the "out" side of the edge, implementing EdgeRelation.
func (s Edge[T, U]) EdgeOut() *types.RecordID {
	if s.Out != nil {
		return s.Out.ID
	}
	return nil
}

func (s Edge[T, U]) Value() (driver.Value, error) {
	// Retornamos bytes JSON para que GORM no trate de analizarlo como relación SQL
	return json.Marshal(s)
}

// Scan implementa sql.Scanner
func (s *Edge[T, U]) Scan(value interface{}) error {
	if value == nil {
		*s = Edge[T, U]{}
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
		*s = Edge[T, U]{}
		return nil
	}

	// Helper to check for basic types array vs object array?
	// Just unmarshal
	return json.Unmarshal(bytes, s)
}

type EdgeSchemafull[T any, U any] struct {
	Schemafull
	Edge[T, U]
}

type EdgeSchemaless[T any, U any] struct {
	Schemaless
	Edge[T, U]
}
