package types

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// stringLikeToFloat64 tries to parse a string-like value (including SurrealDB
// DecimalString and similar custom string types) into a float64.
func stringLikeToFloat64(v interface{}) (float64, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.String {
		s := rv.String()
		// Strip SurrealDB suffixes like 'dec' or 'f' before parsing
		clean := strings.TrimSuffix(strings.TrimSuffix(s, "dec"), "f")
		if f, err := strconv.ParseFloat(clean, 64); err == nil {
			return f, nil
		}
		return 0, fmt.Errorf("cannot convert string-like %q to float64", s)
	}
	return 0, fmt.Errorf("cannot convert %T to float64", v)
}

// stringLikeToInt64 tries to parse a string-like value into an int64.
func stringLikeToInt64(v interface{}) (int64, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.String {
		s := rv.String()
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, nil
		}
		return 0, fmt.Errorf("cannot convert string-like %q to int64", s)
	}
	return 0, fmt.Errorf("cannot convert %T to int64", v)
}

// ToFloat64 coerces a SurrealDB numeric value (which may come back as float64,
// int64, uint64, json.Number, or DecimalString) into a Go float64.
func ToFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, err
		}
		return f, nil
	case string:
		return stringLikeToFloat64(n)
	default:
		// Handles custom string types like sdkModels.DecimalString
		if f, err := stringLikeToFloat64(v); err == nil {
			return f, nil
		}
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}

// ToInt64 coerces a SurrealDB numeric value into a Go int64.
func ToInt64(v interface{}) (int64, error) {
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case float32:
		return int64(n), nil
	case int:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint:
		return int64(n), nil
	case uint8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, err
		}
		return i, nil
	case string:
		return stringLikeToInt64(n)
	default:
		// Handles custom string types like sdkModels.DecimalString
		if i, err := stringLikeToInt64(v); err == nil {
			return i, nil
		}
		return 0, fmt.Errorf("cannot convert %T to int64", v)
	}
}

// ToUint64 coerces a SurrealDB numeric value into a Go uint64.
func ToUint64(v interface{}) (uint64, error) {
	switch n := v.(type) {
	case float64:
		return uint64(n), nil
	case float32:
		return uint64(n), nil
	case int:
		return uint64(n), nil
	case int8:
		return uint64(n), nil
	case int16:
		return uint64(n), nil
	case int32:
		return uint64(n), nil
	case int64:
		return uint64(n), nil
	case uint:
		return uint64(n), nil
	case uint8:
		return uint64(n), nil
	case uint16:
		return uint64(n), nil
	case uint32:
		return uint64(n), nil
	case uint64:
		return n, nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, err
		}
		return uint64(i), nil
	case string:
		if u, err := strconv.ParseUint(n, 10, 64); err == nil {
			return u, nil
		}
		return 0, fmt.Errorf("cannot convert string %q to uint64", n)
	default:
		// Handles custom string types
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.String {
			if u, err := strconv.ParseUint(rv.String(), 10, 64); err == nil {
				return u, nil
			}
			return 0, fmt.Errorf("cannot convert string-like %q to uint64", rv.String())
		}
		return 0, fmt.Errorf("cannot convert %T to uint64", v)
	}
}

// MustFloat64 is like ToFloat64 but panics on error.
func MustFloat64(v interface{}) float64 {
	f, err := ToFloat64(v)
	if err != nil {
		panic(err)
	}
	return f
}

// MustInt64 is like ToInt64 but panics on error.
func MustInt64(v interface{}) int64 {
	i, err := ToInt64(v)
	if err != nil {
		panic(err)
	}
	return i
}

// MustUint64 is like ToUint64 but panics on error.
func MustUint64(v interface{}) uint64 {
	u, err := ToUint64(v)
	if err != nil {
		panic(err)
	}
	return u
}

// IsSurrealNumber reports whether v is a numeric type that SurrealDB may return.
func IsSurrealNumber(v interface{}) bool {
	switch v.(type) {
	case float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, json.Number:
		return true
	default:
		return false
	}
}

// SafeIntFromFloat64 converts a float64 to int64, checking that the value
// is within the safe integer range for float64 (±2^53).
func SafeIntFromFloat64(f float64) (int64, error) {
	if f < -9007199254740992 || f > 9007199254740992 {
		return 0, fmt.Errorf("float64 %f exceeds safe integer range", f)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("float64 is NaN or Inf")
	}
	return int64(f), nil
}
