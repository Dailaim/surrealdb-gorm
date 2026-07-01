package surrealdb

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/surrealdb/surrealdb.go"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

// ============================================================================
// database/sql/driver layer
//
// *sql.Rows and *sql.Row are concrete types with no public constructor, so the
// only way to return real rows from Dialector.QueryContext / QueryRowContext is
// to implement a database/sql driver and open a *sql.DB with sql.OpenDB.
//
// This layer is ADDITIVE: the normal GORM callbacks (Create/Query/Update/Delete)
// still run through executeSQL and the SDK directly. It only backs the raw-row
// paths — db.Raw(sql).Rows(), db.ScanRows, LIVE SELECT / SHOW CHANGES scanning —
// that previously failed because QueryContext was a stub.
// ============================================================================

// sdConnector is a database/sql driver.Connector backed by an existing
// SurrealDB connection owned by the Dialector.
type sdConnector struct {
	dialector *Dialector
}

func (c *sdConnector) Connect(_ context.Context) (driver.Conn, error) {
	if c.dialector == nil || c.dialector.Conn == nil {
		return nil, fmt.Errorf("surrealdb: connection not initialized")
	}
	return &sdConn{dialector: c.dialector}, nil
}

func (c *sdConnector) Driver() driver.Driver { return sdDriver{} }

// sdDriver exists to satisfy driver.Connector.Driver(). Open is unused because
// connections are created via sql.OpenDB(connector), not sql.Open(name, dsn).
type sdDriver struct{}

func (sdDriver) Open(string) (driver.Conn, error) {
	return nil, fmt.Errorf("surrealdb: use sql.OpenDB with a connector, not sql.Open")
}

// sdConn wraps the shared SurrealDB connection. It implements QueryerContext and
// ExecerContext so database/sql never needs Prepare, and NamedValueChecker so
// GORM's custom argument types pass through untouched (we convert them to
// SDK-native CBOR values ourselves via ToSDKValue).
type sdConn struct {
	dialector *Dialector
}

func (c *sdConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("surrealdb: Prepare not supported; queries run via QueryerContext")
}

func (c *sdConn) Close() error { return nil }

func (c *sdConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("surrealdb: transactions are handled by gorm.ConnPoolBeginner, not database/sql")
}

// CheckNamedValue accepts every argument as-is. Without this, database/sql would
// reject GORM's non-standard argument types (e.g. *types.RecordID) before they
// reach us.
func (c *sdConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *sdConn) params(args []driver.NamedValue) map[string]interface{} {
	params := make(map[string]interface{}, len(args))
	for _, a := range args {
		key := a.Name
		if key == "" {
			key = fmt.Sprintf("p%d", a.Ordinal)
		}
		params[key] = TypesM.ToSDKValue(a.Value)
	}
	return params
}

func (c *sdConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.dialector == nil || c.dialector.Conn == nil {
		return nil, fmt.Errorf("surrealdb: connection not initialized")
	}
	results, err := surrealdb.Query[interface{}](ctx, c.dialector.Conn, query, c.params(args))
	if err != nil {
		return nil, &Error{Op: "query", Query: query, Err: err}
	}
	if results == nil || len(*results) == 0 {
		return newRows(nil), nil
	}
	res := (*results)[0]
	if res.Status != "OK" {
		return nil, newStatusError("query", query, res.Status, res.Result)
	}
	return newRows(res.Result), nil
}

func (c *sdConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.dialector == nil || c.dialector.Conn == nil {
		return nil, fmt.Errorf("surrealdb: connection not initialized")
	}
	results, err := surrealdb.Query[interface{}](ctx, c.dialector.Conn, query, c.params(args))
	if err != nil {
		return nil, &Error{Op: "exec", Query: query, Err: err}
	}
	var n int64
	if results != nil && len(*results) > 0 {
		res := (*results)[0]
		if res.Status != "OK" {
			return nil, newStatusError("exec", query, res.Status, res.Result)
		}
		n = int64(countResult(res.Result))
	}
	return DriverResult{Rows: n}, nil
}

// sdRows implements driver.Rows over a SurrealDB result set. Column names are
// derived from the keys of the first document (schemaless documents), or a
// single "value" column when the rows are scalars.
type sdRows struct {
	columns []string
	rows    []interface{}
	idx     int
}

// newRows normalizes a SurrealDB result (usually []interface{} of maps, but also
// a single map or scalar) into an sdRows.
func newRows(result interface{}) *sdRows {
	r := &sdRows{}
	switch v := result.(type) {
	case nil:
		return r
	case []interface{}:
		r.rows = v
	case []map[string]interface{}:
		r.rows = make([]interface{}, len(v))
		for i := range v {
			r.rows[i] = v[i]
		}
	case map[string]interface{}:
		r.rows = []interface{}{v}
	default:
		r.rows = []interface{}{v}
	}
	if len(r.rows) > 0 {
		if m, ok := r.rows[0].(map[string]interface{}); ok {
			cols := make([]string, 0, len(m))
			for k := range m {
				cols = append(cols, k)
			}
			sort.Strings(cols)
			r.columns = cols
		} else {
			r.columns = []string{"value"}
		}
	}
	return r
}

func (r *sdRows) Columns() []string { return r.columns }

func (r *sdRows) Close() error { return nil }

func (r *sdRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++

	if m, ok := row.(map[string]interface{}); ok {
		for i, col := range r.columns {
			if i >= len(dest) {
				break
			}
			dest[i] = toDriverValue(m[col])
		}
		return nil
	}

	// Scalar row → single "value" column.
	if len(dest) > 0 {
		dest[0] = toDriverValue(row)
	}
	return nil
}

// toDriverValue coerces an arbitrary Go value returned by the SDK into a type
// database/sql accepts as a driver.Value. Scalars pass through (normalized to
// int64/float64); everything else (records, nested objects, arrays) is
// JSON-encoded so custom sql.Scanner types and GORM can decode it.
func toDriverValue(v interface{}) driver.Value {
	switch x := v.(type) {
	case nil:
		return nil
	case bool:
		return x
	case string:
		return x
	case []byte:
		return x
	case time.Time:
		return x
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case uint:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		return int64(x)
	case float32:
		return float64(x)
	case float64:
		return x
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return b
	}
}

// countResult reports how many records a result value represents.
func countResult(result interface{}) int {
	switch v := result.(type) {
	case nil:
		return 0
	case []interface{}:
		return len(v)
	case []map[string]interface{}:
		return len(v)
	default:
		return 1
	}
}
