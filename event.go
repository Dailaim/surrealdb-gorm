package surrealdb

import (
	"fmt"
	"strings"
)

// EventOptions describes the configuration for a SurrealDB event trigger.
// See: https://surrealdb.com/docs/surrealql/statements/define/event
type EventOptions struct {
	Overwrite bool
	When      string // WHEN condition expression
	Then      string // THEN action expression or block
	Async     bool
	Retry     int    // ASYNC RETRY count
	MaxDepth  int    // ASYNC MAXDEPTH
	Comment   string
}

// DefineEvent creates a SurrealDB event trigger on the given table.
func (m Migrator) DefineEvent(table, name string, opts EventOptions) error {
	var parts []string

	if opts.Overwrite {
		parts = append(parts, "OVERWRITE")
	} else {
		parts = append(parts, "IF NOT EXISTS")
	}

	parts = append(parts, fmt.Sprintf("`%s`", name))
	parts = append(parts, "ON")
	parts = append(parts, fmt.Sprintf("`%s`", table))

	if opts.Async {
		asyncParts := []string{"ASYNC"}
		if opts.Retry > 0 {
			asyncParts = append(asyncParts, fmt.Sprintf("RETRY %d", opts.Retry))
		}
		if opts.MaxDepth > 0 {
			asyncParts = append(asyncParts, fmt.Sprintf("MAXDEPTH %d", opts.MaxDepth))
		}
		parts = append(parts, strings.Join(asyncParts, " "))
	}

	if opts.When != "" {
		parts = append(parts, fmt.Sprintf("WHEN %s", opts.When))
	}

	if opts.Then != "" {
		parts = append(parts, fmt.Sprintf("THEN (%s)", opts.Then))
	}

	if opts.Comment != "" {
		parts = append(parts, fmt.Sprintf("COMMENT %q", opts.Comment))
	}

	sql := fmt.Sprintf("DEFINE EVENT %s", strings.Join(parts, " "))
	return m.DB.Exec(sql).Error
}

// RemoveEvent drops an existing event from a table.
func (m Migrator) RemoveEvent(table, name string) error {
	return m.DB.Exec(fmt.Sprintf("REMOVE EVENT IF EXISTS `%s` ON TABLE `%s`", name, table)).Error
}

// ============================================================================
// Convenience helpers for common event patterns.
// ============================================================================

// DefineAuditEvent creates an event that logs changes to an audit table.
// actionDesc is a human-readable description (e.g. "user updated").
func (m Migrator) DefineAuditEvent(table, auditTable, actionDesc string) error {
	then := fmt.Sprintf(
		"CREATE `%s` SET target = $value.id, action = '%s', before = $before, after = $after, at = time::now()",
		auditTable, actionDesc,
	)
	return m.DefineEvent(table, fmt.Sprintf("audit_%s", table), EventOptions{
		Then: then,
	})
}

// DefineEmailChangeEvent creates an event that logs email address changes.
func (m Migrator) DefineEmailChangeEvent(table, logTable string) error {
	when := "$before.email != $after.email"
	then := fmt.Sprintf(
		"CREATE `%s` SET user = $value.id, action = 'email ' + $event.lowercase() + 'd', old_email = $before.email ?? '', new_email = $after.email ?? '', at = time::now()",
		logTable,
	)
	return m.DefineEvent(table, fmt.Sprintf("email_change_%s", table), EventOptions{
		When: when,
		Then: then,
	})
}

// DefineCreatedAtEvent creates an event that sets a timestamp field on CREATE only.
// This avoids the infinite recursion that would occur if we updated the field on UPDATE.
func (m Migrator) DefineCreatedAtEvent(table, field string) error {
	then := fmt.Sprintf("UPDATE $value SET `%s` = time::now()", field)
	return m.DefineEvent(table, fmt.Sprintf("created_at_%s_%s", table, field), EventOptions{
		When: "$event = 'CREATE'",
		Then: then,
	})
}
