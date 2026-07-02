package surrealdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
	"github.com/surrealdb/surrealdb.go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// Dialector implements GORM's dialector interface for SurrealDB.
type Dialector struct {
	gorm.Dialector
	DSN  string
	Conn *surrealdb.DB
	// ReconnectInterval controls the auto-reconnecting WebSocket connection:
	// 0 uses the default (5s), a positive value tunes the check interval, and a
	// negative value disables reconnection (plain connection).
	ReconnectInterval time.Duration
	sqlDB             *sql.DB  // backs QueryContext/QueryRowContext with real *sql.Rows
	edgeTables        sync.Map // map[string]string — canonical edge table names; key = any alias, value = canonical name
}

// RegisterEdgeTable marks a table name as a SurrealDB graph edge table.
// It registers the canonical name as well as its de-pluralized form (without
// trailing "s") so that many2many tags that omit the trailing "s" still resolve
// correctly (e.g. many2many:wishlist → wishlists).
func (d *Dialector) RegisterEdgeTable(table string) {
	d.edgeTables.Store(table, table)
	if strings.HasSuffix(table, "s") {
		d.edgeTables.Store(strings.TrimSuffix(table, "s"), table)
	}
}

// FindEdgeTable returns the canonical registered edge table name that best
// matches the given name. It checks:
//  1. Exact match (the name itself is registered or is an alias)
//  2. Pluralized form (name+"s") as canonical
//
// Returns ("", false) if no match is found.
func (d *Dialector) FindEdgeTable(table string) (string, bool) {
	if canonical, ok := d.edgeTables.Load(table); ok {
		return canonical.(string), true
	}
	plural := table + "s"
	if canonical, ok := d.edgeTables.Load(plural); ok {
		return canonical.(string), true
	}
	return "", false
}

// IsEdgeTable reports whether the given table name is a registered graph edge table.
func (d *Dialector) IsEdgeTable(table string) bool {
	_, ok := d.FindEdgeTable(table)
	return ok
}

func (dialector *Dialector) Name() string {
	return "surrealdb"
}

func (dialector *Dialector) Initialize(db *gorm.DB) (err error) {
	if dialector.Conn != nil {
		db.ConnPool = dialector
	} else {
		u, err := url.Parse(dialector.DSN)
		if err != nil {
			return err
		}

		conn, err := dialector.dialConn(context.Background())
		if err != nil {
			return err
		}

		q := u.Query()
		ns := q.Get("namespace")
		dbName := q.Get("database")
		if ns == "" || dbName == "" {
			return errors.New("namespace and database must be provided")
		}

		user := q.Get("username")
		pass := q.Get("password")
		if user == "" || pass == "" {
			return errors.New("username and password must be provided")
		}
		// Sign in first: root-level auth is namespace-independent and is required
		// before defining namespaces/databases.
		if _, err := conn.SignIn(context.Background(), surrealdb.Auth{
			Username: user,
			Password: pass,
		}); err != nil {
			return err
		}

		// SurrealDB v3 does not implicitly create the namespace/database on USE
		// (v2 tolerated it). Create them idempotently so AutoMigrate and queries
		// work out of the box on both versions.
		ctx := context.Background()
		if _, err := surrealdb.Query[interface{}](ctx, conn,
			fmt.Sprintf("DEFINE NAMESPACE IF NOT EXISTS `%s`", ns), nil); err != nil {
			return err
		}
		if err := conn.Use(ctx, ns, dbName); err != nil {
			return err
		}
		if _, err := surrealdb.Query[interface{}](ctx, conn,
			fmt.Sprintf("DEFINE DATABASE IF NOT EXISTS `%s`", dbName), nil); err != nil {
			return err
		}

		dialector.Conn = conn
		db.ConnPool = dialector
	}

	// Open the internal *sql.DB that backs the raw-row query paths. It reuses the
	// already-established SurrealDB connection via a database/sql connector.
	if dialector.sqlDB == nil && dialector.Conn != nil {
		dialector.sqlDB = sql.OpenDB(&sdConnector{dialector: dialector})
	}

	// Disable GORM's implicit per-statement transaction wrapping.
	db.Config.SkipDefaultTransaction = true

	RegisterCallbacks(db)
	return nil
}

func (dialector *Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return Migrator{
		Migrator: migrator.Migrator{
			Config: migrator.Config{
				DB:                          db,
				Dialector:                   dialector,
				CreateIndexAfterCreateTable: true,
			},
		},
	}
}

func (dialector *Dialector) DataTypeOf(field *schema.Field) string {
	// 1. Explicit gorm tag overrides everything: gorm:"type:record<person>"
	if customType, ok := field.TagSettings["TYPE"]; ok && customType != "" {
		return customType
	}

	switch field.DataType {
	case schema.Bool:
		return "bool"
	case schema.Int, schema.Uint:
		return "int"
	case schema.Float:
		return "float"
	case schema.String:
		return "string"
	case schema.Time:
		return "datetime"
	case schema.Bytes:
		return "bytes"
	}

	recordIDType := reflect.TypeOf(TypesM.RecordID{})

	// Single RecordID / *RecordID
	if field.FieldType == recordIDType || field.FieldType == reflect.PtrTo(recordIDType) {
		if refTable := inferRecordTable(field); refTable != "" {
			return fmt.Sprintf("record<%s>", refTable)
		}
		return "record"
	}

	// Single Link[T] / *Link[T]
	if isLinkType(field.FieldType) {
		if refTable := inferRecordTable(field); refTable != "" {
			return fmt.Sprintf("record<%s>", refTable)
		}
		return "record"
	}

	// If GORM classified this as a generic array (from SliceLink[T] etc.),
	// try to specialise it for slices of RecordID / Link[T].
	if field.DataType == "array" || field.DataType == "array<record>" {
		kind := field.FieldType.Kind()
		ft := field.FieldType
		if kind == reflect.Ptr {
			ft = ft.Elem()
			kind = ft.Kind()
		}
		if kind == reflect.Slice || kind == reflect.Array {
			elem := ft.Elem()
			if elem.Kind() == reflect.Ptr {
				elem = elem.Elem()
			}
			if elem == recordIDType {
				if refTable := inferRecordTable(field); refTable != "" {
					return fmt.Sprintf("array<record<%s>>", refTable)
				}
				return "array<record>"
			}
			if isLinkType(elem) {
				if refTable := inferRecordTable(field); refTable != "" {
					return fmt.Sprintf("array<record<%s>>", refTable)
				}
				return "array<record>"
			}
		}
		return "array"
	}

	// If the field has a custom GormDataType (e.g. geometry(point), decimal, etc.)
	// field.DataType will hold that string value.
	if field.DataType != "" {
		return string(field.DataType)
	}

	// Fallback for slices / arrays without a custom GormDataType.
	kind := field.FieldType.Kind()
	ft := field.FieldType
	if kind == reflect.Ptr {
		ft = ft.Elem()
		kind = ft.Kind()
	}
	if kind == reflect.Slice || kind == reflect.Array {
		elem := ft.Elem()
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		if elem == recordIDType {
			if refTable := inferRecordTable(field); refTable != "" {
				return fmt.Sprintf("array<record<%s>>", refTable)
			}
			return "array<record>"
		}
		if isLinkType(elem) {
			if refTable := inferRecordTable(field); refTable != "" {
				return fmt.Sprintf("array<record<%s>>", refTable)
			}
			return "array<record>"
		}
		return "array"
	}
	if kind == reflect.Map || kind == reflect.Struct {
		return "object"
	}
	return "string"
}

// isLinkType reports whether t is types.Link[T] (or a pointer to it).
func isLinkType(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t.Name() == "Link" && t.PkgPath() == "github.com/dailaim/surrealdb-gorm/types"
}

// inferRecordTable tries to discover the target table for a RecordID / Link[T]
// field by inspecting GORM relationships (BelongsTo, HasOne, HasMany, Many2Many).
// If no relationship is found, it falls back to the field-name convention
// (e.g. AuthorID → Author) or to the generic type T extracted from Link[T] / SliceLink[T].
func inferRecordTable(field *schema.Field) string {
	if field.Schema == nil {
		return ""
	}

	allRels := make([]*schema.Relationship, 0)
	for _, rel := range field.Schema.Relationships.Relations {
		allRels = append(allRels, rel)
	}
	for _, rel := range field.Schema.Relationships.BelongsTo {
		allRels = append(allRels, rel)
	}
	for _, rel := range field.Schema.Relationships.HasOne {
		allRels = append(allRels, rel)
	}
	for _, rel := range field.Schema.Relationships.HasMany {
		allRels = append(allRels, rel)
	}
	for _, rel := range field.Schema.Relationships.Many2Many {
		allRels = append(allRels, rel)
	}

	for _, rel := range allRels {
		if rel.Field == field && rel.FieldSchema != nil {
			return rel.FieldSchema.Table
		}
		for _, ref := range rel.References {
			if ref.ForeignKey == field && rel.FieldSchema != nil {
				return rel.FieldSchema.Table
			}
		}
	}

	// Fallback 1: field-name convention (AuthorID → Author → relation named Author)
	name := field.Name
	if strings.HasSuffix(name, "ID") {
		name = strings.TrimSuffix(name, "ID")
	} else if strings.HasSuffix(name, "Id") {
		name = strings.TrimSuffix(name, "Id")
	}
	if rel, ok := field.Schema.Relationships.Relations[name]; ok && rel.FieldSchema != nil {
		return rel.FieldSchema.Table
	}

	// Fallback 2: extract generic type T from Link[T] / SliceLink[T] and pluralise it.
	if gt := extractGenericType(field.FieldType); gt != nil {
		ns := schema.NamingStrategy{}
		return ns.TableName(gt.Name())
	}

	return ""
}

// extractGenericType walks through a Link[T] or SliceLink[T] type and returns
// the concrete type T (the generic argument). It returns nil if the type is not
// a Link or SliceLink.
func extractGenericType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	// Must be types.Link[T]
	if t.Name() != "Link" || t.PkgPath() != "github.com/dailaim/surrealdb-gorm/types" {
		return nil
	}
	// Find the "Data" field (type *T) inside Link[T]
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Name == "Data" && f.Type.Kind() == reflect.Ptr && f.Type.Elem().Kind() == reflect.Struct {
			return f.Type.Elem()
		}
	}
	return nil
}

func (dialector *Dialector) DefaultValueOf(field *schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}

func (dialector *Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
	writer.WriteString(fmt.Sprintf("$p%d", len(stmt.Vars)))
}

func (dialector *Dialector) QuoteTo(writer clause.Writer, str string) {
	writer.WriteByte('`')
	writer.WriteString(str)
	writer.WriteByte('`')
}

func (dialector *Dialector) Explain(sql string, vars ...interface{}) string {
	return sql
}

// ExplainQuery analyzes a SurrealQL query and returns its execution plan.
func (d *Dialector) ExplainQuery(ctx context.Context, query string, vars ...interface{}) (*ExplainResult, error) {
	if d.Conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}
	params := make(map[string]interface{})
	for i, v := range vars {
		params[fmt.Sprintf("p%d", i+1)] = TypesM.ToSDKValue(v)
	}
	results, err := surrealdb.Query[ExplainResult](ctx, d.Conn, fmt.Sprintf("EXPLAIN %s", query), params)
	if err != nil {
		return nil, err
	}
	if len(*results) == 0 {
		return nil, fmt.Errorf("no explain result")
	}
	return &(*results)[0].Result, nil
}

// ExplainResult holds the output of an EXPLAIN query.
type ExplainResult struct {
	Detail  string `json:"detail"`
	Plan    string `json:"plan"`
	Reasons string `json:"reasons,omitempty"`
}

// ConnPool stub implementations (real ones live in driver.go)
func (dialector *Dialector) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return nil, errors.New("method PrepareContext not implemented")
}

// QueryContext returns real *sql.Rows by delegating to the internal *sql.DB,
// which is backed by the SurrealDB database/sql driver (see sqldriver.go).
// This powers db.Raw(sql).Rows(), db.ScanRows, and raw LIVE/SHOW CHANGES reads.
func (dialector *Dialector) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if dialector.sqlDB == nil {
		return nil, errors.New("surrealdb: connection not initialized")
	}
	return dialector.sqlDB.QueryContext(ctx, query, args...)
}

// QueryRowContext returns a real *sql.Row by delegating to the internal *sql.DB.
func (dialector *Dialector) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if dialector.sqlDB == nil {
		return nil
	}
	return dialector.sqlDB.QueryRowContext(ctx, query, args...)
}

// BeginTx implements gorm.ConnPoolBeginner. It opens a native interactive
// transaction on the SurrealDB WebSocket connection (requires SurrealDB v3+).
// All subsequent operations on the returned ConnPool run inside that transaction,
// so read-your-own-writes works transparently.
func (dialector *Dialector) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	if dialector.Conn == nil {
		return nil, errors.New("surrealdb: connection not initialized")
	}
	sdkTx, err := dialector.Conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("surrealdb BeginTx: %w", err)
	}
	return &SurrealTx{
		dialector: dialector,
		ctx:       ctx,
		sdkTx:     sdkTx,
	}, nil
}
