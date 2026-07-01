package surrealdb

import "fmt"

// Error is a structured error returned by the SurrealDB driver. It preserves the
// operation, the SurrealQL that failed, and the status/detail reported by the
// server so callers can inspect failures with errors.As:
//
//	var serr *surrealdb.Error
//	if errors.As(db.Error, &serr) {
//	    log.Printf("query failed (%s): %s\n%s", serr.Status, serr.Detail, serr.Query)
//	}
type Error struct {
	Op     string // logical operation: "query", "exec", "create", "relate", ...
	Query  string // the SurrealQL statement that failed, if available
	Status string // SurrealDB result status, e.g. "ERR"
	Detail string // server-provided detail / message
	Err    error  // underlying transport error, if any
}

func (e *Error) Error() string {
	switch {
	case e.Err != nil:
		if e.Query != "" {
			return fmt.Sprintf("surrealdb %s: %v (query: %s)", e.Op, e.Err, e.Query)
		}
		return fmt.Sprintf("surrealdb %s: %v", e.Op, e.Err)
	case e.Detail != "":
		return fmt.Sprintf("surrealdb %s: %s", e.Op, e.Detail)
	default:
		return fmt.Sprintf("surrealdb %s failed (status %s)", e.Op, e.Status)
	}
}

// Unwrap exposes the underlying transport error for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Err }

// newStatusError builds an *Error for a SurrealDB statement that returned a
// non-OK status.
func newStatusError(op, query, status string, detail interface{}) *Error {
	return &Error{
		Op:     op,
		Query:  query,
		Status: status,
		Detail: fmt.Sprintf("%v", detail),
	}
}
