package types

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/surrealdb/surrealdb.go/pkg/models"
)

// Duration wraps time.Duration to marshal as SurrealDB duration string (e.g. "1h30m")
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	// 1. Try as plain string (e.g. "1h30m")
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	}

	// 2. Try as plain number (nanoseconds)
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		d.Duration = time.Duration(n)
		return nil
	}

	// 3. Try as SDK CustomDuration object: {"Duration":5400000000000}
	var obj struct {
		Duration float64 `json:"Duration"`
	}
	if err := json.Unmarshal(b, &obj); err == nil && obj.Duration != 0 {
		d.Duration = time.Duration(obj.Duration)
		return nil
	}

	return errors.New("invalid duration format")
}

// Value implements driver.Valuer
func (d Duration) Value() (driver.Value, error) {
	return d.String(), nil
}

// Scan implements sql.Scanner
func (d *Duration) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	case int64:
		d.Duration = time.Duration(v)
		return nil
	case float64:
		d.Duration = time.Duration(v)
		return nil
	case time.Duration:
		d.Duration = v
		return nil
	case models.CustomDuration:
		d.Duration = v.Duration
		return nil
	case *models.CustomDuration:
		d.Duration = v.Duration
		return nil
	case models.CustomDurationString:
		parsed, err := time.ParseDuration(string(v))
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	default:
		return fmt.Errorf("invalid duration db value: %T", value)
	}
}

func (Duration) GormDataType() string {
	return "string"
}

// Geometry types (manual GeoJSON)
// All geometry types implement json.Marshaler/Unmarshaler, sql.Scanner and driver.Valuer.
// The marshal/unmarshal implementations use a type alias to avoid infinite recursion.

type GeometryPoint struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"`
}

func NewPoint(lon, lat float64) GeometryPoint {
	return GeometryPoint{Type: "Point", Coordinates: []float64{lon, lat}}
}

func (GeometryPoint) GormDataType() string { return "geometry(point)" }
func (g GeometryPoint) MarshalJSON() ([]byte, error) {
	type Alias GeometryPoint
	return json.Marshal(Alias(g))
}
func (g *GeometryPoint) UnmarshalJSON(d []byte) error {
	type Alias GeometryPoint
	if err := json.Unmarshal(d, (*Alias)(g)); err == nil && g.Type != "" {
		return nil
	}
	// Fallback to SurrealDB tuple format: [-0.118092, 51.509865]
	var arr []float64
	if err := json.Unmarshal(d, &arr); err == nil && len(arr) == 2 {
		*g = NewPoint(arr[0], arr[1])
		return nil
	}
	// Fallback to SDK format: {"Longitude":-0.118092,"Latitude":51.509865}
	var sdk struct {
		Longitude float64 `json:"Longitude"`
		Latitude  float64 `json:"Latitude"`
	}
	if err := json.Unmarshal(d, &sdk); err != nil {
		return err
	}
	*g = NewPoint(sdk.Longitude, sdk.Latitude)
	return nil
}
func (g *GeometryPoint) Scan(v interface{}) error {
	switch val := v.(type) {
	case models.GeometryPoint:
		*g = NewPoint(val.Longitude, val.Latitude)
		return nil
	case *models.GeometryPoint:
		*g = NewPoint(val.Longitude, val.Latitude)
		return nil
	}
	return scanGeoJSON(v, g)
}
func (g GeometryPoint) Value() (driver.Value, error) {
	type Alias GeometryPoint
	return json.Marshal(Alias(g))
}

type GeometryLine struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

func NewLineString(coords [][]float64) GeometryLine {
	return GeometryLine{Type: "LineString", Coordinates: coords}
}

func (GeometryLine) GormDataType() string { return "geometry(linestring)" }
func (g GeometryLine) MarshalJSON() ([]byte, error) {
	type Alias GeometryLine
	return json.Marshal(Alias(g))
}
func (g *GeometryLine) UnmarshalJSON(d []byte) error {
	type Alias GeometryLine
	if err := json.Unmarshal(d, (*Alias)(g)); err == nil && g.Type != "" {
		return nil
	}
	var sdk []struct {
		Longitude float64 `json:"Longitude"`
		Latitude  float64 `json:"Latitude"`
	}
	if err := json.Unmarshal(d, &sdk); err != nil {
		return err
	}
	coords := make([][]float64, len(sdk))
	for i, p := range sdk {
		coords[i] = []float64{p.Longitude, p.Latitude}
	}
	*g = GeometryLine{Type: "LineString", Coordinates: coords}
	return nil
}
func (g *GeometryLine) Scan(v interface{}) error {
	switch val := v.(type) {
	case models.GeometryLine:
		coords := make([][]float64, len(val))
		for i, p := range val {
			coords[i] = []float64{p.Longitude, p.Latitude}
		}
		*g = GeometryLine{Type: "LineString", Coordinates: coords}
		return nil
	case *models.GeometryLine:
		coords := make([][]float64, len(*val))
		for i, p := range *val {
			coords[i] = []float64{p.Longitude, p.Latitude}
		}
		*g = GeometryLine{Type: "LineString", Coordinates: coords}
		return nil
	}
	return scanGeoJSON(v, g)
}
func (g GeometryLine) Value() (driver.Value, error) {
	type Alias GeometryLine
	return json.Marshal(Alias(g))
}

type GeometryPolygon struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

func NewPolygon(coords [][][]float64) GeometryPolygon {
	return GeometryPolygon{Type: "Polygon", Coordinates: coords}
}

func (GeometryPolygon) GormDataType() string { return "geometry(polygon)" }
func (g GeometryPolygon) MarshalJSON() ([]byte, error) {
	type Alias GeometryPolygon
	return json.Marshal(Alias(g))
}
func (g *GeometryPolygon) UnmarshalJSON(d []byte) error {
	type Alias GeometryPolygon
	if err := json.Unmarshal(d, (*Alias)(g)); err == nil && g.Type != "" {
		return nil
	}
	var sdk [][]struct {
		Longitude float64 `json:"Longitude"`
		Latitude  float64 `json:"Latitude"`
	}
	if err := json.Unmarshal(d, &sdk); err != nil {
		return err
	}
	coords := make([][][]float64, len(sdk))
	for i, line := range sdk {
		coords[i] = make([][]float64, len(line))
		for j, p := range line {
			coords[i][j] = []float64{p.Longitude, p.Latitude}
		}
	}
	*g = GeometryPolygon{Type: "Polygon", Coordinates: coords}
	return nil
}
func (g *GeometryPolygon) Scan(v interface{}) error {
	switch val := v.(type) {
	case models.GeometryPolygon:
		coords := make([][][]float64, len(val))
		for i, ring := range val {
			coords[i] = make([][]float64, len(ring))
			for j, p := range ring {
				coords[i][j] = []float64{p.Longitude, p.Latitude}
			}
		}
		*g = GeometryPolygon{Type: "Polygon", Coordinates: coords}
		return nil
	case *models.GeometryPolygon:
		coords := make([][][]float64, len(*val))
		for i, ring := range *val {
			coords[i] = make([][]float64, len(ring))
			for j, p := range ring {
				coords[i][j] = []float64{p.Longitude, p.Latitude}
			}
		}
		*g = GeometryPolygon{Type: "Polygon", Coordinates: coords}
		return nil
	}
	return scanGeoJSON(v, g)
}
func (g GeometryPolygon) Value() (driver.Value, error) {
	type Alias GeometryPolygon
	return json.Marshal(Alias(g))
}

type GeometryMultiPoint struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

func NewMultiPoint(coords [][]float64) GeometryMultiPoint {
	return GeometryMultiPoint{Type: "MultiPoint", Coordinates: coords}
}

func (GeometryMultiPoint) GormDataType() string { return "geometry(multipoint)" }
func (g GeometryMultiPoint) MarshalJSON() ([]byte, error) {
	type Alias GeometryMultiPoint
	return json.Marshal(Alias(g))
}
func (g *GeometryMultiPoint) UnmarshalJSON(d []byte) error {
	type Alias GeometryMultiPoint
	if err := json.Unmarshal(d, (*Alias)(g)); err == nil && g.Type != "" {
		return nil
	}
	var sdk []struct {
		Longitude float64 `json:"Longitude"`
		Latitude  float64 `json:"Latitude"`
	}
	if err := json.Unmarshal(d, &sdk); err != nil {
		return err
	}
	coords := make([][]float64, len(sdk))
	for i, p := range sdk {
		coords[i] = []float64{p.Longitude, p.Latitude}
	}
	*g = GeometryMultiPoint{Type: "MultiPoint", Coordinates: coords}
	return nil
}
func (g *GeometryMultiPoint) Scan(v interface{}) error {
	switch val := v.(type) {
	case models.GeometryMultiPoint:
		coords := make([][]float64, len(val))
		for i, p := range val {
			coords[i] = []float64{p.Longitude, p.Latitude}
		}
		*g = GeometryMultiPoint{Type: "MultiPoint", Coordinates: coords}
		return nil
	case *models.GeometryMultiPoint:
		coords := make([][]float64, len(*val))
		for i, p := range *val {
			coords[i] = []float64{p.Longitude, p.Latitude}
		}
		*g = GeometryMultiPoint{Type: "MultiPoint", Coordinates: coords}
		return nil
	}
	return scanGeoJSON(v, g)
}
func (g GeometryMultiPoint) Value() (driver.Value, error) {
	type Alias GeometryMultiPoint
	return json.Marshal(Alias(g))
}

type GeometryMultiLineString struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

func NewMultiLineString(coords [][][]float64) GeometryMultiLineString {
	return GeometryMultiLineString{Type: "MultiLineString", Coordinates: coords}
}

func (GeometryMultiLineString) GormDataType() string { return "geometry(multilinestring)" }
func (g GeometryMultiLineString) MarshalJSON() ([]byte, error) {
	type Alias GeometryMultiLineString
	return json.Marshal(Alias(g))
}
func (g *GeometryMultiLineString) UnmarshalJSON(d []byte) error {
	type Alias GeometryMultiLineString
	if err := json.Unmarshal(d, (*Alias)(g)); err == nil && g.Type != "" {
		return nil
	}
	var sdk [][]struct {
		Longitude float64 `json:"Longitude"`
		Latitude  float64 `json:"Latitude"`
	}
	if err := json.Unmarshal(d, &sdk); err != nil {
		return err
	}
	coords := make([][][]float64, len(sdk))
	for i, line := range sdk {
		coords[i] = make([][]float64, len(line))
		for j, p := range line {
			coords[i][j] = []float64{p.Longitude, p.Latitude}
		}
	}
	*g = GeometryMultiLineString{Type: "MultiLineString", Coordinates: coords}
	return nil
}
func (g *GeometryMultiLineString) Scan(v interface{}) error {
	switch val := v.(type) {
	case models.GeometryMultiLine:
		coords := make([][][]float64, len(val))
		for i, line := range val {
			lineCoords := make([][]float64, len(line))
			for j, p := range line {
				lineCoords[j] = []float64{p.Longitude, p.Latitude}
			}
			coords[i] = lineCoords
		}
		*g = GeometryMultiLineString{Type: "MultiLineString", Coordinates: coords}
		return nil
	case *models.GeometryMultiLine:
		coords := make([][][]float64, len(*val))
		for i, line := range *val {
			lineCoords := make([][]float64, len(line))
			for j, p := range line {
				lineCoords[j] = []float64{p.Longitude, p.Latitude}
			}
			coords[i] = lineCoords
		}
		*g = GeometryMultiLineString{Type: "MultiLineString", Coordinates: coords}
		return nil
	}
	return scanGeoJSON(v, g)
}
func (g GeometryMultiLineString) Value() (driver.Value, error) {
	type Alias GeometryMultiLineString
	return json.Marshal(Alias(g))
}

type GeometryMultiPolygon struct {
	Type        string          `json:"type"`
	Coordinates [][][][]float64 `json:"coordinates"`
}

func NewMultiPolygon(coords [][][][]float64) GeometryMultiPolygon {
	return GeometryMultiPolygon{Type: "MultiPolygon", Coordinates: coords}
}

func (GeometryMultiPolygon) GormDataType() string { return "geometry(multipolygon)" }
func (g GeometryMultiPolygon) MarshalJSON() ([]byte, error) {
	type Alias GeometryMultiPolygon
	return json.Marshal(Alias(g))
}
func (g *GeometryMultiPolygon) UnmarshalJSON(d []byte) error {
	type Alias GeometryMultiPolygon
	if err := json.Unmarshal(d, (*Alias)(g)); err == nil && g.Type != "" {
		return nil
	}
	var sdk [][][]struct {
		Longitude float64 `json:"Longitude"`
		Latitude  float64 `json:"Latitude"`
	}
	if err := json.Unmarshal(d, &sdk); err != nil {
		return err
	}
	coords := make([][][][]float64, len(sdk))
	for i, polygon := range sdk {
		coords[i] = make([][][]float64, len(polygon))
		for j, line := range polygon {
			coords[i][j] = make([][]float64, len(line))
			for k, p := range line {
				coords[i][j][k] = []float64{p.Longitude, p.Latitude}
			}
		}
	}
	*g = GeometryMultiPolygon{Type: "MultiPolygon", Coordinates: coords}
	return nil
}
func (g *GeometryMultiPolygon) Scan(v interface{}) error {
	switch val := v.(type) {
	case models.GeometryMultiPolygon:
		coords := make([][][][]float64, len(val))
		for i, polygon := range val {
			polyCoords := make([][][]float64, len(polygon))
			for j, line := range polygon {
				lineCoords := make([][]float64, len(line))
				for k, p := range line {
					lineCoords[k] = []float64{p.Longitude, p.Latitude}
				}
				polyCoords[j] = lineCoords
			}
			coords[i] = polyCoords
		}
		*g = GeometryMultiPolygon{Type: "MultiPolygon", Coordinates: coords}
		return nil
	case *models.GeometryMultiPolygon:
		coords := make([][][][]float64, len(*val))
		for i, polygon := range *val {
			polyCoords := make([][][]float64, len(polygon))
			for j, line := range polygon {
				lineCoords := make([][]float64, len(line))
				for k, p := range line {
					lineCoords[k] = []float64{p.Longitude, p.Latitude}
				}
				polyCoords[j] = lineCoords
			}
			coords[i] = polyCoords
		}
		*g = GeometryMultiPolygon{Type: "MultiPolygon", Coordinates: coords}
		return nil
	}
	return scanGeoJSON(v, g)
}
func (g GeometryMultiPolygon) Value() (driver.Value, error) {
	type Alias GeometryMultiPolygon
	return json.Marshal(Alias(g))
}

type GeometryCollection struct {
	Type       string        `json:"type"`
	Geometries []interface{} `json:"geometries"`
}

func NewGeometryCollection(geoms []interface{}) GeometryCollection {
	return GeometryCollection{Type: "GeometryCollection", Geometries: geoms}
}

func (GeometryCollection) GormDataType() string { return "geometry(collection)" }
func (g GeometryCollection) MarshalJSON() ([]byte, error) {
	type Alias GeometryCollection
	return json.Marshal(Alias(g))
}
func (g *GeometryCollection) UnmarshalJSON(d []byte) error {
	type Alias GeometryCollection
	if err := json.Unmarshal(d, (*Alias)(g)); err != nil {
		// Fallback to SDK format: raw array of geometry objects
		var raw []json.RawMessage
		if err := json.Unmarshal(d, &raw); err != nil {
			return err
		}
		geoms := make([]interface{}, len(raw))
		for i, item := range raw {
			// Try Point first (SDK object)
			var p struct {
				Longitude float64 `json:"Longitude"`
				Latitude  float64 `json:"Latitude"`
			}
			if err := json.Unmarshal(item, &p); err == nil {
				geoms[i] = NewPoint(p.Longitude, p.Latitude)
				continue
			}
			// Try array (Line, Polygon, etc.)
			var arr []json.RawMessage
			if err := json.Unmarshal(item, &arr); err == nil {
				geoms[i] = item
			}
		}
		*g = GeometryCollection{Type: "GeometryCollection", Geometries: geoms}
		return nil
	}
	// Post-process: convert tuple arrays inside geometries to GeometryPoint
	for i, geom := range g.Geometries {
		if arr, ok := geom.([]interface{}); ok && len(arr) == 2 {
			if lon, ok1 := arr[0].(float64); ok1 {
				if lat, ok2 := arr[1].(float64); ok2 {
					g.Geometries[i] = NewPoint(lon, lat)
				}
			}
		}
	}
	return nil
}
func (g *GeometryCollection) Scan(v interface{}) error {
	switch val := v.(type) {
	case models.GeometryCollection:
		geoms := make([]interface{}, len(val))
		for i, item := range val {
			switch geom := item.(type) {
			case models.GeometryPoint:
				p := NewPoint(geom.Longitude, geom.Latitude)
				geoms[i] = p
			case models.GeometryLine:
				line := GeometryLine{Type: "LineString"}
				for _, p := range geom {
					line.Coordinates = append(line.Coordinates, []float64{p.Longitude, p.Latitude})
				}
				geoms[i] = line
			case models.GeometryPolygon:
				poly := GeometryPolygon{Type: "Polygon"}
				for _, line := range geom {
					lineCoords := make([][]float64, len(line))
					for j, p := range line {
						lineCoords[j] = []float64{p.Longitude, p.Latitude}
					}
					poly.Coordinates = append(poly.Coordinates, lineCoords)
				}
				geoms[i] = poly
			case models.GeometryMultiPoint:
				mp := GeometryMultiPoint{Type: "MultiPoint"}
				for _, p := range geom {
					mp.Coordinates = append(mp.Coordinates, []float64{p.Longitude, p.Latitude})
				}
				geoms[i] = mp
			case models.GeometryMultiLine:
				ml := GeometryMultiLineString{Type: "MultiLineString"}
				for _, line := range geom {
					lineCoords := make([][]float64, len(line))
					for j, p := range line {
						lineCoords[j] = []float64{p.Longitude, p.Latitude}
					}
					ml.Coordinates = append(ml.Coordinates, lineCoords)
				}
				geoms[i] = ml
			case models.GeometryMultiPolygon:
				mpoly := GeometryMultiPolygon{Type: "MultiPolygon"}
				for _, polygon := range geom {
					polyCoords := make([][][]float64, len(polygon))
					for j, line := range polygon {
						lineCoords := make([][]float64, len(line))
						for k, p := range line {
							lineCoords[k] = []float64{p.Longitude, p.Latitude}
						}
						polyCoords[j] = lineCoords
					}
					mpoly.Coordinates = append(mpoly.Coordinates, polyCoords)
				}
				geoms[i] = mpoly
			default:
				geoms[i] = item
			}
		}
		*g = GeometryCollection{Type: "GeometryCollection", Geometries: geoms}
		return nil
	case *models.GeometryCollection:
		return g.Scan(*val)
	}
	return scanGeoJSON(v, g)
}
func (g GeometryCollection) Value() (driver.Value, error) {
	type Alias GeometryCollection
	return json.Marshal(Alias(g))
}

// scanGeoJSON scans a driver value ([]byte, string, or map) into a geometry struct.
func scanGeoJSON(value interface{}, dst interface{}) error {
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	case map[string]interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		data = b
	default:
		return fmt.Errorf("cannot scan %T into geometry type", value)
	}
	return json.Unmarshal(data, dst)
}

// Bytes - Custom type needed for native storage
type Bytes []byte

func (b Bytes) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte("null"), nil
	}
	ints := make([]int, len(b))
	for i, v := range b {
		ints[i] = int(v)
	}
	return json.Marshal(ints)
}

func (b *Bytes) UnmarshalJSON(data []byte) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case string:
		decoded, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			*b = []byte(val)
		} else {
			*b = decoded
		}
	case []interface{}:
		bytes := make([]byte, len(val))
		for i, item := range val {
			if num, ok := item.(float64); ok {
				bytes[i] = byte(num)
			}
		}
		*b = bytes
	}
	return nil
}

func (Bytes) GormDataType() string {
	return "bytes"
}

func (b *Bytes) Scan(value interface{}) error {
	switch v := value.(type) {
	case []byte:
		*b = Bytes(v)
	case string:
		// SurrealDB may store bytes as base64 string
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			// fallback: treat as raw string
			*b = Bytes(v)
			return nil
		}
		*b = Bytes(decoded)
	case []interface{}:
		bytes := make([]byte, len(v))
		for i, item := range v {
			switch num := item.(type) {
			case float64:
				bytes[i] = byte(num)
			case int64:
				bytes[i] = byte(num)
			case int:
				bytes[i] = byte(num)
			}
		}
		*b = bytes
	default:
		return errors.New("unsupported scan type for Bytes")
	}
	return nil
}

// Value implements driver.Valuer.
func (b Bytes) Value() (driver.Value, error) {
	return []byte(b), nil
}
