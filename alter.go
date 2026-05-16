package surrealdb

import (
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// AlterTableOptions describes the clauses available in an ALTER TABLE statement.
// See: https://surrealdb.com/docs/surrealql/statements/alter/table
// ============================================================================
type AlterTableOptions struct {
	// Schema changes the table type. Only one may be true.
	SchemaFull  bool
	SchemaLess  bool
	DropComment bool
	DropChangefeed bool
	Compact     bool
	Changefeed  string   // e.g. "1d" or "1h"
	IncludeOriginal bool // adds INCLUDE ORIGINAL to CHANGEFEED
	Comment     string
	Permissions string   // e.g. "NONE" or "FULL" or a custom expression
}

// AlterTable executes an ALTER TABLE statement against SurrealDB.
func (m Migrator) AlterTable(table string, opts AlterTableOptions) error {
	var parts []string

	if opts.DropComment {
		parts = append(parts, "DROP COMMENT")
	}
	if opts.DropChangefeed {
		parts = append(parts, "DROP CHANGEFEED")
	}
	if opts.Compact {
		parts = append(parts, "COMPACT")
	}
	if opts.SchemaFull {
		parts = append(parts, "SCHEMAFULL")
	}
	if opts.SchemaLess {
		parts = append(parts, "SCHEMALESS")
	}
	if opts.Permissions != "" {
		parts = append(parts, fmt.Sprintf("PERMISSIONS %s", opts.Permissions))
	}
	if opts.Changefeed != "" {
		clause := fmt.Sprintf("CHANGEFEED %s", opts.Changefeed)
		if opts.IncludeOriginal {
			clause += " INCLUDE ORIGINAL"
		}
		parts = append(parts, clause)
	}
	if opts.Comment != "" {
		parts = append(parts, fmt.Sprintf("COMMENT %q", opts.Comment))
	}

	if len(parts) == 0 {
		return fmt.Errorf("no ALTER TABLE clauses provided")
	}

	sql := fmt.Sprintf("ALTER TABLE IF EXISTS `%s` %s", table, strings.Join(parts, " "))
	return m.DB.Exec(sql).Error
}

// ============================================================================
// AlterFieldOptions describes the clauses available in an ALTER FIELD statement.
// See: https://surrealdb.com/docs/surrealql/statements/alter/field
// ============================================================================
type AlterFieldOptions struct {
	// DROP clauses
	DropType      bool
	DropFlexible  bool
	DropReadonly  bool
	DropValue     bool
	DropAssert    bool
	DropDefault   bool
	DropComment   bool
	DropReference bool

	// ADD/SET clauses
	Flexible      bool
	Readonly      bool
	Reference     string   // e.g. "ON DELETE UNSET" or "ON DELETE REJECT"
	Type          string   // SurrealDB type: string, int, datetime, etc.
	Value         string   // VALUE expression
	Assert        string   // ASSERT expression
	Default       string   // DEFAULT expression
	DefaultAlways bool     // DEFAULT ALWAYS @expression
	Comment       string
}

// AlterField executes an ALTER FIELD statement against SurrealDB.
func (m Migrator) AlterField(table, field string, opts AlterFieldOptions) error {
	var parts []string

	if opts.DropType      { parts = append(parts, "DROP TYPE") }
	if opts.DropFlexible  { parts = append(parts, "DROP FLEXIBLE") }
	if opts.DropReadonly  { parts = append(parts, "DROP READONLY") }
	if opts.DropValue     { parts = append(parts, "DROP VALUE") }
	if opts.DropAssert    { parts = append(parts, "DROP ASSERT") }
	if opts.DropDefault   { parts = append(parts, "DROP DEFAULT") }
	if opts.DropComment   { parts = append(parts, "DROP COMMENT") }
	if opts.DropReference { parts = append(parts, "DROP REFERENCE") }

	if opts.Flexible  { parts = append(parts, "FLEXIBLE") }
	if opts.Readonly  { parts = append(parts, "READONLY") }
	if opts.Reference != "" {
		parts = append(parts, fmt.Sprintf("REFERENCE %s", opts.Reference))
	}
	if opts.Type != "" {
		parts = append(parts, fmt.Sprintf("TYPE %s", opts.Type))
	}
	if opts.Value != "" {
		parts = append(parts, fmt.Sprintf("VALUE %s", opts.Value))
	}
	if opts.Assert != "" {
		parts = append(parts, fmt.Sprintf("ASSERT %s", opts.Assert))
	}
	if opts.Default != "" {
		clause := fmt.Sprintf("DEFAULT %s", opts.Default)
		if opts.DefaultAlways {
			clause = fmt.Sprintf("DEFAULT ALWAYS %s", opts.Default)
		}
		parts = append(parts, clause)
	}
	if opts.Comment != "" {
		parts = append(parts, fmt.Sprintf("COMMENT %q", opts.Comment))
	}

	if len(parts) == 0 {
		return fmt.Errorf("no ALTER FIELD clauses provided")
	}

	sql := fmt.Sprintf("ALTER FIELD IF EXISTS `%s` ON `%s` %s", field, table, strings.Join(parts, " "))
	return m.DB.Exec(sql).Error
}

// ============================================================================
// Convenience helpers that combine ALTER TABLE / ALTER FIELD with schema introspection.
// ============================================================================

// MakeFieldReadonly adds the READONLY clause to an existing field.
func (m Migrator) MakeFieldReadonly(table, field string) error {
	return m.AlterField(table, field, AlterFieldOptions{Readonly: true})
}

// MakeFieldMutable removes the READONLY clause from an existing field.
func (m Migrator) MakeFieldMutable(table, field string) error {
	return m.AlterField(table, field, AlterFieldOptions{DropReadonly: true})
}

// ChangeFieldType updates the type of an existing field (uses OVERWRITE semantics via DROP TYPE + TYPE new).
func (m Migrator) ChangeFieldType(table, field, newType string) error {
	return m.AlterField(table, field, AlterFieldOptions{DropType: true, Type: newType})
}

// AddFieldAssert adds an ASSERT clause to an existing field.
func (m Migrator) AddFieldAssert(table, field, expression string) error {
	return m.AlterField(table, field, AlterFieldOptions{Assert: expression})
}

// DropFieldAssert removes the ASSERT clause from an existing field.
func (m Migrator) DropFieldAssert(table, field string) error {
	return m.AlterField(table, field, AlterFieldOptions{DropAssert: true})
}

// AddFieldDefault adds a DEFAULT clause to an existing field.
func (m Migrator) AddFieldDefault(table, field, expression string, always bool) error {
	return m.AlterField(table, field, AlterFieldOptions{Default: expression, DefaultAlways: always})
}

// DropFieldDefault removes the DEFAULT clause from an existing field.
func (m Migrator) DropFieldDefault(table, field string) error {
	return m.AlterField(table, field, AlterFieldOptions{DropDefault: true})
}

// SetFieldComment sets or updates the COMMENT on an existing field.
func (m Migrator) SetFieldComment(table, field, comment string) error {
	return m.AlterField(table, field, AlterFieldOptions{Comment: comment})
}

// DropFieldComment removes the COMMENT from an existing field.
func (m Migrator) DropFieldComment(table, field string) error {
	return m.AlterField(table, field, AlterFieldOptions{DropComment: true})
}

// CompactTable runs storage compaction on a table (SurrealDB v3.0+).
func (m Migrator) CompactTable(table string) error {
	return m.AlterTable(table, AlterTableOptions{Compact: true})
}

// SetTableSchemaFull converts a SCHEMALESS table to SCHEMAFULL.
func (m Migrator) SetTableSchemaFull(table string) error {
	return m.AlterTable(table, AlterTableOptions{SchemaFull: true})
}

// SetTableSchemaLess converts a SCHEMAFULL table to SCHEMALESS.
func (m Migrator) SetTableSchemaLess(table string) error {
	return m.AlterTable(table, AlterTableOptions{SchemaLess: true})
}

// SetTableChangefeed enables a changefeed on a table with the given duration (e.g. "1h", "1d").
// If includeOriginal is true, the changefeed stores reverse diffs (the state before each change).
func (m Migrator) SetTableChangefeed(table, duration string, includeOriginal bool) error {
	return m.AlterTable(table, AlterTableOptions{Changefeed: duration, IncludeOriginal: includeOriginal})
}

// DropTableChangefeed removes the changefeed from a table.
func (m Migrator) DropTableChangefeed(table string) error {
	return m.AlterTable(table, AlterTableOptions{DropChangefeed: true})
}

// ShowChanges replays changes for a table since a given timestamp or versionstamp.
// since can be a time.Time or an int (versionstamp). If limit > 0, a LIMIT clause is added.
// Usage: db.Raw(ShowChangesSQL("users", time.Now().Add(-time.Hour), 10))
func ShowChangesSQL(table string, since interface{}, limit int) string {
	var sinceStr string
	switch v := since.(type) {
	case time.Time:
		sinceStr = fmt.Sprintf("d'%s'", v.UTC().Format(time.RFC3339Nano))
	case int, int64:
		sinceStr = fmt.Sprintf("%v", v)
	case string:
		sinceStr = v
	default:
		sinceStr = fmt.Sprintf("%v", v)
	}

	sql := fmt.Sprintf("SHOW CHANGES FOR TABLE `%s` SINCE %s", table, sinceStr)
	if limit > 0 {
		sql = fmt.Sprintf("%s LIMIT %d", sql, limit)
	}
	return sql
}

// SetTableComment sets or updates the COMMENT on a table.
func (m Migrator) SetTableComment(table, comment string) error {
	return m.AlterTable(table, AlterTableOptions{Comment: comment})
}

// DropTableComment removes the COMMENT from a table.
func (m Migrator) DropTableComment(table string) error {
	return m.AlterTable(table, AlterTableOptions{DropComment: true})
}

// SetTablePermissions updates the PERMISSIONS clause on a table.
func (m Migrator) SetTablePermissions(table, permissions string) error {
	return m.AlterTable(table, AlterTableOptions{Permissions: permissions})
}
