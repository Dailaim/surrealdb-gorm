package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/surrealdb/surrealdb.go/pkg/models"
)

type RecordID struct {
	models.RecordID
}

// String returns the standard SurrealDB record ID format: "table:id".
func (r RecordID) String() string {
	return fmt.Sprintf("%s:%v", r.Table, r.ID)
}

// StringToRecordID parses a "table:id" string into this RecordID.
func (r *RecordID) StringToRecordID(s string) error {
	parsed, err := models.ParseRecordID(s)
	if err != nil {
		return err
	}
	r.RecordID = *parsed
	return nil
}

// MarshalJSON serializes the RecordID as a SurrealDB string: "table:id".
func (r RecordID) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON accepts either a string "table:id" or the SDK's object format
// {"Table":"x","ID":"y"}.
func (r *RecordID) UnmarshalJSON(data []byte) error {
	// 1. Try as a plain string (most common SurrealDB format)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return r.StringToRecordID(s)
	}

	// 2. Fallback to SDK object format
	var raw models.RecordID
	if err := json.Unmarshal(data, &raw); err == nil {
		r.RecordID = raw
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into RecordID", string(data))
}

// Scan implements sql.Scanner.
// It accepts: *models.RecordID, string, []byte, or map[string]interface{}.
func (r *RecordID) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case models.RecordID:
		r.RecordID = v
		return nil
	case *models.RecordID:
		if v != nil {
			r.RecordID = *v
		}
		return nil
	case RecordID:
		r.RecordID = v.RecordID
		return nil
	case *RecordID:
		if v != nil {
			r.RecordID = v.RecordID
		}
		return nil
	case string:
		return r.StringToRecordID(v)
	case []byte:
		return r.StringToRecordID(string(v))
	case map[string]interface{}:
		// Reconstruct from JSON map
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return r.UnmarshalJSON(b)
	default:
		// Last resort: try JSON round-trip
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("cannot scan %T into RecordID", value)
		}
		return r.UnmarshalJSON(b)
	}
}

// Value implements driver.Valuer.
func (r RecordID) Value() (driver.Value, error) {
	return r.String(), nil
}

func (RecordID) GormDataType() string {
	return "record"
}
