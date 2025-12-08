package types

import (
	"encoding/json"
	"reflect"
	"strings"
)

// SurrealMapToStruct populates a struct from a map, respecting GORM tags for column names.
// It fallbacks to JSON tag or field name if GORM tag is missing or not found in map.
func SurrealMapToStruct(dest interface{}, data interface{}) error {
	// If data is bytes, unmarshal to map first (or direct to struct if simple)
	// But we need to handle the map keys manually.

	// Convert data to map[string]interface{} (if it's not already)
	// Or handling map directly.

	var dataMap map[string]interface{}

	val := reflect.ValueOf(data)
	if val.Kind() == reflect.Map {
		// Convert map[?] to map[string]
		if _, ok := data.(map[string]interface{}); ok {
			dataMap = data.(map[string]interface{})
		} else {
			// Marshaling/Unmarshaling is safe way to normalize
			b, err := json.Marshal(data)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(b, &dataMap); err != nil {
				return err
			}
		}
	} else if b, ok := data.([]byte); ok {
		if err := json.Unmarshal(b, &dataMap); err != nil {
			// Maybe it's not a map (object), so we fallback to standard unmarshal
			return json.Unmarshal(b, dest)
		}
	} else {
		// Unmarshal anything else via JSON
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &dataMap); err != nil {
			return json.Unmarshal(b, dest)
		}
	}

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() == reflect.Ptr {
		destVal = destVal.Elem()
	}
	if destVal.Kind() != reflect.Struct {
		// Not a struct, standard Unmarshal
		b, _ := json.Marshal(dataMap)
		return json.Unmarshal(b, dest)
	}

	return populateStruct(destVal, dataMap)
}

func populateStruct(destVal reflect.Value, dataMap map[string]interface{}) error {
	destType := destVal.Type()

	for i := 0; i < destType.NumField(); i++ {
		field := destType.Field(i)
		fieldVal := destVal.Field(i)

		// Handle Embedded Structs (Anonymous)
		if field.Anonymous {
			// Recurse into embedded struct
			// We check if it is a struct or pointer to struct
			embeddedVal := fieldVal
			if embeddedVal.Kind() == reflect.Ptr {
				// If nil, allocate?
				if embeddedVal.IsNil() {
					embeddedVal.Set(reflect.New(embeddedVal.Type().Elem()))
				}
				embeddedVal = embeddedVal.Elem()
			}

			if embeddedVal.Kind() == reflect.Struct {
				if err := populateStruct(embeddedVal, dataMap); err != nil {
					return err
				}
			}
			continue
		}

		// Check GORM tag
		fieldName := field.Name
		dbName := fieldName
		// GORM standard is Snake Case but here we only care if explicitly tagged?
		// We should match loosely or check explicit tag.

		gormTag := field.Tag.Get("gorm")
		if gormTag != "" {
			// Parse column:xxx
			parts := strings.Split(gormTag, ";")
			for _, p := range parts {
				if strings.HasPrefix(p, "column:") {
					dbName = strings.TrimPrefix(p, "column:")
					break
				}
			}
		}

		// Try to find value for dbName
		var val interface{}
		var found bool

		// Check exact match
		if v, ok := dataMap[dbName]; ok {
			val = v
			found = true
		} else {
			// Fallback: Check JSON tag
			jsonTag := field.Tag.Get("json")
			if jsonTag != "" {
				name := strings.Split(jsonTag, ",")[0]
				if name != "-" {
					if v, ok := dataMap[name]; ok {
						val = v
						found = true
					}
				}
			}
		}
		if !found {
			// Fallback: Check snake_case of dbName/fieldName
			snake := toSnakeCase(dbName)
			if v, ok := dataMap[snake]; ok {
				val = v
				found = true
			} else {
				// Fallback: Check lowercase
				lower := strings.ToLower(dbName)
				if v, ok := dataMap[lower]; ok {
					val = v
					found = true
				}
			}
		}

		if !found {
			// Also try snake_case of dbName/fieldName?
			// Optional, but good for robustness.
			continue
		}

		// Set Field
		if fieldVal.CanSet() {
			// Use JSON roundtrip to handle type conversion (safe)
			b, err := json.Marshal(val)
			if err == nil {
				json.Unmarshal(b, fieldVal.Addr().Interface())
			}
		}
	}
	return nil
}

func toSnakeCase(str string) string {
	var matchFirstCap = true
	// Simple implementation
	// "Title" -> "title"
	// "MyTitle" -> "my_title"
	// "myField" -> "my_field"

	// A basic implementation or just strict dependency?
	// Let's use a simple builder.
	var b strings.Builder
	for i, r := range str {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && !matchFirstCap && (i+1 < len(str) && str[i+1] >= 'a' && str[i+1] <= 'z') {
				b.WriteRune('_')
			}
			// Another heuristic for acronyms? ID -> id.
			// Let's stick to standard GORM-like
			if i > 0 && (str[i-1] >= 'a' && str[i-1] <= 'z') {
				b.WriteRune('_')
			}
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
		matchFirstCap = false // Not used essentially here
	}
	return b.String()
}
