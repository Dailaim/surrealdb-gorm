package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/surrealdb/surrealdb.go/pkg/models"
)

type Identifiable interface {
	GetID() *RecordID
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
		// Use SurrealMapToStruct to respect GORM tags
		if err := SurrealMapToStruct(&obj, data); err == nil {
			l.Data = &obj
			// Aquí podrías extraer el ID del objeto si tu struct T tiene campo ID
			if getter, ok := any(&obj).(Identifiable); ok {
				l.ID = getter.GetID()
			}
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
		// Handle map/slice directly if driver returns it?
		// Fallback to json marshal
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("tipo no soportado para Link: %T", value)
		}
		data = b
	}

	if len(data) == 0 {
		return nil
	}

	// 2. Si empieza con '{', es un objeto (FETCH realizado)
	if len(data) > 0 && data[0] == '{' {
		var obj T
		// Usamos SurrealMapToStruct para respetar tags de GORM (column:...)
		if err := SurrealMapToStruct(&obj, data); err != nil {
			return err
		}
		l.Data = &obj

		if getter, ok := any(&obj).(Identifiable); ok {
			l.ID = getter.GetID()
		}

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
	// Try to get ID from Data
	if l.Data != nil {
		val := reflect.ValueOf(l.Data)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		if val.Kind() == reflect.Struct {
			// Naive check for ID field?
			// Best to rely on interface
			if getter, ok := any(l.Data).(Identifiable); ok {
				return getter.GetID(), nil
			}
		}
	}

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
		return json.Marshal(l.ID.String()) // Devuelve solo el string ID
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
