package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/surrealdb/surrealdb.go/pkg/models"
)

// Decimal wraps shopspring/decimal.Decimal for SurrealDB decimal support.
// SurrealDB may return decimals as models.DecimalString (e.g. "123.456dec").
type Decimal struct {
	decimal.Decimal
}

func NewDecimal(v string) (Decimal, error) {
	d, err := decimal.NewFromString(v)
	if err != nil {
		return Decimal{}, err
	}
	return Decimal{Decimal: d}, nil
}

func (Decimal) GormDataType() string { return "decimal" }

func (d Decimal) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Decimal) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("cannot unmarshal %s into Decimal: %w", string(data), err)
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return err
	}
	d.Decimal = v
	return nil
}

func (d *Decimal) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case decimal.Decimal:
		d.Decimal = v
		return nil
	case string:
		// Handle SurrealDB DecimalString suffix "dec"
		var s = v
		if len(s) > 3 && s[len(s)-3:] == "dec" {
			s = s[:len(s)-3]
		}
		val, err := decimal.NewFromString(s)
		if err != nil {
			return err
		}
		d.Decimal = val
		return nil
	case []byte:
		return d.Scan(string(v))
	case float64:
		d.Decimal = decimal.NewFromFloat(v)
		return nil
	case int64:
		d.Decimal = decimal.NewFromInt(v)
		return nil
	case int:
		d.Decimal = decimal.NewFromInt(int64(v))
		return nil
	case models.DecimalString:
		return d.Scan(string(v))
	default:
		return fmt.Errorf("cannot scan %T into Decimal", value)
	}
}

func (d Decimal) Value() (driver.Value, error) {
	if d.Decimal.IsZero() {
		return nil, nil
	}
	return d.String(), nil
}
