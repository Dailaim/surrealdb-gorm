package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/surrealdb/surrealdb.go/pkg/models"
)

// DateTime wraps time.Time for SurrealDB datetime support.
// It serializes to/from ISO8601 format.
type DateTime struct {
	time.Time
}

func NewDateTime(t time.Time) DateTime {
	return DateTime{Time: t}
}

func (DateTime) GormDataType() string { return "datetime" }

const iso8601Layout = "2006-01-02T15:04:05.000Z"

func (d DateTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Format(iso8601Layout))
}

func (d *DateTime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("cannot unmarshal %s into DateTime: %w", string(data), err)
	}
	t, err := time.Parse(iso8601Layout, s)
	if err != nil {
		// Fallback to RFC3339
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return err
		}
	}
	d.Time = t
	return nil
}

func (d *DateTime) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		d.Time = v
		return nil
	case models.CustomDateTime:
		d.Time = v.Time
		return nil
	case string:
		t, err := time.Parse(iso8601Layout, v)
		if err != nil {
			t, err = time.Parse(time.RFC3339, v)
			if err != nil {
				return err
			}
		}
		d.Time = t
		return nil
	case []byte:
		return d.Scan(string(v))
	default:
		return fmt.Errorf("cannot scan %T into DateTime", value)
	}
}

func (d DateTime) Value() (driver.Value, error) {
	if d.Time.IsZero() {
		return nil, nil
	}
	return d.Format(iso8601Layout), nil
}
