package surrealdb

import (
	"context"
	"fmt"
	"reflect"

	localModels "github.com/dailaim/surrealdb-gorm/models"
	"github.com/surrealdb/surrealdb.go"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
)

type Migrator struct {
	migrator.Migrator
}

func (m Migrator) AutoMigrate(dst ...interface{}) error {
	// Fetch existing tables once
	existingTables, err := m.getExistingTables()
	if err != nil {
		return err
	}

	edgeRelType := reflect.TypeOf((*localModels.EdgeRelation)(nil)).Elem()

	for _, value := range dst {
		var tableName string
		var modelType reflect.Type
		m.RunWithValue(value, func(stmt *gorm.Statement) error {
			tableName = stmt.Table
			if stmt.Schema != nil {
				modelType = stmt.Schema.ModelType
			}
			return nil
		})

		// Always register edge tables in the in-memory registry so that
		// callbacks can detect them even when the DB table already exists
		// from a previous run.
		if modelType != nil {
			if modelType.Implements(edgeRelType) || reflect.PointerTo(modelType).Implements(edgeRelType) {
				if d, ok := m.DB.Dialector.(*Dialector); ok {
					d.RegisterEdgeTable(tableName)
				}
			}
		}

		if _, exists := existingTables[tableName]; !exists {
			if err := m.CreateTable(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m Migrator) CurrentDatabase() (name string) {
	return "surrealdb"
}

func (m Migrator) HasTable(value interface{}) bool {
	var tableName string
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		tableName = stmt.Table
		return nil
	})

	if tableName == "" {
		return false
	}

	existingTables, err := m.getExistingTables()
	if err != nil {
		return false
	}

	_, exists := existingTables[tableName]
	return exists
}

func (m Migrator) getExistingTables() (map[string]string, error) {
	dialector, ok := m.DB.Dialector.(*Dialector)
	if !ok || dialector.Conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}

	type InfoForDB struct {
		Tables map[string]string `json:"tables"`
	}

	// Execute directly
	results, err := surrealdb.Query[InfoForDB](context.Background(), dialector.Conn, "INFO FOR DB", nil)
	if err != nil {
		return nil, err
	}

	if len(*results) > 0 {
		return (*results)[0].Result.Tables, nil
	}

	return map[string]string{}, nil
}

func (m Migrator) CreateTable(models ...interface{}) error {
	for _, model := range models {
		if err := m.RunWithValue(model, func(stmt *gorm.Statement) error {
			tableName := stmt.Schema.Table

			// Detect if the model is a SurrealDB graph edge (embeds Edge[T,U]).
			// We check both the value and pointer type.
			isEdge := false
			edgeRelType := reflect.TypeOf((*localModels.EdgeRelation)(nil)).Elem()
			mt := stmt.Schema.ModelType
			if mt.Implements(edgeRelType) || reflect.PointerTo(mt).Implements(edgeRelType) {
				isEdge = true
			}

			var defineSQL string
			if isEdge {
				// SurrealDB TYPE RELATION creates a proper graph edge table.
				defineSQL = fmt.Sprintf("DEFINE TABLE %s TYPE RELATION SCHEMALESS", tableName)
			} else {
				defineSQL = fmt.Sprintf("DEFINE TABLE %s SCHEMALESS", tableName)
			}

			if err := m.DB.Exec(defineSQL).Error; err != nil {
				return err
			}

			// Register edge table in the dialector registry so callbacks can
			// route association inserts to InsertRelation.
			if isEdge {
				if d, ok := m.DB.Dialector.(*Dialector); ok {
					d.RegisterEdgeTable(tableName)
				}
			}

			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
