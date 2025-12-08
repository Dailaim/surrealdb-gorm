package types

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
)

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

func (d *DeletedAt) SurrealString() string {
	return fmt.Sprintf("<datetime> '%s'", d.Time.String())
}

func (d *DeletedAt) MarshalCBOR() ([]byte, error) {

	if !d.Valid {
		customNil := models.CustomNil{}
		return customNil.MarshalCBOR()
	}

	customTime := models.CustomDateTime{
		Time: d.Time,
	}
	return customTime.MarshalCBOR()
}

func (d *DeletedAt) UnmarshalCBOR(data []byte) error {
	customTime := new(models.CustomDateTime)
	if err := customTime.UnmarshalCBOR(data); err != nil {
		return err
	}

	if customTime.Time.IsZero() {
		d.Valid = false
		return nil
	}

	d.Time = customTime.Time
	d.Valid = true

	return nil
}
