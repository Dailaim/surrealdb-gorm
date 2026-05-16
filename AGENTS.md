# AGENTS.md — SurrealDB GORM Driver

> This file is for AI coding agents. It contains architectural decisions,
> conventions, and critical context that is NOT in README.md.

## Project Overview

This is a **GORM v2 driver for SurrealDB**. It maps GORM's relational model
onto SurrealDB's document-graph hybrid database, leveraging native SurrealQL
features rather than emulating them in Go.

- **Package**: `github.com/dailaim/surrealdb-gorm`
- **Package name**: `surrealdb` (Go convention, no underscores)
- **SDK**: `github.com/surrealdb/surrealdb.go` v1.4.0 (CBOR wire protocol)
- **DSN format**: `ws://host:port/rpc?namespace=NS&database=DB&username=USR&password=PWD`

---

## Critical Type System Decisions

### Custom Types → SDK CBOR Mapping

SurrealDB v1.4.0 uses CBOR, not JSON, for the wire protocol. Passing a raw
`time.Time` serializes as a struct, which SurrealDB rejects with:

```
Expected datetime but found {...}
```

**Solution**: `types/sdk.go` contains `ToSDKValue(v any) (any, error)` which
converts every custom type to its SDK-native CBOR-tagged counterpart:

| Custom Type | SDK CBOR Type | Pointer? |
|-------------|---------------|----------|
| `types.DateTime` | `*models.CustomDateTime` | Yes (pointer receiver on MarshalCBOR) |
| `types.Duration` | `*models.CustomDuration` | Yes |
| `types.Decimal` | `*models.Decimal` | Yes |
| `types.UUID` | `models.UUID` | No (value receiver) |
| `types.GeometryPoint` | `*models.GeometryPoint` | Yes |
| `types.GeometryLine` | `*models.GeometryLine` | Yes |
| `types.GeometryPolygon` | `*models.GeometryPolygon` | Yes |
| `types.GeometryMultiPoint` | `*models.GeometryMultiPoint` | Yes |
| `types.GeometryMultiLine` | `*models.GeometryMultiLine` | Yes |
| `types.GeometryMultiPolygon` | `*models.GeometryMultiPolygon` | Yes |
| `types.GeometryCollection` | `*models.GeometryCollection` | Yes |
| `[]byte` (Bytes) | `models.Bytes` | No (value) |

**Rule**: Before sending ANY value to the SDK via `db.Exec`, `db.Raw`, or
callbacks, pass it through `ToSDKValue`. This is already done in:
- `callback_create.go`
- `driver.go`
- `executor.go`

### JSON Marshaler Trap

Do NOT rely on `json.Marshaler` for SDK types. If you do something like:

```go
if m, ok := v.(json.Marshaler); ok { return m.MarshalJSON() }
```

`time.Time` will be converted to an RFC3339 string BEFORE the SDK sees it,
which SurrealDB rejects. Always route through `ToSDKValue` first.

### Geometry Stack Overflow Fix

Geometry types used to stack-overflow on `MarshalJSON` / `UnmarshalJSON` due to
infinite recursion on embedding `models.GeometryPoint`. Fixed by using the
`type Alias` pattern:

```go
func (p GeometryPoint) MarshalJSON() ([]byte, error) {
    type Alias models.GeometryPoint // breaks recursion
    return json.Marshal((*Alias)(&p))
}
```

### Geometry Scan Formats

`GeometryPoint.Scan` handles THREE input formats from SurrealDB:
1. **GeoJSON**: `{"type":"Point","coordinates":[x,y]}`
2. **SurrealDB tuple**: `[x, y]`
3. **SDK struct**: `{"Longitude":x,"Latitude":y}`

All three must be supported because different query paths return different
representations.

---

## Schema Migration (Migrator)

### Table Creation

- Tables are created with `DEFINE TABLE ... SCHEMALESS` by default
- If a model implements `models.SchemaFull` (method `SchemaFull() bool` returning `true`), the table is created as `SCHEMAFULL`
- Edge tables use `DEFINE TABLE ... TYPE RELATION FROM in_table TO out_table` (native SurrealDB syntax, not separate `DEFINE FIELD in/out`)
- Edge tables use `DEFINE TABLE ... TYPE RELATION FROM in_table TO out_table`
  (native SurrealDB syntax, not separate `DEFINE FIELD in/out`)

### Field Definitions

- `option<datatype>` is used for nullable fields (documented SurrealDB syntax)
- `datatype` (not wrapped) for non-null fields
- `READONLY` is controlled by the `gorm:"readonly"` struct tag, NOT hardcoded on `created_at`/`id`
- `DEFAULT` is generated from GORM's `default` tag
- `REFERENCE ON DELETE UNSET` for optional `record<T>`
- `REFERENCE ON DELETE REJECT` for `NotNull` `record<T>`

### Index Definitions

- UNIQUE: `DEFINE INDEX ... UNIQUE`
- FULLTEXT: `DEFINE INDEX ... SEARCH ANALYZER <name>`
- Fulltext analyzer name can be passed via `gorm:"index:,class:FULLTEXT,option:analyzer:myAnalyzer"`

### Smart Migration

- `OVERWRITE` (v2.0+) is used for existing field changes
- `removeObsoleteFields` queries `INFO FOR TABLE x`, compares with current GORM
  schema, and drops fields that no longer exist

### Link / SliceLink Type Inference

`DataTypeOf` in `dialector.go` infers:
- `Link[T]` → `record<table_of_T>`
- `SliceLink[T]` → `array<record<table_of_T>>`

Table name is derived via reflection on the generic type parameter `T` using
`GormDataType()`. `SliceLink[T].GormDataType()` calls `Link[T].GormDataType()`
which uses `reflect.TypeFor[Link[T]]().Elem().FieldByName("To")` to find the
underlying record type and extract its table name.

---

## Graph Relations (Edges)

### Edge Model

```go
type Wishes struct {
    models.Edge[Person, Product]  // ID, In, Out links
    Since time.Time `json:"since"`
}
```

- `models.Edge[T, U]` provides `ID`, `In`, `Out`, `InTableName()`, `OutTableName()`
- Its fields are **flattened** into the parent table (no nested `edge` blob) because `Edge` does **not** implement `driver.Valuer` / `sql.Scanner`
- **Do NOT mix with `models.BaseModel`** — `Edge` already has its own `ID`; embedding both would create duplicate ID fields.
- Use `models.EdgeBaseModel[T, U]` when you need timestamps + soft-delete on an edge:
  ```go
  type Wishes struct {
      models.EdgeBaseModel[Person, Product]  // ID, In, Out, CreatedAt, UpdatedAt, DeletedAt
      Since time.Time
  }
  ```
- Soft-delete on edges is fully supported: `db.Delete(&edge)` sets `deleted_at`, normal queries hide it, `.Unscoped()` reveals it
- Constructor helper: `models.NewEdge[T, U](inID, outID *types.RecordID)`
- `inferEdgeEndpointTable` uses reflection to call `InTableName()` / `OutTableName()`
- Edge table is registered via `Dialector.RegisterEdgeTable` so that callbacks can
distinguish edge tables from regular tables

### Native RELATE Statement

```go
surrealdb.Relate(db, personID, "wishes", productID, map[string]any{"since": time.Now()})
```

Generates: `RELATE person:1 WISHES product:2 SET since = <datetime>`

### Fetch (Preload)

`FETCH` is SurrealDB's native graph traversal. The driver implements GORM
`Preload` using native `FETCH out` / `FETCH in` where possible. However,
SurrealDB FETCH has cardinality limits (returns only the first related record
for 1:N edges). Tests that need bulk graph operations use native `SELECT ... FETCH`
or raw queries instead of GORM Preload.

---

## Live Queries (Real-Time Subscriptions)

```go
live, err := surrealdb.NewLiveQuery(db, "users", false)
notifications, err := live.Notifications()
// ... read from channel ...
live.Kill()
```

- `LiveSelect` returns a live query UUID (string)
- `LiveNotifications` returns a `<-chan connection.Notification`
- `KillLiveQuery` terminates the subscription
- Only works with WebSocket connections (not HTTP)
- Raw `LIVE SELECT` via GORM `db.Raw(...).Scan(...)` is NOT supported because
the driver returns the live ID via a different mechanism

---

## Events (Database Triggers)

```go
m.DefineEvent("users", "audit_log", surrealdb.EventOptions{
    When: "$before.email != $after.email",
    Then: "CREATE `logs` SET user = $value.id, action = 'email changed', at = time::now()",
})
```

- `WHEN` condition controls when the event fires
- `THEN` is the action (can be a block or expression)
- `ASYNC` with optional `RETRY` and `MAXDEPTH` supported
- **WARNING**: Events that UPDATE the same table can cause infinite recursion.
Always use `WHEN "$event = 'CREATE'"` or similar guards.

---

## Analyzers (Full-Text Search)

```go
m.DefineEdgeNgramAnalyzer("autocomplete", 2, 10)
m.DefineSnowballAnalyzer("english", "english")
m.DefineBasicAnalyzer("simple")
```

- `DEFINE ANALYZER` with TOKENIZERS and FILTERS
- Pre-built helpers for common patterns: basic, edge-ngram, snowball, n-gram, ASCII
- Fulltext indexes reference the analyzer by name: `SEARCH ANALYZER <name>`

---

## Changefeed (CDC)

```go
m.SetTableChangefeed("users", "1h", false)  // enable
m.DropTableChangefeed("users")               // disable

sql := surrealdb.ShowChangesSQL("users", time.Now().Add(-time.Hour), 10)
```

- `CHANGEFEED` is defined on tables via `ALTER TABLE ... CHANGEFEED <duration>`
- `INCLUDE ORIGINAL` stores reverse diffs (before-image)
- `SHOW CHANGES FOR TABLE ... SINCE ... LIMIT ...` replays mutations
- Reading changefeeds via GORM `Scan` is unreliable due to driver limitations;
use raw query execution and handle the result yourself

---

## ALTER TABLE / ALTER FIELD

```go
m.MakeFieldReadonly("users", "email")
m.MakeFieldMutable("users", "email")
m.ChangeFieldType("users", "age", "int")
m.AddFieldAssert("users", "name", "string::len($value) > 0")
m.CompactTable("users")
m.SetTableSchemaFull("users")
```

- Supports `DROP TYPE`, `DROP READONLY`, `DROP ASSERT`, `DROP DEFAULT`, etc.
- `ALTER FIELD IF EXISTS ON TABLE` syntax (note: `ON` keyword, no `TABLE` keyword after `ON`)
- Convenience wrappers for common operations

---

## Testing Conventions

- All tests live in `test/` package (external test package)
- DSN points to `ws://10.89.1.1:8000/rpc` (local SurrealDB instance)
- Credentials: `root` / `root`, namespace `test`, database `test`
- `setupDB(t)` creates a fresh GORM connection and AutoMigrates `User`
- Tests should clean up tables they create to avoid cross-test pollution
- Some driver features (LIVE SELECT raw, SHOW CHANGES Scan) are skipped in
tests because the Go SDK does not return standard `sql.Rows` for them

### Known Test Workarounds

- `TestBulkGraphOperations`, `TestPreloadWithComplexQuery`, `TestReversePreloadMassive`:
  Use native `SELECT ... FETCH` instead of GORM Preload because SurrealDB FETCH
  has cardinality limits for 1:N edges.

---

## File Inventory

| File | Purpose |
|------|---------|
| `surrealdb.go` | Dialector registration, `Open()`, `Relate()` wrapper |
| `dialector.go` | `Dialector` struct, `DataTypeOf`, `ExplainQuery`, `Migrator()` |
| `driver.go` | `Conn`, `Stmt`, `Tx` implementations, parameter serialization |
| `executor.go` | Query execution, `ToSDKValue` integration |
| `callback_create.go` | GORM CREATE callback with CBOR serialization |
| `callback_delete.go` | GORM DELETE callback |
| `callback_update.go` | GORM UPDATE callback |
| `migrator.go` | `AutoMigrate`, `defineFields`, `defineIndexes`, `removeObsoleteFields` |
| `alter.go` | `AlterTable`, `AlterField`, convenience helpers |
| `analyzer.go` | `DefineAnalyzer`, pre-built analyzer helpers |
| `event.go` | `DefineEvent`, `RemoveEvent`, convenience helpers |
| `live.go` | `LiveSelect`, `LiveNotifications`, `KillLiveQuery` |
| `types/types.go` | Geometry, Duration, Bytes, Regex, DateTime, Decimal, UUID, RecordID |
| `types/sdk.go` | `ToSDKValue()` mapping custom types → SDK CBOR types |
| `types/link.go` | `Link[T]` with `GormDataType()` inferring `record<T>` |
| `types/slice-link.go` | `SliceLink[T]` with array inference |
| `models/base.go` | `BaseModel` with ID, timestamps, soft-delete |
| `models/schemas.go` | `SchemaFull` interface (opt-in to SCHEMAFULL) |
| `models/relation.go` | `Edge[T,U]`, `EdgeRelation`, `NewEdge[T,U]` constructor |
| `clauses/` | SurrealDB-specific GORM clauses (FETCH, RELATE, etc.) |
| `test/` | Full test suite |

---

## Dependencies

```
gorm.io/gorm v1.31.1
github.com/surrealdb/surrealdb.go v1.4.0
github.com/shopspring/decimal v1.4.0
github.com/gofrs/uuid v4.x
```

## SurrealDB Version

Tested against SurrealDB v2.x / v3.x (features like `OVERWRITE`, `COMPACT`,
`ASYNC` events require v2.0+ / v3.0+).

---

## What NOT to Do

- **Do NOT use `BEGIN` / `COMMIT`**: SurrealDB transactions are complex and
  not yet supported by this driver.
- **Do NOT pass raw `time.Time` / `time.Duration` to the SDK**: Always use
  `ToSDKValue`.
- **Do NOT hardcode `READONLY` on `created_at` / `id`**: Use `gorm:"readonly"` tag.
- **Do NOT use `models.Schemaless`, `models.Schemafull`, `models.EdgeSchemaless`, or `models.EdgeSchemafull`**: These structs have been removed. Use `models.BaseModel` if you need ID/timestamps, `models.SchemaFull` if you want `SCHEMAFULL`, and compose `models.BaseModel` + `models.Edge[T,U]` for edges.
- **Do NOT use `TABLE` keyword after `ON` in `ALTER FIELD`**: Correct syntax is
  `ALTER FIELD IF EXISTS name ON table ...`, NOT `ON TABLE table`.
- **Do NOT create events that UPDATE the same table without a `WHEN` guard**:
  This causes infinite recursion and "excessive computation depth" errors.

---

Last updated: 2026-05-16
