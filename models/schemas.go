package models

// SchemaFull is implemented by models that want their table created as
// SCHEMAFULL in SurrealDB. By default every table is SCHEMALESS.
type SchemaFull interface {
	SchemaFull() bool
}
