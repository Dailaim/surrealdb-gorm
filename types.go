package surrealdb

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/surrealdb/surrealdb.go/pkg/models"
)

type RecordID struct {
	models.RecordID
}

func (r *RecordID) StringToRecordID(s string) error {
	parsed, err := models.ParseRecordID(s)
	if err != nil {
		return err
	}
	r.RecordID = *parsed
	return nil
}

// Value implements driver.Valuer

func (RecordID) GormDataType() string {
	return "record"
}

// Duration wraps time.Duration to marshal as SurrealDB duration string (e.g. "1h30m")
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	case float64:
		d.Duration = time.Duration(value)
		return nil
	default:
		return errors.New("invalid duration format")
	}
}

// Value implements driver.Valuer
func (d Duration) Value() (driver.Value, error) {
	return d.String(), nil
}

// Scan implements sql.Scanner
func (d *Duration) Scan(value interface{}) error {
	switch v := value.(type) {
	case string:
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	default:
		return errors.New("invalid duration db value")
	}
}

func (Duration) GormDataType() string {
	return "string"
}

// Geometry types (manual GeoJSON)

type GeometryPoint struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"`
}

func NewPoint(lon, lat float64) GeometryPoint {
	return GeometryPoint{
		Type:        "Point",
		Coordinates: []float64{lon, lat},
	}
}

func (GeometryPoint) GormDataType() string {
	return "point"
}

type GeometryLine struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

func NewLineString(coords [][]float64) GeometryLine {
	return GeometryLine{
		Type:        "LineString",
		Coordinates: coords,
	}
}

func (GeometryLine) GormDataType() string {
	return "geometry"
}

type GeometryPolygon struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

func NewPolygon(coords [][][]float64) GeometryPolygon {
	return GeometryPolygon{
		Type:        "Polygon",
		Coordinates: coords,
	}
}

func (GeometryPolygon) GormDataType() string {
	return "geometry"
}

type GeometryMultiPoint struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

func NewMultiPoint(coords [][]float64) GeometryMultiPoint {
	return GeometryMultiPoint{
		Type:        "MultiPoint",
		Coordinates: coords,
	}
}

func (GeometryMultiPoint) GormDataType() string {
	return "geometry"
}

type GeometryMultiLineString struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

func NewMultiLineString(coords [][][]float64) GeometryMultiLineString {
	return GeometryMultiLineString{
		Type:        "MultiLineString",
		Coordinates: coords,
	}
}

func (GeometryMultiLineString) GormDataType() string {
	return "geometry"
}

type GeometryMultiPolygon struct {
	Type        string          `json:"type"`
	Coordinates [][][][]float64 `json:"coordinates"`
}

func NewMultiPolygon(coords [][][][]float64) GeometryMultiPolygon {
	return GeometryMultiPolygon{
		Type:        "MultiPolygon",
		Coordinates: coords,
	}
}

func (GeometryMultiPolygon) GormDataType() string {
	return "geometry"
}

type GeometryCollection struct {
	Type       string        `json:"type"`
	Geometries []interface{} `json:"geometries"`
}

func NewGeometryCollection(geoms []interface{}) GeometryCollection {
	return GeometryCollection{
		Type:       "GeometryCollection",
		Geometries: geoms,
	}
}

func (GeometryCollection) GormDataType() string {
	return "geometry"
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
		*b = []byte(val)
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
		*b = Bytes(v)
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

// Link es un tipo inteligente que maneja tanto el ID como el objeto expandido
type Link[T any] struct {
	ID   *RecordID
	Data *T
}

// UnmarshalJSON es la magia: decide si vino un string o un objeto
func (l *Link[T]) UnmarshalJSON(data []byte) error {
	// 1. Caso FETCH: Intentamos desserializar el objeto completo (T)
	// Si el primer caracter es '{', es un objeto
	if len(data) > 0 && data[0] == '{' {
		var obj T
		if err := json.Unmarshal(data, &obj); err == nil {
			l.Data = &obj
			// Aquí podrías extraer el ID del objeto si tu struct T tiene campo ID
			return nil
		}
	}

	// 2. Caso NO FETCH: Intentamos desserializar solo el RecordID
	var id RecordID
	if err := json.Unmarshal(data, &id); err == nil {
		l.ID = &id
		return nil
	}

	return nil
}

func (l *Link[T]) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	// 1. Intentamos convertir a bytes (normalmente Surreal envía JSON como bytes o string)
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("tipo no soportado para Link: %T", value)
	}

	if len(data) == 0 {
		return nil
	}

	// 2. Si empieza con '{', es un objeto (FETCH realizado)
	if data[0] == '{' {
		var obj T
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		l.Data = &obj

		// Opcional: Intentar extraer el ID del objeto si tiene campo "id"
		// Por simplicidad, asumimos que si hay objeto, el ID está dentro de él.
		return nil
	}

	// 3. Si no es un objeto, asumimos que es el string del ID (ej: "book:123")
	// En GORM/Surreal a veces el ID viene con comillas extra, las limpiamos si es necesario
	var idString string
	// Intentamos decodificar como JSON string por si viene con comillas "\"book:1\""
	if err := json.Unmarshal(data, &idString); err == nil {
		l.ID.StringToRecordID(idString)
	} else {
		// Si falla, lo tomamos como string crudo
		l.ID.StringToRecordID(string(data))
	}

	return nil
}

// Value implementa driver.Valuer (Escritura hacia la BD)
// Cuando guardas la Persona, solo quieres guardar el ID del libro, no todo el objeto anidado
func (l Link[T]) Value() (driver.Value, error) {
	if l.ID != nil {
		return l.ID, nil
	}
	// Si tenemos el objeto cargado pero no el ID separado, intentamos obtener el ID del objeto
	// (Esto depende de tu lógica, pero por seguridad devolvemos nil si no hay ID)
	return nil, nil
}

// MarshalJSON (Para tu API):
// Esto hace que cuando hagas json.Marshal(persona), el campo "book"
// no se vea como {"ID": "...", "Data": ...}, sino que devuelva directamente
// el objeto o el string, haciéndolo transparente para el frontend.
func (l Link[T]) MarshalJSON() ([]byte, error) {
	if l.Data != nil {
		return json.Marshal(l.Data) // Devuelve el objeto completo
	}
	if l.ID != nil {
		return json.Marshal(l.ID) // Devuelve solo el string ID
	}
	return json.Marshal(nil)
}

func (l Link[T]) MarshalCBOR() ([]byte, error) {
	if l.ID != nil {
		return l.ID.MarshalCBOR()
	}
	nilCustom := models.CustomNil{}
	return nilCustom.MarshalCBOR()
}
