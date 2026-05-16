package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"

	"gorm.io/gorm/schema"
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

// MarshalJSON serializes the slice of links.
func (s SliceLink[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal([]Link[T](s))
}

// GormDataType returns the GORM data type for this slice of links.
// It tries to discover the target table for T (via TableName() or reflection)
// and returns array<record<tabla>> so that SurrealDB knows the exact reference.
func (SliceLink[T]) GormDataType() string {
	var zero T
	v := reflect.ValueOf(&zero).Elem()

	// Try pointer receiver: (*T).TableName()
	if v.CanAddr() {
		addr := v.Addr()
		if m := addr.MethodByName("TableName"); m.IsValid() {
			out := m.Call(nil)
			if len(out) == 1 && out[0].Kind() == reflect.String {
				return fmt.Sprintf("array<record<%s>>", out[0].String())
			}
		}
	}

	// Try value receiver: (T).TableName()
	if m := v.MethodByName("TableName"); m.IsValid() {
		out := m.Call(nil)
		if len(out) == 1 && out[0].Kind() == reflect.String {
			return fmt.Sprintf("array<record<%s>>", out[0].String())
		}
	}

	// Fallback: derive table name from the type name using GORM naming strategy.
	rt := v.Type()
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if rt.Kind() == reflect.Struct {
		ns := schema.NamingStrategy{}
		table := ns.TableName(rt.Name())
		return fmt.Sprintf("array<record<%s>>", table)
	}

	return "array"
}
