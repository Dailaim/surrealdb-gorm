package surrealdb

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	localModels "github.com/dailaim/surrealdb-gorm/models"
	"github.com/surrealdb/surrealdb.go"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

type Migrator struct {
	migrator.Migrator
}

// ctx returns the caller's context when one is bound to the migrator's DB,
// falling back to context.Background(). This propagates deadlines/cancellation
// into the INFO FOR ... introspection queries the migrator runs directly.
func (m Migrator) ctx() context.Context {
	if m.DB != nil && m.DB.Statement != nil && m.DB.Statement.Context != nil {
		return m.DB.Statement.Context
	}
	return context.Background()
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

		isEdge := false
		if modelType != nil {
			if modelType.Implements(edgeRelType) || reflect.PointerTo(modelType).Implements(edgeRelType) {
				isEdge = true
			}
		}

		isNewTable := true
		if _, exists := existingTables[tableName]; exists {
			isNewTable = false
		}

		if isNewTable {
			if err := m.CreateTable(value); err != nil {
				return err
			}
		}

		// Define fields and indexes for all tables.
		// Edge tables skip in/out/id/meta fields because SurrealDB manages them.
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			if err := m.defineFields(stmt, isEdge, isNewTable); err != nil {
				return err
			}
			if err := m.defineIndexes(stmt); err != nil {
				return err
			}
			// For existing tables, clean up fields that no longer exist in the model.
			if !isNewTable {
				return m.removeObsoleteFields(stmt, isEdge)
			}
			return nil
		}); err != nil {
			return err
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

	results, err := surrealdb.Query[InfoForDB](m.ctx(), dialector.Conn, "INFO FOR DB", nil)
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

			isEdge := false
			edgeRelType := reflect.TypeOf((*localModels.EdgeRelation)(nil)).Elem()
			mt := stmt.Schema.ModelType
			if mt.Implements(edgeRelType) || reflect.PointerTo(mt).Implements(edgeRelType) {
				isEdge = true
			}

			schemaClause := "SCHEMALESS"
			// Check if the model opts-in to SCHEMAFULL via the SchemaFull interface.
			schemaFullType := reflect.TypeOf((*localModels.SchemaFull)(nil)).Elem()
			if mt.Implements(schemaFullType) || reflect.PointerTo(mt).Implements(schemaFullType) {
				schemaClause = "SCHEMAFULL"
			}

			var defineSQL string
			if isEdge {
				inTable := inferEdgeEndpointTable(mt, "in")
				outTable := inferEdgeEndpointTable(mt, "out")
				if inTable != "" && outTable != "" {
					defineSQL = fmt.Sprintf("DEFINE TABLE %s TYPE RELATION FROM %s TO %s %s", tableName, inTable, outTable, schemaClause)
				} else {
					defineSQL = fmt.Sprintf("DEFINE TABLE %s TYPE RELATION %s", tableName, schemaClause)
				}
			} else {
				defineSQL = fmt.Sprintf("DEFINE TABLE %s %s", tableName, schemaClause)
			}

			if err := m.DB.Exec(defineSQL).Error; err != nil {
				return err
			}

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

// defineFields generates DEFINE FIELD statements for every schema field.
// For existing tables it uses OVERWRITE so that type changes are applied
// without needing to DROP the field first.
// Edge tables skip in/out/id and GORM meta-fields.
func (m Migrator) defineFields(stmt *gorm.Statement, isEdge bool, isNewTable bool) error {
	dialector, ok := m.DB.Dialector.(*Dialector)
	if !ok {
		return fmt.Errorf("dialector not available")
	}

	tableName := stmt.Schema.Table
	clause := "IF NOT EXISTS"
	if !isNewTable {
		clause = "OVERWRITE"
	}

	for _, field := range stmt.Schema.Fields {
		dbName := field.DBName
		if dbName == "" {
			continue
		}

		// id is always handled by SurrealDB.
		if dbName == "id" {
			continue
		}

		// Edge tables: SurrealDB handles in/out automatically via
		// TYPE RELATION FROM ... TO ... declared in CreateTable.
		if isEdge && (dbName == "in" || dbName == "out" ||
			dbName == "created_at" || dbName == "updated_at" || dbName == "deleted_at") {
			continue
		}

		dataType := dialector.DataTypeOf(field)
		if dataType == "" {
			continue
		}

		// Normalise geometry sub-types to plain "geometry" because SurrealDB
		// does not accept TYPE geometry(point) etc.
		if strings.HasPrefix(dataType, "geometry(") {
			dataType = "geometry"
		}

		// Build the TYPE expression.
		// Only NOT NULL fields get the strict type; everything else is option<datatype>
		// so that SurrealDB accepts NONE for absent / zero-value fields.
		var typeExpr string
		if field.NotNull {
			typeExpr = dataType
		} else {
			typeExpr = fmt.Sprintf("option<%s>", dataType)
		}

		parts := []string{
			fmt.Sprintf("DEFINE FIELD %s `%s` ON `%s` TYPE %s", clause, dbName, tableName, typeExpr),
		}

		// ASSERT for NOT NULL fields.
		if field.NotNull {
			parts = append(parts, "ASSERT $value != NONE AND $value != NULL")
		}

		// REFERENCE for typed record fields (record<T> or array<record<T>>).
		if strings.Contains(dataType, "record<") {
			if field.NotNull {
				parts = append(parts, "REFERENCE ON DELETE REJECT")
			} else {
				parts = append(parts, "REFERENCE ON DELETE UNSET")
			}
		}

		// READONLY for fields explicitly tagged with gorm:"readonly".
		if _, ok := field.TagSettings["READONLY"]; ok {
			parts = append(parts, "READONLY")
		}

		// DEFAULT clause.
		if field.HasDefaultValue && field.DefaultValue != "" {
			defaultVal := strings.ReplaceAll(field.DefaultValue, "'", "\\'")
			parts = append(parts, fmt.Sprintf("DEFAULT '%s'", defaultVal))
		}

		sql := strings.Join(parts, " ")
		if err := m.DB.Exec(sql).Error; err != nil {
			return fmt.Errorf("define field %s on %s: %w", dbName, tableName, err)
		}
	}

	return nil
}

// removeObsoleteFields drops fields that exist in the database but are no longer
// present in the current GORM schema. It queries INFO FOR TABLE to discover
// the existing field definitions.
func (m Migrator) removeObsoleteFields(stmt *gorm.Statement, isEdge bool) error {
	dialector, ok := m.DB.Dialector.(*Dialector)
	if !ok || dialector.Conn == nil {
		return nil
	}

	tableName := stmt.Schema.Table

	// Query current table info to discover existing fields.
	type InfoForTable struct {
		Fields map[string]string `json:"fields"`
	}
	res, err := surrealdb.Query[InfoForTable](m.ctx(), dialector.Conn,
		fmt.Sprintf("INFO FOR TABLE `%s`", tableName), nil)
	if err != nil || res == nil || len(*res) == 0 {
		return nil // best-effort cleanup
	}

	fieldsMap := (*res)[0].Result.Fields
	if len(fieldsMap) == 0 {
		return nil
	}

	// Build a set of desired field names (DBName).
	desired := make(map[string]bool)
	for _, field := range stmt.Schema.Fields {
		if field.DBName != "" {
			desired[field.DBName] = true
		}
	}

	// SurrealDB always manages these automatically.
	desired["id"] = true
	if isEdge {
		desired["in"] = true
		desired["out"] = true
		desired["created_at"] = true
		desired["updated_at"] = true
		desired["deleted_at"] = true
	}

	for fieldName := range fieldsMap {
		if desired[fieldName] {
			continue
		}
		sql := fmt.Sprintf("REMOVE FIELD IF EXISTS `%s` ON `%s`", fieldName, tableName)
		_ = m.DB.Exec(sql).Error // best-effort
	}

	return nil
}

// defineIndexes generates DEFINE INDEX statements from schema indexes.
func (m Migrator) defineIndexes(stmt *gorm.Statement) error {
	tableName := stmt.Schema.Table

	indexes := stmt.Schema.ParseIndexes()

	for _, idx := range indexes {
		if idx == nil || idx.Name == "" {
			continue
		}

		// SurrealDB automatically indexes the id field; skip primary-key indexes.
		if isPrimaryKeyIndex(idx) {
			continue
		}

		var fieldNames []string
		for _, opt := range idx.Fields {
			if opt.DBName != "" {
				fieldNames = append(fieldNames, opt.DBName)
			}
		}
		if len(fieldNames) == 0 {
			continue
		}
		fields := strings.Join(fieldNames, ", ")

		var sql string
		switch idx.Class {
		case "UNIQUE":
			sql = fmt.Sprintf("DEFINE INDEX IF NOT EXISTS `%s` ON `%s` FIELDS %s UNIQUE", idx.Name, tableName, fields)
		case "FULLTEXT":
			// SurrealDB fulltext index requires a SEARCH ANALYZER clause.
			// The analyzer name can be passed via the index option string: "analyzer:myAnalyzer".
			analyzer := "default"
			if strings.Contains(idx.Option, "analyzer:") {
				parts := strings.Split(idx.Option, "analyzer:")
				if len(parts) > 1 {
					analyzer = strings.TrimSpace(parts[1])
				}
			}
			sql = fmt.Sprintf("DEFINE INDEX IF NOT EXISTS `%s` ON `%s` FIELDS %s SEARCH ANALYZER %s", idx.Name, tableName, fields, analyzer)
		default:
			sql = fmt.Sprintf("DEFINE INDEX IF NOT EXISTS `%s` ON `%s` FIELDS %s", idx.Name, tableName, fields)
		}

		if err := m.DB.Exec(sql).Error; err != nil {
			return fmt.Errorf("define index %s on %s: %w", idx.Name, tableName, err)
		}
	}

	return nil
}

// isPrimaryKeyIndex reports whether the index covers only the primary-key field(s).
func isPrimaryKeyIndex(idx *schema.Index) bool {
	if len(idx.Fields) != 1 {
		return false
	}
	return strings.ToLower(idx.Fields[0].DBName) == "id"
}

// inferEdgeEndpointTable asks the edge model itself for its endpoint table names.
// It relies on Edge[T, U].InTableName() / OutTableName() (via the embedded Edge).
func inferEdgeEndpointTable(modelType reflect.Type, endpoint string) string {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return ""
	}

	// Create a zero value so we can call the methods inherited from Edge[T, U].
	val := reflect.New(modelType).Elem()
	if !val.CanAddr() {
		return ""
	}

	methodName := "InTableName"
	if endpoint == "out" {
		methodName = "OutTableName"
	}

	m := val.Addr().MethodByName(methodName)
	if !m.IsValid() {
		return ""
	}
	out := m.Call(nil)
	if len(out) == 1 && out[0].Kind() == reflect.String {
		return out[0].String()
	}
	return ""
}
