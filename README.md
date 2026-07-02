# surrealdb-gorm

[![CI](https://github.com/Dailaim/surrealdb-gorm/actions/workflows/ci.yml/badge.svg)](https://github.com/Dailaim/surrealdb-gorm/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dailaim/surrealdb-gorm.svg)](https://pkg.go.dev/github.com/dailaim/surrealdb-gorm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

A [GORM v2](https://gorm.io) driver for [SurrealDB](https://surrealdb.com), mapping GORM's relational model onto SurrealDB's document-graph hybrid database using native SurrealQL.

> **Status**: v1.3.0 — stable API. SurrealDB **v3+ recommended** (interactive transactions, `ALTER FIELD`, `DROP CHANGEFEED`, vector `<|k,EF|>` search require it); works on v2 without those features. The driver creates the namespace/database on connect if missing and auto-reconnects on transient WebSocket drops.

---

## Installation

```bash
go get github.com/dailaim/surrealdb-gorm@v1.3.0
```

**Requirements**: Go 1.25+, SurrealDB v3+ (v2 works with reduced features), WebSocket endpoint. SDK: `surrealdb.go` v1.5.0.

---

## Quick Start

```go
import (
    surrealdb "github.com/dailaim/surrealdb-gorm"
    "github.com/dailaim/surrealdb-gorm/models"
    "gorm.io/gorm"
)

type User struct {
    models.BaseModel
    Name  string `json:"name"`
    Email string `json:"email" gorm:"uniqueIndex"`
}

func main() {
    dsn := "ws://localhost:8000/rpc?namespace=test&database=test&username=root&password=root"
    db, err := gorm.Open(surrealdb.Open(dsn), &gorm.Config{})
    if err != nil {
        panic(err)
    }

    db.AutoMigrate(&User{})

    user := User{Name: "Alice", Email: "alice@example.com"}
    db.Create(&user)
    fmt.Println(user.ID) // users:abc123
}
```

### DSN Format

```
ws://<host>:<port>/rpc?namespace=<NS>&database=<DB>&username=<USER>&password=<PASS>
```

### Config constructor

To avoid credentials in a DSN string, use `New(Config{...})`:

```go
db, err := gorm.Open(surrealdb.New(surrealdb.Config{
    Endpoint:  "ws://localhost:8000/rpc",
    Namespace: "test",
    Database:  "test",
    Username:  "root",
    Password:  "root",
    // ReconnectInterval: 5 * time.Second, // 0 = default, <0 = disable
}), &gorm.Config{})
```

WebSocket connections auto-reconnect on transient drops and replay the SignIn token + `USE`; tune or disable via `ReconnectInterval`.

### Errors

Query failures are wrapped in `*surrealdb.Error` (inspect with `errors.As`):

```go
var serr *surrealdb.Error
if errors.As(db.Error, &serr) {
    log.Printf("surrealdb %s failed (%s): %s\n%s", serr.Op, serr.Status, serr.Detail, serr.Query)
}
```

`db.Debug()` prints the generated SurrealQL through GORM's logger.

---

## Models

### BaseModel

Provides `ID` (SurrealDB RecordID), `CreatedAt`, `UpdatedAt`, and soft-delete via `DeletedAt`:

```go
type Product struct {
    models.BaseModel
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}
```

### Schema Modes

By default tables are `SCHEMALESS`. Implement `SchemaFull()` to opt into `SCHEMAFULL`:

```go
type StrictModel struct {
    models.BaseModel
    Name string `json:"name"`
}

func (StrictModel) SchemaFull() bool { return true }
```

---

## Types

The `types` package provides SurrealDB-native Go types that serialize correctly over CBOR:

| Go Type | SurrealDB Type | Notes |
|---|---|---|
| `types.RecordID` | `record<T>` | Parsed from `"table:id"` strings |
| `types.Link[T]` | `record<T>` | Smart link: holds ID or full object after FETCH |
| `types.SliceLink[T]` | `array<record<T>>` | Slice of links |
| `types.DateTime` | `datetime` | Wraps `time.Time` |
| `types.Decimal` | `decimal` | High-precision via shopspring/decimal |
| `types.UUID` | `uuid` | RFC 4122 UUID |
| `types.Duration` | `duration` | SurrealDB duration |
| `types.GeometryPoint` | `geometry` | GeoJSON point |

Also available: `types.Duration`, `types.Bytes`, `types.Regex`, and the geometry family (`GeometryLine`, `GeometryPolygon`, …).

```go
import "github.com/dailaim/surrealdb-gorm/types"

id, err := types.ParseRecordID("users:abc")
```

Any SurrealDB type can be set verbatim with the `type:` tag, including literal/enum unions (enforced by the schema):

```go
type Ticket struct {
    models.BaseModel
    Status string `gorm:"type:'open'|'closed'|'pending'"`
}
```

> `set<T>` is reachable as a schema type, but SurrealDB does not auto-coerce a plain array to a set on write. The v3 `file` type has no SDK model yet.

---

## Graph Relations (Edges)

SurrealDB models graph edges as first-class tables. Use `models.Edge[In, Out]`:

```go
type Follows struct {
    models.EdgeBaseModel[User, User]  // ID, In, Out + timestamps + soft-delete
    Since time.Time `json:"since"`
}

// Create a graph edge
err := db.AutoMigrate(&Follows{})

// Relate two nodes
result, err := surrealdb.Relate(ctx, dialector, "users:alice", "follows", "users:bob",
    map[string]any{"since": time.Now()})
```

### FETCH (Preload)

```go
// GORM Preload translates to SurrealDB FETCH
var user User
db.Preload("Follows").First(&user, "users:alice")
```

---

## Transactions

### GORM-native transactions (SurrealDB v3+)

```go
err := db.Transaction(func(tx *gorm.DB) error {
    tx.Create(&User{Name: "Bob"})
    tx.Create(&User{Name: "Carol"})
    return nil
})
```

### Raw SurrealQL transactions

```go
err := surrealdb.Transaction(db, func(tx *surrealdb.Tx) error {
    tx.Exec("UPDATE accounts SET balance -= $amt WHERE owner = $owner",
        map[string]any{"amt": 100, "owner": "alice"})
    tx.Exec("UPDATE accounts SET balance += $amt WHERE owner = $owner",
        map[string]any{"amt": 100, "owner": "bob"})
    return tx.Err()
})
```

---

## Batch Insert

```go
users := []User{{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"}}
err := surrealdb.CreateMany(db, &users)
```

Generates a single `INSERT INTO users [{...}, {...}, {...}]`.

---

## Live Queries (Real-Time)

> Requires WebSocket connection.

```go
live, err := surrealdb.NewLiveQuery(db, "users", false)

notifications, err := live.Notifications()
for notif := range notifications {
    fmt.Println(notif.Action, notif.Result)
}

live.Kill()
```

---

## Full-Text Search

```go
m := db.Migrator().(surrealdb.Migrator)

// Define an analyzer
m.DefineEdgeNgramAnalyzer("autocomplete", 2, 10)

// Use it in a field index
type Article struct {
    models.BaseModel
    Title string `json:"title" gorm:"index:idx_title,class:FULLTEXT,option:analyzer:autocomplete"`
}
db.AutoMigrate(&Article{})
```

---

## Vector Search (Embeddings)

Define an `HNSW` (or `MTREE`) index on an embedding field via the index tag, then run kNN/ANN queries:

```go
type Doc struct {
    models.BaseModel
    Embedding []float64 `gorm:"type:array<float>;index:idx_emb,class:HNSW,option:dimension=768 dist=cosine efc=150 m=12"`
}
db.AutoMigrate(&Doc{})

// k-nearest-neighbors search (v3 operator <|k,EF|>)
var ids []string
db.Raw("SELECT id FROM docs WHERE embedding <|5,100|> $q", queryVec).Scan(&ids)
```

Index params (`dimension`, `type`, `dist`, `efc`, `m`, `capacity`) are space-separated `key=value` pairs in the `option:` tag; `dimension` is required.

---

## Computed & Advanced Fields

`AutoMigrate` emits these `DEFINE FIELD` clauses from struct tags:

```go
type Person struct {
    models.BaseModel
    Name      string         `json:"name"`
    NameUpper string         `json:"name_upper" gorm:"type:string;value:string::uppercase(name)"` // computed
    Meta      map[string]any `json:"meta" gorm:"type:object;flexible"`                            // FLEXIBLE
    Email     string         `json:"email" gorm:"assert:string::is::email($value);permissions:FULL"`
}
```

- `value:<expr>` → computed field (`VALUE`)
- `flexible` → `FLEXIBLE` (arbitrary nested content)
- `assert:<expr>` → `ASSERT`
- `permissions:<clause>` → field `PERMISSIONS`

---

## Events (Database Triggers)

```go
m := db.Migrator().(surrealdb.Migrator)
m.DefineEvent("users", "audit_log", surrealdb.EventOptions{
    When: "$event = 'UPDATE' AND $before.email != $after.email",
    Then: "CREATE logs SET user = $value.id, action = 'email changed', at = time::now()",
})
```

---

## Changefeeds (CDC)

```go
m := db.Migrator().(surrealdb.Migrator)
m.SetTableChangefeed("users", "1h", false)

sql := surrealdb.ShowChangesSQL("users", time.Now().Add(-time.Hour), 100)
```

---

## Schema Migrations

`AutoMigrate` creates or updates tables and fields. For existing tables it applies `DEFINE FIELD OVERWRITE` to pick up type changes, and drops fields that have been removed from the struct.

```go
db.AutoMigrate(&User{}, &Product{}, &Follows{})
```

### Migration Helpers

```go
m := db.Migrator().(surrealdb.Migrator)

m.DropField("users", "legacy_column")
m.RenameField("users", "old_name", "new_name")
m.RemoveTableIndex("users", "idx_name")
m.RenameTableTo("old_table", "new_table")
m.ChangeFieldType("users", "age", "int")
m.MakeFieldReadonly("users", "email")
m.AddFieldAssert("users", "name", "string::len($value) > 0")
m.SetTableSchemaFull("users")
m.CompactTable("users")

// GORM Migrator interface (native, via INFO FOR TABLE)
m.HasColumn(&User{}, "email")
m.HasIndex(&User{}, "idx_title")
m.RenameColumn(&User{}, "old", "new")
```

### DEFINE Helpers

For statements outside GORM's schema model:

```go
m.DefineParam("endpointBase", `"https://example.com"`)
m.DefineFunction("greet", "$name: string", "RETURN 'Hello ' + $name;")
m.DefineSequence("order_seq", surrealdb.SequenceOptions{Batch: 1000, Start: 1})
m.DefineUser(surrealdb.UserOptions{Name: "bot", Level: "ROOT", Password: "secret", Roles: "VIEWER"})

// Generic escape hatch for ACCESS / API / BUCKET / CONFIG / MODEL
m.Define("ACCESS app ON DATABASE TYPE RECORD ... DURATION FOR SESSION 1h")
m.Remove("ACCESS app ON DATABASE")
```

---

## Raw Queries & Rows

`Scan` and native `*sql.Rows` iteration both work:

```go
// Scan into a struct/slice
var users []User
db.Raw("SELECT * FROM users WHERE age > $1", 18).Scan(&users)

// Iterate real *sql.Rows (v1.1.0+)
rows, _ := db.Raw("SELECT * FROM users").Rows()
defer rows.Close()
for rows.Next() {
    var u User
    db.ScanRows(rows, &u)
}
```

Columns are derived from the first document's keys. `.Rows()` inside `db.Transaction(...)` is not yet supported.

---

## Query Explain

```go
dialector := db.Dialector.(*surrealdb.Dialector)
plan, err := dialector.ExplainQuery(ctx, "SELECT * FROM users WHERE name = $p1", "Alice")
fmt.Println(plan.Detail)
```

---

## Docker

A `docker-compose.yml` is included for local development:

```bash
docker compose up -d
```

Starts SurrealDB at `ws://localhost:8000/rpc`.

---

## Architecture

```
surrealdb.go        Open(), New(Config), Relate()
connection.go       Auto-reconnecting WebSocket connection (rews)
dialector.go        Dialector, DataTypeOf, edge table registry
driver.go           ConnPool, ExecContext, BeginTx, SQL rewrites
executor.go         Query execution, tx routing, logging, parameter serialization
errors.go           Typed *surrealdb.Error
sqldriver.go        database/sql/driver layer → real *sql.Rows
callback_create.go  GORM CREATE → SurrealDB Create/InsertRelation/Relate
callback_update.go  GORM UPDATE → SurrealDB MERGE/UPDATE
callback_delete.go  GORM DELETE → SurrealDB DELETE / soft-delete
callback_query.go   GORM SELECT → SurrealQL SELECT
callback_row.go     GORM Row/Rows callback
migrator.go         AutoMigrate, DEFINE TABLE/FIELD/INDEX (incl. vector)
define.go           DEFINE PARAM/FUNCTION/SEQUENCE/USER helpers
alter.go            ALTER TABLE/FIELD, changefeed, migration helpers
transaction.go      Native interactive Tx (v3+) + raw Tx builder
types/              Custom Go↔SurrealDB type system (CBOR-safe)
models/             BaseModel, EdgeBaseModel, Edge[T,U]
clauses/            FETCH, graph SELECT clause extensions
```

---

## Limitations

- **Single connection (no pool)**: requests are serialized over one mutex-guarded WebSocket. Correct and concurrency-safe (transactions are UUID-tagged), but not a throughput pool.
- **Preload cardinality**: SurrealDB `FETCH` returns only the first related record for 1:N edges. Use raw `SELECT ... FETCH` for bulk graph traversal.
- **Interactive transactions** require SurrealDB v3+ (WebSocket only). `db.Raw(...).Rows()` inside a transaction is not yet wired.
- **Reconnection** recovers transient drops (server stays up, token valid); a full server restart that wipes state / regenerates signing keys is not recoverable.
- **`set<T>`** is not auto-coerced from arrays on write, and the v3 **`file`** type has no SDK model yet.

---

## License

MIT
