package surrealdb

import (
	"gorm.io/gorm"
)

// Open returns a new SurrealDB dialector for GORM.
func Open(dsn string) gorm.Dialector {
	return &Dialector{DSN: dsn}
}
