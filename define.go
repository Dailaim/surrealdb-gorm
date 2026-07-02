package surrealdb

import (
	"fmt"
	"strings"
)

// This file adds Migrator helpers for SurrealDB DEFINE statements that are not
// part of GORM's schema model: PARAM, FUNCTION, SEQUENCE, USER, plus generic
// Define/Remove escape hatches for the remaining statements (ACCESS, API,
// BUCKET, CONFIG, MODEL, ...).
//
// Values embedded in these statements are raw SurrealQL (DDL is not
// parameterized), so callers are responsible for trusting/escaping inputs.

// Define executes an arbitrary DEFINE statement. Use it for statements without a
// dedicated helper, e.g. DEFINE ACCESS / API / BUCKET / CONFIG / MODEL.
//
//	m.Define("ACCESS user ON DATABASE TYPE RECORD SIGNIN (...) DURATION FOR SESSION 1h")
func (m Migrator) Define(statement string) error {
	return m.DB.Exec("DEFINE " + statement).Error
}

// Remove executes an arbitrary REMOVE statement (the inverse of Define).
//
//	m.Remove("ACCESS user ON DATABASE")
func (m Migrator) Remove(statement string) error {
	return m.DB.Exec("REMOVE " + statement).Error
}

// DefineParam defines a global (database-wide) parameter. The name is given
// without the leading `$`; valueExpr is a raw SurrealQL expression.
//
//	m.DefineParam("endpointBase", `"https://example.com"`)
func (m Migrator) DefineParam(name, valueExpr string) error {
	name = strings.TrimPrefix(name, "$")
	return m.DB.Exec(fmt.Sprintf("DEFINE PARAM IF NOT EXISTS $%s VALUE %s", name, valueExpr)).Error
}

// RemoveParam removes a global parameter (name without the leading `$`).
func (m Migrator) RemoveParam(name string) error {
	name = strings.TrimPrefix(name, "$")
	return m.DB.Exec(fmt.Sprintf("REMOVE PARAM IF EXISTS $%s", name)).Error
}

// DefineFunction defines a custom SurrealQL function. name is the bare function
// name (without the `fn::` prefix), args is the parenthesized-less argument list
// (e.g. `$name: string`), and body is the function body (statements).
//
//	m.DefineFunction("greet", "$name: string", "RETURN 'Hello ' + $name;")
func (m Migrator) DefineFunction(name, args, body string) error {
	name = strings.TrimPrefix(name, "fn::")
	return m.DB.Exec(fmt.Sprintf("DEFINE FUNCTION IF NOT EXISTS fn::%s(%s) { %s }", name, args, body)).Error
}

// RemoveFunction removes a custom function (name without the `fn::` prefix).
func (m Migrator) RemoveFunction(name string) error {
	name = strings.TrimPrefix(name, "fn::")
	return m.DB.Exec(fmt.Sprintf("REMOVE FUNCTION IF EXISTS fn::%s", name)).Error
}

// DefineBucket defines a storage bucket for the file type (SurrealDB v3+,
// experimental files feature). backend may be "memory", "file:/path", or an
// object-store URL; empty uses the server default.
//
//	m.DefineBucket("assets", "memory")
func (m Migrator) DefineBucket(name, backend string) error {
	sql := fmt.Sprintf("DEFINE BUCKET IF NOT EXISTS %s", name)
	if backend != "" {
		sql += fmt.Sprintf(" BACKEND %q", backend)
	}
	return m.DB.Exec(sql).Error
}

// RemoveBucket removes a storage bucket.
func (m Migrator) RemoveBucket(name string) error {
	return m.DB.Exec(fmt.Sprintf("REMOVE BUCKET IF EXISTS %s", name)).Error
}

// SequenceOptions configures a DEFINE SEQUENCE statement (SurrealDB v3+).
type SequenceOptions struct {
	Batch   int    // optional; 0 omits BATCH
	Start   int    // optional; 0 omits START
	Timeout string // optional duration, e.g. "5s"; empty omits TIMEOUT
}

// DefineSequence defines a distributed monotonically-increasing sequence.
//
//	m.DefineSequence("mySeq", surrealdb.SequenceOptions{Batch: 1000, Start: 100})
func (m Migrator) DefineSequence(name string, opts SequenceOptions) error {
	parts := []string{fmt.Sprintf("DEFINE SEQUENCE IF NOT EXISTS %s", name)}
	if opts.Batch > 0 {
		parts = append(parts, fmt.Sprintf("BATCH %d", opts.Batch))
	}
	if opts.Start > 0 {
		parts = append(parts, fmt.Sprintf("START %d", opts.Start))
	}
	if opts.Timeout != "" {
		parts = append(parts, "TIMEOUT "+opts.Timeout)
	}
	return m.DB.Exec(strings.Join(parts, " ")).Error
}

// RemoveSequence removes a sequence.
func (m Migrator) RemoveSequence(name string) error {
	return m.DB.Exec(fmt.Sprintf("REMOVE SEQUENCE IF EXISTS %s", name)).Error
}

// UserOptions configures a DEFINE USER statement.
type UserOptions struct {
	Name     string // required
	Level    string // ROOT | NAMESPACE | DATABASE (default ROOT)
	Password string // required (plaintext; SurrealDB hashes it)
	Roles    string // e.g. "OWNER", "EDITOR", "VIEWER"
	Duration string // optional, e.g. "FOR SESSION 15m, FOR TOKEN 5s"
}

// DefineUser defines a system user at the given level.
//
//	m.DefineUser(surrealdb.UserOptions{Name: "bot", Level: "ROOT", Password: "s3cret", Roles: "VIEWER"})
func (m Migrator) DefineUser(opts UserOptions) error {
	level := opts.Level
	if level == "" {
		level = "ROOT"
	}
	sql := fmt.Sprintf("DEFINE USER IF NOT EXISTS %s ON %s PASSWORD '%s'",
		opts.Name, level, strings.ReplaceAll(opts.Password, "'", "\\'"))
	if opts.Roles != "" {
		sql += " ROLES " + opts.Roles
	}
	if opts.Duration != "" {
		sql += " DURATION " + opts.Duration
	}
	return m.DB.Exec(sql).Error
}

// RemoveUser removes a system user at the given level (default ROOT).
func (m Migrator) RemoveUser(name, level string) error {
	if level == "" {
		level = "ROOT"
	}
	return m.DB.Exec(fmt.Sprintf("REMOVE USER IF EXISTS %s ON %s", name, level)).Error
}
