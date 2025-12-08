package surrealdb

import (
	"encoding/json"
	"time"

	"github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
)

// Model a basic GoLang struct which includes the following fields: ID, CreatedAt, UpdatedAt, DeletedAt
// It may be embedded into your model or you may build your own model without it
//
//	type User struct {
//	  surrealdb.Model
//	}
type Model struct {
	ID        *models.RecordID `gorm:"primaryKey;type:record;<-:create" json:"id,omitempty"`
	CreatedAt time.Time        `json:"created_at,omitempty"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
	DeletedAt DeletedAt        `gorm:"index;softDelete:true" json:"deleted_at,omitempty"`
}

type DeletedAt struct {
	gorm.DeletedAt
}

// UnmarshalJSON implements json.Unmarshaler.
// It handles simple string/null (standard) or the struct format (legacy/fallback).
// It handles simple string (time) or null (standard).
func (d *DeletedAt) UnmarshalJSON(data []byte) error {
	// 1. If null, it's valid (not deleted)
	if string(data) == "null" {
		d.Valid = false
		return nil
	}

	// 2. Try standard unmarshal (string -> time)
	// GORM's DeletedAt.UnmarshalJSON handles "null" and time strings.
	if err := d.DeletedAt.UnmarshalJSON(data); err == nil {
		return nil
	}

	// 3. Fallback for legacy object format (optional, keeping for safety if existing data)
	// only if starts with '{'
	if len(data) > 0 && data[0] == '{' {
		type Alias struct {
			Time  time.Time
			Valid bool
		}
		var aux Alias
		if err := json.Unmarshal(data, &aux); err == nil {
			d.Time = aux.Time
			d.Valid = aux.Valid
			return nil
		}
	}

	return nil
}

// MarshalJSON implements json.Marshaler.
// It ensures DeletedAt is serialized as null or time string, never an object.
func (d DeletedAt) MarshalJSON() ([]byte, error) {
	if !d.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(d.Time)
}
