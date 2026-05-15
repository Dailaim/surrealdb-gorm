package surrealdb_test

import (
	"context"
	"testing"
	"time"

	"github.com/dailaim/surrealdb-gorm/types"
	surrealdb "github.com/surrealdb/surrealdb.go"
	"gorm.io/gorm"
)

// TestUUIDType verifies UUID creation, marshal, unmarshal, scan and value.
func TestUUIDType(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	conn := getDialector(db).Conn

	// Insert via SurrealDB
	res, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		"CREATE types_test SET uid = u'550e8400-e29b-41d4-a716-446655440000'",
		nil,
	)
	if err != nil {
		t.Fatalf("create uuid: %v", err)
	}
	row := (*res)[0].Result[0]
	uidRaw := row["uid"]

	var uid types.UUID
	if err := uid.Scan(uidRaw); err != nil {
		t.Fatalf("Scan UUID: %v", err)
	}
	expected := "550e8400-e29b-41d4-a716-446655440000"
	if uid.String != expected {
		t.Errorf("UUID: expected %s, got %s", expected, uid.String)
	}

	// MarshalJSON
	b, err := uid.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"`+expected+`"` {
		t.Errorf("MarshalJSON: expected \"%s\", got %s", expected, string(b))
	}

	// Value
	v, err := uid.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != expected {
		t.Errorf("Value: expected %s, got %v", expected, v)
	}

	cleanupTypesTest(t, db)
}

// TestDecimalType verifies Decimal creation, marshal, unmarshal, scan and value.
func TestDecimalType(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	conn := getDialector(db).Conn

	// Insert via SurrealDB
	res, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		"CREATE types_test SET price = 123.456dec",
		nil,
	)
	if err != nil {
		t.Fatalf("create decimal: %v", err)
	}
	row := (*res)[0].Result[0]
	priceRaw := row["price"]

	var price types.Decimal
	if err := price.Scan(priceRaw); err != nil {
		t.Fatalf("Scan Decimal: %v", err)
	}
	if price.String() != "123.456" {
		t.Errorf("Decimal: expected 123.456, got %s", price.String())
	}

	// MarshalJSON
	b, err := price.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"123.456"` {
		t.Errorf("MarshalJSON: expected \"123.456\", got %s", string(b))
	}

	// Value
	v, err := price.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != "123.456" {
		t.Errorf("Value: expected 123.456, got %v", v)
	}

	cleanupTypesTest(t, db)
}

// TestDateTimeType verifies DateTime creation, marshal, unmarshal, scan and value.
func TestDateTimeType(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	conn := getDialector(db).Conn

	// Insert via SurrealDB
	res, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		"CREATE types_test SET created = d'2024-03-15T10:30:00.000Z'",
		nil,
	)
	if err != nil {
		t.Fatalf("create datetime: %v", err)
	}
	row := (*res)[0].Result[0]
	createdRaw := row["created"]

	var created types.DateTime
	if err := created.Scan(createdRaw); err != nil {
		t.Fatalf("Scan DateTime: %v", err)
	}
	expected := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	if !created.Time.Equal(expected) {
		t.Errorf("DateTime: expected %v, got %v", expected, created.Time)
	}

	// MarshalJSON
	b, err := created.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"2024-03-15T10:30:00.000Z"` {
		t.Errorf("MarshalJSON: got %s", string(b))
	}

	// Value
	v, err := created.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != "2024-03-15T10:30:00.000Z" {
		t.Errorf("Value: expected 2024-03-15T10:30:00.000Z, got %v", v)
	}

	cleanupTypesTest(t, db)
}

// TestRegexType verifies Regex creation, marshal, unmarshal, scan and value.
func TestRegexType(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	conn := getDialector(db).Conn

	// Regex se representa como string con delimitadores en SurrealDB.
	// Insertamos un string con formato de regex para probar Scan.
	res, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		`CREATE types_test SET pattern = '/[a-z]+/i'`,
		nil,
	)
	if err != nil {
		t.Fatalf("create regex: %v", err)
	}
	row := (*res)[0].Result[0]
	patternRaw := row["pattern"]

	var pattern types.Regex
	if err := pattern.Scan(patternRaw); err != nil {
		t.Fatalf("Scan Regex: %v", err)
	}
	if pattern.Pattern != `[a-z]+` {
		t.Errorf("Regex Pattern: expected [a-z]+, got %s", pattern.Pattern)
	}
	if pattern.Flags != "i" {
		t.Errorf("Regex Flags: expected i, got %s", pattern.Flags)
	}

	// MarshalJSON
	b, err := pattern.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"/[a-z]+/i"` {
		t.Errorf("MarshalJSON: got %s", string(b))
	}

	// Value
	v, err := pattern.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != `/[a-z]+/i` {
		t.Errorf("Value: expected /[a-z]+/i, got %v", v)
	}

	cleanupTypesTest(t, db)
}

func cleanupTypesTest(t *testing.T, db *gorm.DB) {
	if err := db.Exec("DELETE FROM types_test").Error; err != nil {
		t.Logf("cleanup warning: %v", err)
	}
}
