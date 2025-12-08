package surrealdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"

	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

type Dialector struct {
	gorm.Dialector
	DSN  string
	Conn *surrealdb.DB
}

func Open(dsn string) gorm.Dialector {
	return &Dialector{DSN: dsn}
}

func (dialector *Dialector) Name() string {
	return "surrealdb"
}

func (dialector *Dialector) Initialize(db *gorm.DB) (err error) {
	if dialector.Conn != nil {
		db.ConnPool = dialector
	} else {
		// Parse DSN and connect
		// Format: ws://user:pass@localhost:8000 or with params
		// Check for query params for namespace/database
		u, err := url.Parse(dialector.DSN)
		if err != nil {
			return err
		}

		// Connect
		var conn *surrealdb.DB
		conn, err = surrealdb.FromEndpointURLString(context.Background(), dialector.DSN)
		if err != nil {
			return err
		}

		// Extract NS and DB from query params
		q := u.Query()
		ns := q.Get("namespace")
		dbName := q.Get("database")

		if ns == "" || dbName == "" {
			return errors.New("namespace and database must be provided")
		}

		if err := conn.Use(context.Background(), ns, dbName); err != nil {
			return err
		}

		// Login
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

	if field.FieldType == reflect.TypeOf(&models.RecordID{}) {
		return "record"
	}

	// Handle Slices (Array) and Maps (Object)
	kind := field.FieldType.Kind()
	if kind == reflect.Ptr {
		kind = field.FieldType.Elem().Kind()
	}

	if kind == reflect.Slice || kind == reflect.Array {
		// Could be specific array type like array<string>, but generic "array" is safer for schema-less mostly
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

// ConnPool implementations
func (dialector *Dialector) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return nil, errors.New("method PrepareContext not implemented")
}

func (dialector *Dialector) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	// Panic removed. Logging raw SQL.
	fmt.Fprintf(os.Stderr, "DEBUG RAW SQL: %s\n", query)

	// Sanitize UPDATE SQL (Remove ID from SET)
	if len(query) > 6 && (strings.HasPrefix(strings.ToUpper(query), "UPDATE")) {
		lowerSql := strings.ToLower(query)
		setIdx := strings.Index(lowerSql, " set ")
		whereIdx := strings.LastIndex(lowerSql, " where ")

		if setIdx != -1 && (whereIdx == -1 || setIdx < whereIdx) {
			beforeSet := query[:setIdx+5]
			var setPart, afterSet string
			if whereIdx != -1 {
				setPart = query[setIdx+5 : whereIdx]
				afterSet = query[whereIdx:]
			} else {
				setPart = query[setIdx+5:]
			}

			// Remove id assignments
			parts := strings.Split(setPart, ",")
			var cleanParts []string
			for _, p := range parts {
				trimmed := strings.TrimSpace(p)
				// Check if targets atomic ID (backticks or double quotes or none)
				if strings.HasPrefix(trimmed, "`id` =") || strings.HasPrefix(trimmed, "id =") || strings.HasPrefix(trimmed, "\"id\" =") {
					continue
				}
				cleanParts = append(cleanParts, p)
			}
			newSetPart := strings.Join(cleanParts, ", ")
			query = beforeSet + newSetPart + " " + afterSet
			fmt.Fprintf(os.Stderr, "DEBUG SANITIZED SQL: %s\n", query)
		}
	}

	// Map args to map[string]interface{}
	params := make(map[string]interface{})
	for i, v := range args {
		// Handle json.Marshaler same as executeSQL
		if m, ok := v.(json.Marshaler); ok {
			if b, err := m.MarshalJSON(); err == nil {
				var iv interface{}
				if err := json.Unmarshal(b, &iv); err == nil {
					params[fmt.Sprintf("p%d", i+1)] = iv
					continue
				}
			}
		}
		params[fmt.Sprintf("p%d", i+1)] = v
	}

	fmt.Printf("DEBUG EXEC: %s PARAMS: %+v\n", query, params)

	results, err := surrealdb.Query[interface{}](ctx, dialector.Conn, query, params)
	if err != nil {
		return nil, err
	}

	// Count rows affected
	var count int64
	if len(*results) > 0 {
		res := (*results)[0]
		if res.Status != "OK" {
			return nil, fmt.Errorf("surrealdb exec error: %v", res)
		}
		if res.Result != nil {
			val := reflect.ValueOf(res.Result)
			if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
				count = int64(val.Len())
			} else {
				// assume 1 if not slice but valid result?
				count = 1
			}
		}
	}

	return DriverResult{Rows: count}, nil
}

type DriverResult struct {
	Rows int64
}

func (DriverResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (d DriverResult) RowsAffected() (int64, error) {
	return d.Rows, nil
}

func (dialector *Dialector) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("method QueryContext not implemented")
}

func (dialector *Dialector) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	// We cannot return *sql.Row easily because it requires *sql.Rows which we can't create.
	// However, GORM might use this for `First`, `Take` etc if we don't replace callbacks.
	// But we replaced Query logic.
	return nil
}
