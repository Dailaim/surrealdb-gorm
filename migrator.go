package surrealdb

import (
	"context"
	"fmt"

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

	for _, value := range dst {
		var tableName string
		m.RunWithValue(value, func(stmt *gorm.Statement) error {
			tableName = stmt.Table
			return nil
		})

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
			// Prepare DEFINE TABLE statement
			// SurrealDB uses DEFINE TABLE, not CREATE TABLE.
			// Using SCHEMALESS mode for flexibility as GORM AutoMigrate is dynamic.
			tableName := stmt.Schema.Table
			// sql := fmt.Sprintf("DEFINE TABLE %s SCHEMALESS", tableName)

			// We should just define the table as SCHEMALESS first
			if err := m.DB.Exec(fmt.Sprintf("DEFINE TABLE %s SCHEMALESS", tableName)).Error; err != nil {
				return err
			}

			// For now, we skip detailed Field definitions because GORM's AutoMigrate
			// iterates all fields and tries to call DataTypeOf.
			// If DataTypeOf returns "array" or "object", fine.
			// If "unsupported", we error.

			// Actually, we can just return, effectively doing "DEFINE TABLE table SCHEMALESS"
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
