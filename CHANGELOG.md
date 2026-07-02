# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.0] - 2026-07-01

### Added

- **Vector indexes** (`HNSW` / `MTREE`) in `AutoMigrate` via the index tag, e.g.
  `gorm:"index:idx_emb,class:HNSW,option:dimension=768 dist=cosine efc=150 m=12"`.
  Enables ANN/kNN similarity search on embedding fields (`<|k,EF|>`).
- **DEFINE helpers** for statements outside GORM's schema model: `DefineParam`,
  `DefineFunction`, `DefineSequence` (v3), `DefineUser` and their `Remove*`
  counterparts, plus generic `Define`/`Remove` escape hatches for
  `ACCESS`/`API`/`BUCKET`/`CONFIG`/`MODEL`.
- **Computed & advanced fields** from struct tags in `AutoMigrate`:
  `gorm:"value:<expr>"` (computed `VALUE`), `gorm:"flexible"` (`FLEXIBLE`
  objects), `gorm:"permissions:<clause>"`, and explicit `gorm:"assert:<expr>"`.
- Verified **literal/enum union types** work through the `type:` tag
  (`gorm:"type:'a'|'b'|'c'"`), enforced by the schema.

### Notes

- `set<T>` is reachable as a schema type but SurrealDB does not auto-coerce a
  plain array to a set on write, so it is not wired for value round-trips.
- The v3 `file` type has no SDK model in surrealdb.go v1.5.0 yet, so it is not
  supported as a runtime type.

## [1.2.0] - 2026-07-01

### Added

- **Auto-reconnecting WebSocket connection** via the SDK's reliable-websocket
  (`rews`) wrapper. On connection loss the driver transparently reconnects and
  replays the recorded SignIn token and `USE` namespace/database, so callers keep
  working across transient drops. Configurable via `Dialector.ReconnectInterval`
  and `Config.ReconnectInterval` (0 = default 5s, negative = disabled).
- `TestReconnectAfterDrop` (gated by `SURREALDB_RECONNECT_TEST`) verifies
  recovery after a transient outage; validated by pausing/resuming the server
  mid-run (queries fail while down, then recover with data intact).

### Notes

- Reconnection recovers **transient** drops where the server stays up and the
  session token remains valid. A full server restart that wipes state (e.g. an
  in-memory instance) or regenerates signing keys is not recoverable — the token
  re-auth fails and the data is gone regardless.
- Still single-connection (no pool). The SDK serializes requests over one
  mutex-protected socket and tags interactive transactions by UUID, so this is
  correct and concurrency-safe, just not a throughput pool. A pool remains future
  work.

## [1.1.0] - 2026-07-01

### Added

- **Real `*sql.Rows`** via a `database/sql/driver` layer (`sqldriver.go`): the
  dialector opens an internal `*sql.DB` and `QueryContext`/`QueryRowContext`
  delegate to it, so `db.Raw(sql).Rows()` and `sql.ScanRows` work. Columns are
  derived from the first document's keys (scalars use a single `value` column).
- **`RowCallback`** replacing the previous no-op `gorm:row`, yielding to the
  edge association-count path for many2many.
- **Migrator interface completeness** — native `HasColumn`, `HasIndex`, and
  `RenameColumn` backed by `INFO FOR TABLE` instead of GORM's
  information_schema defaults (which SurrealDB rejects).

### Fixed

- `RenameField` generated `ALTER TABLE ... RENAME FIELD`, which is not valid
  SurrealQL. It now recreates the field definition under the new name, copies
  the value, and removes the old field.

### Known limitations (tracked for a future release)

- No automatic WebSocket reconnection (the SDK's `contrib/rews` is not wired in).
- The single shared connection serializes requests (correct and concurrency-safe
  via the SDK's mutex + UUID-tagged transactions, but not a throughput pool).
- Raw `.Rows()` inside `db.Transaction(...)` and `db.Model(&x).Rows()` (without
  the SurrealQL rewrites) are not yet supported; `db.Raw(...).Rows()` is.

## [1.0.0] - 2026-07-01

First stable release of the GORM v2 driver for SurrealDB.

### Added

- **Dialector** implementing GORM's driver interface over the SurrealDB Go SDK
  (`surrealdb.go` v1.4.0, CBOR wire protocol).
- **CRUD callbacks** — `Create`, `Update`, `Delete`, `Query` mapped to native
  SurrealQL.
- **Migrator** — `AutoMigrate` with `DEFINE TABLE/FIELD/INDEX`, `OVERWRITE` for
  field changes, obsolete-field cleanup, and `SCHEMAFULL` opt-in via the
  `SchemaFull` interface.
- **Type system** (`types/`) — `RecordID`, `Link[T]`, `SliceLink[T]`, `DateTime`,
  `Decimal`, `UUID`, `Duration`, and geometry types, all CBOR-safe via
  `ToSDKValue`.
- **Graph relations** — `Edge[T,U]` / `EdgeBaseModel[T,U]` models, native
  `RELATE`, and `Relate()` helper.
- **Interactive transactions** (SurrealDB v3+) via `db.Transaction(...)`, plus a
  raw SurrealQL transaction builder `surrealdb.Transaction(...)`.
- **Batch insert** — `CreateMany` using a single native `INSERT`.
- **Live queries** — `NewLiveQuery`, notifications channel, and `Kill`.
- **Full-text search** — analyzer helpers and `SEARCH ANALYZER` indexes.
- **Events** — `DefineEvent` database triggers.
- **Changefeeds** — table changefeed configuration and `SHOW CHANGES`.
- **ALTER helpers** — `DropField`, `RenameField`, `RemoveTableIndex`,
  `RenameTableTo`, `ChangeFieldType`, `MakeFieldReadonly`, and more.
- **Query explain** — `Dialector.ExplainQuery`.

### Fixed

- Nil-pointer panic in `Link.Scan` when scanning a bare record-id string into a
  `Link[T]` whose `ID` had not been allocated.
- Edge writes, bulk inserts, and soft-deletes now participate in an open
  interactive transaction instead of silently escaping to the shared connection.
  Connection-vs-transaction routing is centralized in `execTxQuery` /
  `txFromStatement`.
- `SurrealTx.ExecContext` now applies the same edge-`INSERT` intercept and SQL
  rewrites as the shared-connection path, so many2many appends and updates work
  inside `db.Transaction(...)`.
- Removed a duplicated `DELETE FROM` translation block in the SQL executor.

### Changed

- Upgraded SDK to `surrealdb.go` v1.5.0.
- On connect, `Initialize` now signs in and runs `DEFINE NAMESPACE/DATABASE IF
  NOT EXISTS` before `USE`, so the driver works on SurrealDB v3 (which no longer
  auto-creates them) as well as v2.
- `docker-compose.yml` uses the `nightly` (v3) image; the full test suite passes
  against SurrealDB v3.

### Notes

- The test suite honors the `SURREALDB_DSN` environment variable, falling back
  to a local development instance.
- Full suite: 105 passing, 1 intentional skip (raw `LIVE SELECT` via Scan).

[1.3.0]: https://github.com/Dailaim/surrealdb-gorm/releases/tag/v1.3.0
[1.2.0]: https://github.com/Dailaim/surrealdb-gorm/releases/tag/v1.2.0
[1.1.0]: https://github.com/Dailaim/surrealdb-gorm/releases/tag/v1.1.0
[1.0.0]: https://github.com/Dailaim/surrealdb-gorm/releases/tag/v1.0.0
