package models

import (
	"reflect"

	"gorm.io/gorm/schema"

	"github.com/dailaim/surrealdb-gorm/types"
)

type EdgeIdentifiable[T any, U any] interface {
	ConnectionOut() *types.Link[T]
	ConnectionIn() *types.Link[U]
}

// EdgeRelation is a non-generic interface that edge models implement,
// allowing CreateCallback to detect and route through InsertRelation.
type EdgeRelation interface {
	EdgeIn() *types.RecordID
	EdgeOut() *types.RecordID
}

// Edge is the base embedded type for SurrealDB graph edge models.
// Embed it in your own struct together with BaseModel if you need IDs / timestamps.
//
// # Extra fields and Association.Append
//
// GORM's standard Association.Append API does not provide a way to pass
// additional data to the join table. If you need an edge with custom fields
// (e.g. Name, Year), use db.Create(&MyEdge{Edge: ..., Name: "x"}) directly.
// Association.Append will create the edge but leave extra fields at their zero
// value. This is a limitation of the GORM association API and cannot be worked
// around without a custom helper.
type Edge[T any, U any] struct {
	ID  *types.RecordID `gorm:"primaryKey;type:record;<-:create" json:"id,omitempty"`
	In  *types.Link[T]  `gorm:"column:in" json:"in,omitempty"`
	Out *types.Link[U]  `gorm:"column:out" json:"out,omitempty"`
}

func (e *Edge[T, U]) GetID() *types.RecordID {
	return e.ID
}

// InTableName returns the GORM table name for the generic type T (the "in" endpoint).
func (Edge[T, U]) InTableName() string {
	return tableNameForType[T]()
}

// OutTableName returns the GORM table name for the generic type U (the "out" endpoint).
func (Edge[T, U]) OutTableName() string {
	return tableNameForType[U]()
}

// tableNameForType derives a SurrealDB table name from a generic type argument.
// It checks for a TableName() method first, then falls back to GORM naming strategy.
func tableNameForType[T any]() string {
	var zero T
	v := reflect.ValueOf(&zero).Elem()

	// Try pointer receiver: (*T).TableName()
	if v.CanAddr() {
		if m := v.Addr().MethodByName("TableName"); m.IsValid() {
			out := m.Call(nil)
			if len(out) == 1 && out[0].Kind() == reflect.String {
				return out[0].String()
			}
		}
	}

	// Try value receiver: (T).TableName()
	if m := v.MethodByName("TableName"); m.IsValid() {
		out := m.Call(nil)
		if len(out) == 1 && out[0].Kind() == reflect.String {
			return out[0].String()
		}
	}

	// Fallback: GORM naming strategy on the type name.
	rt := v.Type()
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if rt.Kind() == reflect.Struct {
		ns := schema.NamingStrategy{}
		return ns.TableName(rt.Name())
	}
	return ""
}

func (s Edge[T, U]) ConectionOut() *types.Link[U] {
	return s.Out
}

func (s Edge[T, U]) ConectionIn() *types.Link[T] {
	return s.In
}

// EdgeIn returns the RecordID of the "in" side of the edge, implementing EdgeRelation.
func (s Edge[T, U]) EdgeIn() *types.RecordID {
	if s.In != nil {
		return s.In.ID
	}
	return nil
}

// EdgeOut returns the RecordID of the "out" side of the edge, implementing EdgeRelation.
func (s Edge[T, U]) EdgeOut() *types.RecordID {
	if s.Out != nil {
		return s.Out.ID
	}
	return nil
}

// NewEdge creates a bare Edge with In/Out links ready to use.
//
//	edge := models.NewEdge[Store, Product](store.ID, product.ID)
func NewEdge[T any, U any](inID, outID *types.RecordID) Edge[T, U] {
	return Edge[T, U]{
		In:  &types.Link[T]{ID: inID},
		Out: &types.Link[U]{ID: outID},
	}
}

// SetIn replaces the In side of the edge.
func (e *Edge[T, U]) SetIn(id *types.RecordID) {
	e.In = &types.Link[T]{ID: id}
}

// SetOut replaces the Out side of the edge.
func (e *Edge[T, U]) SetOut(id *types.RecordID) {
	e.Out = &types.Link[U]{ID: id}
}
