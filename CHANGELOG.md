# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

### Notes

- The test suite honors the `SURREALDB_DSN` environment variable, falling back
  to a local development instance.

[1.0.0]: https://github.com/Dailaim/surrealdb-gorm/releases/tag/v1.0.0
