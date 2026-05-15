package surrealdb

import (
	"fmt"
	"reflect"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
)

// edgeAssocDeleteCallback handles Association("X").Delete(&y) for edge tables.
func edgeAssocDeleteCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	assocMode, _ := db.Statement.Settings.Load("gorm:association:delete")
	if assocMode == nil {
		return
	}
	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema == nil {
		return
	}
	for _, rel := range db.Statement.Schema.Relationships.Many2Many {
		if rel.JoinTable == nil {
			continue
		}
		registeredEdge, ok := dialector.FindEdgeTable(rel.JoinTable.Table)
		if !ok {
			continue
		}
		rv := db.Statement.ReflectValue
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		var inID *sdkModels.RecordID
		for _, ref := range rel.References {
			if ref.OwnPrimaryKey {
				v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, rv)
				if !isZero {
					inID = extractRecordID(v)
				}
				break
			}
		}
		if inID == nil {
			continue
		}

		destVal := reflect.ValueOf(db.Statement.Dest)
		for destVal.Kind() == reflect.Pointer {
			destVal = destVal.Elem()
		}
		var outIDs []*sdkModels.RecordID
		if destVal.Kind() == reflect.Slice {
			for i := 0; i < destVal.Len(); i++ {
				elem := destVal.Index(i)
				for elem.Kind() == reflect.Pointer {
					elem = elem.Elem()
				}
				for _, ref := range rel.References {
					if !ref.OwnPrimaryKey && ref.PrimaryValue == "" {
						v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, elem)
						if !isZero {
							if rid := extractRecordID(v); rid != nil {
								outIDs = append(outIDs, rid)
							}
						}
						break
					}
				}
			}
		} else if destVal.IsValid() {
			for _, ref := range rel.References {
				if !ref.OwnPrimaryKey && ref.PrimaryValue == "" {
					v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, destVal)
					if !isZero {
						if rid := extractRecordID(v); rid != nil {
							outIDs = append(outIDs, rid)
						}
					}
					break
				}
			}
		}
		for _, outID := range outIDs {
			results, err := surrealdb.Query[interface{}](
				db.Statement.Context, dialector.Conn,
				fmt.Sprintf("DELETE %s WHERE in = $in AND out = $out", registeredEdge),
				map[string]interface{}{"in": inID, "out": outID},
			)
			if err != nil {
				db.AddError(err)
				return
			}
			if len(*results) > 0 && (*results)[0].Status != "OK" {
				db.AddError(fmt.Errorf("edge assoc delete error: %v", (*results)[0]))
				return
			}
		}
		db.Statement.SQL.WriteString("-- edge assoc delete handled")
		return
	}
}

// edgeAssocReplaceCallback handles Association("X").Replace(&y) for edge tables.
func edgeAssocReplaceCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	assocMode, _ := db.Statement.Settings.Load("gorm:association:replace")
	if assocMode == nil {
		return
	}
	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema == nil {
		return
	}
	for _, rel := range db.Statement.Schema.Relationships.Many2Many {
		if rel.JoinTable == nil {
			continue
		}
		registeredEdge, ok := dialector.FindEdgeTable(rel.JoinTable.Table)
		if !ok {
			continue
		}
		rv := db.Statement.ReflectValue
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		var inID *sdkModels.RecordID
		for _, ref := range rel.References {
			if ref.OwnPrimaryKey {
				v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, rv)
				if !isZero {
					inID = extractRecordID(v)
				}
				break
			}
		}
		if inID == nil {
			continue
		}

		results, err := surrealdb.Query[interface{}](
			db.Statement.Context, dialector.Conn,
			fmt.Sprintf("DELETE %s WHERE in = $in", registeredEdge),
			map[string]interface{}{"in": inID},
		)
		if err != nil {
			db.AddError(err)
			return
		}
		if len(*results) > 0 && (*results)[0].Status != "OK" {
			db.AddError(fmt.Errorf("edge assoc replace (delete phase) error: %v", (*results)[0]))
			return
		}

		destVal := reflect.ValueOf(db.Statement.Dest)
		for destVal.Kind() == reflect.Pointer {
			destVal = destVal.Elem()
		}
		var newOutIDs []*sdkModels.RecordID
		if destVal.Kind() == reflect.Slice {
			for i := 0; i < destVal.Len(); i++ {
				elem := destVal.Index(i)
				for elem.Kind() == reflect.Pointer {
					elem = elem.Elem()
				}
				for _, ref := range rel.References {
					if !ref.OwnPrimaryKey && ref.PrimaryValue == "" {
						v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, elem)
						if !isZero {
							if rid := extractRecordID(v); rid != nil {
								newOutIDs = append(newOutIDs, rid)
							}
						}
						break
					}
				}
			}
		}
		for _, outID := range newOutIDs {
			rel2 := &surrealdb.Relationship{
				In:       *inID,
				Out:      *outID,
				Relation: sdkModels.Table(registeredEdge),
			}
			if _, err := surrealdb.InsertRelation[interface{}](db.Statement.Context, dialector.Conn, rel2); err != nil {
				db.AddError(err)
				return
			}
		}
		db.Statement.SQL.WriteString("-- edge assoc replace handled")
		return
	}
}

// edgeAssocCountCallback handles Association("X").Count() for edge tables.
func edgeAssocCountCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	_, isAssocCount := db.Statement.Settings.Load("gorm:association:count")
	if !isAssocCount {
		return
	}
	dialector := db.Dialector.(*Dialector)
	if db.Statement.Schema == nil {
		return
	}
	for _, rel := range db.Statement.Schema.Relationships.Many2Many {
		if rel.JoinTable == nil {
			continue
		}
		registeredEdge, ok := dialector.FindEdgeTable(rel.JoinTable.Table)
		if !ok {
			continue
		}
		rv := db.Statement.ReflectValue
		for rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		var inID *sdkModels.RecordID
		for _, ref := range rel.References {
			if ref.OwnPrimaryKey {
				v, isZero := ref.PrimaryKey.ValueOf(db.Statement.Context, rv)
				if !isZero {
					inID = extractRecordID(v)
				}
				break
			}
		}
		if inID == nil {
			return
		}
		type countResult struct {
			Count int64 `json:"count"`
		}
		results, err := surrealdb.Query[[]countResult](
			db.Statement.Context, dialector.Conn,
			fmt.Sprintf("SELECT count() FROM %s WHERE in = $in GROUP ALL", registeredEdge),
			map[string]interface{}{"in": inID},
		)
		if err != nil {
			db.AddError(err)
			return
		}
		if len(*results) > 0 && (*results)[0].Status == "OK" && len((*results)[0].Result) > 0 {
			db.RowsAffected = (*results)[0].Result[0].Count
		}
		db.Statement.SQL.WriteString("-- edge count handled")
		return
	}
}
