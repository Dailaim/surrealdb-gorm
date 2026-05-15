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

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// Dialector implements GORM's dialector interface for SurrealDB.
type Dialector struct {
	gorm.Dialector
	DSN        string
	Conn       *surrealdb.DB
	edgeTables sync.Map // map[string]string — canonical edge table names; key = any alias, value = canonical name
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

		conn, err := surrealdb.FromEndpointURLString(context.Background(), dialector.DSN)
		if err != nil {
			return err
		}

		q := u.Query()
		ns := q.Get("namespace")
		dbName := q.Get("database")
		if ns == "" || dbName == "" {
			return errors.New("namespace and database must be provided")
		}
		if err := conn.Use(context.Background(), ns, dbName); err != nil {
			return err
		}

		user := q.Get("username")
		pass := q.Get("password")
		if user == "" || pass == "" {
			return errors.New("username and password must be provided")
		}
		if _, err := conn.SignIn(context.Background(), surrealdb.Auth{
			Username: user,
			Password: pass,
		}); err != nil {
			return err
		}

		dialector.Conn = conn
		db.ConnPool = dialector
	}

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

	if field.FieldType == reflect.TypeOf(&sdkModels.RecordID{}) {
		return "record"
	}

	kind := field.FieldType.Kind()
	if kind == reflect.Ptr {
		kind = field.FieldType.Elem().Kind()
	}
	if kind == reflect.Slice || kind == reflect.Array {
		return "array"
	}
	if kind == reflect.Map || kind == reflect.Struct {
		return "object"
	}
	return "string"
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

// ConnPool stub implementations (real ones live in driver.go)
func (dialector *Dialector) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return nil, errors.New("method PrepareContext not implemented")
}

func (dialector *Dialector) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("method QueryContext not implemented")
}

func (dialector *Dialector) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return nil
}
