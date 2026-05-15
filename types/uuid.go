package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// UUID is a string wrapper representing a SurrealDB UUID.
type UUID struct {
	String string
}

func NewUUID(v string) UUID {
	return UUID{String: v}
}

func (UUID) GormDataType() string { return "string" }

func (u UUID) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String)
}

func (u *UUID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("cannot unmarshal %s into UUID: %w", string(data), err)
	}
	u.String = s
	return nil
}

func (u *UUID) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		u.String = v
	case []byte:
		u.String = string(v)
	default:
		u.String = fmt.Sprintf("%v", v)
	}
	return nil
}

func (u UUID) Value() (driver.Value, error) {
	if u.String == "" {
		return nil, nil
	}
	return u.String, nil
}
