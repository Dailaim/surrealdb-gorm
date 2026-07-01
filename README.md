# surrealdb-gorm

[![CI](https://github.com/Dailaim/surrealdb-gorm/actions/workflows/ci.yml/badge.svg)](https://github.com/Dailaim/surrealdb-gorm/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dailaim/surrealdb-gorm.svg)](https://pkg.go.dev/github.com/dailaim/surrealdb-gorm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

A [GORM v2](https://gorm.io) driver for [SurrealDB](https://surrealdb.com), mapping GORM's relational model onto SurrealDB's document-graph hybrid database using native SurrealQL.

> **Status**: v1.0.0 — stable API. SurrealDB **v3+ recommended** (interactive transactions, `ALTER FIELD`, and `DROP CHANGEFEED` require it); works on v2 without those features. The driver creates the namespace/database on connect if missing.

---

## Installation

```bash
go get github.com/dailaim/surrealdb-gorm@v1.0.0
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

```go
import "github.com/dailaim/surrealdb-gorm/types"

id, err := types.ParseRecordID("users:abc")
```

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
```

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
surrealdb.go        Open(), Relate()
dialector.go        Dialector, DataTypeOf, edge table registry
driver.go           ConnPool, ExecContext, BeginTx
executor.go         Query execution, parameter serialization
callback_create.go  GORM CREATE → SurrealDB Create/InsertRelation/Relate
callback_update.go  GORM UPDATE → SurrealDB MERGE/UPDATE
callback_delete.go  GORM DELETE → SurrealDB DELETE / soft-delete
callback_query.go   GORM SELECT → SurrealQL SELECT
migrator.go         AutoMigrate, DEFINE TABLE/FIELD/INDEX
transaction.go      Native interactive Tx (v3+) + raw Tx builder
types/              Custom Go↔SurrealDB type system (CBOR-safe)
models/             BaseModel, EdgeBaseModel, Edge[T,U]
clauses/            FETCH, graph SELECT clause extensions
```

---

## Limitations

- **No HTTP transport**: WebSocket only (required for live queries and interactive transactions).
- **Preload cardinality**: SurrealDB `FETCH` returns only the first related record for 1:N edges. Use raw `SELECT ... FETCH` for bulk graph traversal.
- **Interactive transactions** require SurrealDB v3+.
- **Raw `sql.Rows`** are not returned; use `db.Raw(...).Scan(&dest)`.

---

## License

MIT
