package surrealdb_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/dailaim/surrealdb-gorm"
)

// AllTypes struct to verify support for all requested data types
type AllTypes struct {
	surrealdb.Model
	// Basic Types
	Bool   bool
	Int    int
	Float  float64
	String string
	Bytes  []byte // Should be base64 encoded by json.Marshal

	// Collections
	ArrayString []string          `gorm:"type:array"`
	ArrayInt    []int             `gorm:"type:array"`
	MapString   map[string]string `gorm:"type:object"`
	MapInt      map[string]int    `gorm:"type:object"`

	// Time
	Time     time.Time
	Duration surrealdb.Duration // Uses our custom wrapper

	// Simple Geometry
	Location   surrealdb.GeometryPoint // Uses our custom wrapper which has GormDataType
	LineString surrealdb.GeometryLine
	Polygon    surrealdb.GeometryPolygon
	MultiPoint surrealdb.GeometryMultiPoint
	MultiLine  surrealdb.GeometryMultiLineString
	MultiPoly  surrealdb.GeometryMultiPolygon
	Collection surrealdb.GeometryCollection

	// UUID (simulated as string, or user can use google/uuid which marshals to string)
	UUID string `gorm:"type:string"`
}

func (AllTypes) TableName() string {
	return "all_types"
}

func TestAllTypes(t *testing.T) {
	db := setupDB(t)

	// AutoMigrate
	if err := db.AutoMigrate(&AllTypes{}); err != nil {
		t.Fatalf("Failed to migrate AllTypes: %v", err)
	}

	// Prepare data
	now := time.Now().Round(time.Second) // Round for comparison stability
	// Coordinates [lon, lat]
	loc := surrealdb.NewPoint(-0.118092, 51.509865)

	// Duration: 1h30m
	durBase, _ := time.ParseDuration("1h30m")
	dur := surrealdb.Duration{Duration: durBase}

	// Other Geometries
	line := surrealdb.NewLineString([][]float64{{0, 0}, {1, 1}})
	poly := surrealdb.NewPolygon([][][]float64{{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {0, 0}}})
	mpoint := surrealdb.NewMultiPoint([][]float64{{0, 0}, {1, 1}})
	mline := surrealdb.NewMultiLineString([][][]float64{{{0, 0}, {1, 1}}, {{2, 2}, {3, 3}}})
	mpoly := surrealdb.NewMultiPolygon([][][][]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {0, 0}}}})
	// Collection containing a point
	coll := surrealdb.NewGeometryCollection([]interface{}{
		surrealdb.NewPoint(10, 10),
	})

	data := AllTypes{
		Bool:        true,
		Int:         42,
		Float:       3.14159,
		String:      "Hello Surreal",
		Bytes:       []byte("SurrealDB"),
		ArrayString: []string{"A", "B", "C"},
		ArrayInt:    []int{1, 2, 3},
		MapString:   map[string]string{"foo": "bar"},
		MapInt:      map[string]int{"one": 1},
		Time:        now,
		Duration:    dur,
		Location:    loc,
		LineString:  line,
		Polygon:     poly,
		MultiPoint:  mpoint,
		MultiLine:   mline,
		MultiPoly:   mpoly,
		Collection:  coll,
		UUID:        "550e8400-e29b-41d4-a716-446655440000",
	}

	// Create
	if err := db.Create(&data).Error; err != nil {
		t.Fatalf("Failed to create AllTypes: %v", err)
	}
	t.Logf("Created AllTypes ID: %v", data.ID)

	// Read back
	var found AllTypes
	if err := db.Table("all_types").First(&found, "id = ?", data.ID).Error; err != nil {
		t.Fatalf("Failed to find AllTypes: %v", err)
	}

	// Verify Basic
	if found.Bool != data.Bool {
		t.Errorf("Bool mismatch: got %v, want %v", found.Bool, data.Bool)
	}
	if found.Int != data.Int {
		t.Errorf("Int mismatch: got %v, want %v", found.Int, data.Int)
	}
	if found.Float != data.Float {
		t.Errorf("Float mismatch: got %v, want %v", found.Float, data.Float)
	}
	if found.String != data.String {
		t.Errorf("String mismatch: got %v, want %v", found.String, data.String)
	}
	// Bytes usually marshal to base64 string in JSON. Go unmarshals automatically.
	if string(found.Bytes) != string(data.Bytes) {
		t.Errorf("Bytes mismatch: got %s, want %s", found.Bytes, data.Bytes)
	}

	// Verify Collections
	if len(found.ArrayString) != 3 || found.ArrayString[0] != "A" {
		t.Errorf("ArrayString mismatch: got %v", found.ArrayString)
	}
	if len(found.MapString) != 1 || found.MapString["foo"] != "bar" {
		t.Errorf("MapString mismatch: got %v", found.MapString)
	}

	// Verify Time
	if !found.Time.Equal(data.Time) {
		t.Errorf("Time mismatch: got %v, want %v", found.Time, data.Time)
	}

	// Verify Duration
	// Note: time.Duration marshals to int (nanoseconds) by default in generic JSON.
	// SurrealDB "duration" type is different (string like "1h").
	// Let's see how it behaves round-trip as int.
	if found.Duration != data.Duration {
		t.Errorf("Duration mismatch: got %v, want %v", found.Duration, data.Duration)
	}

	// Verify Geometry
	if found.Location.Type != loc.Type {
		t.Errorf("Location Type mismatch: got %v, want %v", found.Location.Type, loc.Type)
	}
	if len(found.Location.Coordinates) != 2 || found.Location.Coordinates[0] != loc.Coordinates[0] {
		t.Errorf("Location Coordinates mismatch: got %v, want %v", found.Location.Coordinates, loc.Coordinates)
	}

	// Verify MultiPoint presence (shallow check)
	if found.MultiPoint.Type != "MultiPoint" {
		t.Errorf("MultiPoint Type mismatch")
	}
	if len(found.Collection.Geometries) == 0 {
		t.Errorf("Collection empty")
	}
}

// TestBytesDirect verification
func TestBytesDirect(t *testing.T) {
	db := setupDB(t)

	// Define a struct to read back results
	type BytesStruct struct {
		surrealdb.Model
		Data surrealdb.Bytes `gorm:"type:bytes"`
	}

	// Test using db.Create to verify MarshalJSON path
	table := "bytes_structs" // GORM default pluralized
	// Clean previous run
	db.Exec(fmt.Sprintf("DELETE %s", table))

	// AutoMigrate should set field type bytes based on GormDataType()
	if err := db.AutoMigrate(&BytesStruct{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	payload := []byte("Direct Bytes Payload")
	item := BytesStruct{
		Data: surrealdb.Bytes(payload),
	}

	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("Failed to create bytes struct: %v", err)
	}
	t.Logf("Created ID: %s", item.ID)

	// Verify what's in DB using map
	var mapResults []map[string]interface{}
	// Use Find to fetch all (should be 1)
	if err := db.Table(table).Find(&mapResults).Error; err != nil {
		t.Fatalf("Failed to scan map: %v", err)
	}
	t.Logf("Raw DB Map: %+v", mapResults)

	if len(mapResults) > 0 {
		data := mapResults[0]["Data"]
		t.Logf("Data Type: %T, Value: %v", data, data)
	}

	if len(mapResults) == 0 {
		t.Fatalf("No records found")
	}

	// Read back using GORM Table/First
	var result BytesStruct
	if err := db.First(&result, "id = ?", item.ID).Error; err != nil {
		t.Fatalf("Failed to read back: %v", err)
	}

	if string(result.Data) != string(payload) {
		t.Errorf("Bytes mismatch: got %q, want %q", result.Data, payload)
	}
}
