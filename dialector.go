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
	"sync"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

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
	// Store canonical name → canonical name
	d.edgeTables.Store(table, table)
	// Also store singular alias → canonical name (strip trailing "s" if present)
	if strings.HasSuffix(table, "s") {
		singular := strings.TrimSuffix(table, "s")
		d.edgeTables.Store(singular, table)
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
	// Try name+"s" as canonical (e.g. caller passes "wishlist", we try "wishlists")
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

	if field.FieldType == reflect.TypeOf(&sdkModels.RecordID{}) {
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

	// Intercept INSERT INTO <edge_table> generated by GORM's many2many association handler.
	// GORM generates: INSERT INTO `wishlist` (`buyer_id`,`product_id`) VALUES ($p1,$p2) ...
	// We translate this to a SurrealDB InsertRelation call.
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	if strings.HasPrefix(upperQuery, "INSERT INTO") {
		// Extract bare table name (strip backticks/quotes)
		rest := strings.TrimSpace(query[len("INSERT INTO"):])
		tbl := strings.FieldsFunc(rest, func(r rune) bool {
			return r == ' ' || r == '(' || r == '`' || r == '"'
		})
		if len(tbl) > 0 {
			// Use FindEdgeTable so that both the exact tag name and its pluralized form work.
			if registeredName, ok := dialector.FindEdgeTable(tbl[0]); ok {
				// GORM lists source FK first (→ in) and target FK second (→ out).
				if len(args) >= 2 {
					inID := extractRecordID(args[0])
					outID := extractRecordID(args[1])
					if inID != nil && outID != nil {
						rel := &surrealdb.Relationship{
							In:       *inID,
							Out:      *outID,
							Relation: sdkModels.Table(registeredName),
						}
						if _, err := surrealdb.InsertRelation[interface{}](ctx, dialector.Conn, rel); err != nil {
							return nil, err
						}
						return DriverResult{Rows: 1}, nil
					}
				}
			}
		}
	}

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

// extractRecordID coerces an interface{} arg value into a *sdkModels.RecordID.
// It handles *sdkModels.RecordID and TypesM.RecordID (wrapper) directly.
func extractRecordID(v interface{}) *sdkModels.RecordID {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	concrete := rv.Interface()
	switch id := concrete.(type) {
	case sdkModels.RecordID:
		return &id
	case TypesM.RecordID:
		native := id.RecordID
		return &native
	}
	// Try JSON round-trip as last resort
	b, err := json.Marshal(concrete)
	if err != nil {
		return nil
	}
	var rid sdkModels.RecordID
	if err := json.Unmarshal(b, &rid); err != nil {
		return nil
	}
	return &rid
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
