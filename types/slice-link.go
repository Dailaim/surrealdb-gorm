package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// SliceLink ayuda a GORM a entender que esto no es una relación, sino un array de links
type SliceLink[T any] []Link[T]

// Value implementa driver.Valuer
func (s SliceLink[T]) Value() (driver.Value, error) {
	// Retornamos bytes JSON para que GORM no trate de analizarlo como relación SQL
	return json.Marshal(s)
}

// Scan implementa sql.Scanner
func (s *SliceLink[T]) Scan(value interface{}) error {
	if value == nil {
		*s = nil
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
		*s = nil
		return nil
	}

	// Helper to check for basic types array vs object array?
	// Just unmarshal
	return json.Unmarshal(bytes, s)
}

// UnmarshalJSON delegating to slice unmarshal which calls Element UnmarshalJSON
func (s *SliceLink[T]) UnmarshalJSON(data []byte) error {
	// Need to type alias to avoid recursion if we just unmarshal to SliceLink
	// But we are SliceLink method.
	// Generic alias trick:
	// type Alias SliceLink[T]
	// But SliceLink is []Link[T].
	// json.Unmarshal to []Link[T] uses Link[T].UnmarshalJSON.
	var tmp []Link[T]
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*s = SliceLink[T](tmp)
	return nil
}

