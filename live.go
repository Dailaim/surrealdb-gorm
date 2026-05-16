package surrealdb

import (
	"fmt"

	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/connection"
	"github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
)

// getSurrealDB extracts the underlying *surrealdb.DB from a GORM instance.
func getSurrealDB(db *gorm.DB) (*surrealdb.DB, error) {
	dialector, ok := db.Dialector.(*Dialector)
	if !ok {
		return nil, fmt.Errorf("db is not using surrealdb dialector")
	}
	if dialector.Conn == nil {
		return nil, fmt.Errorf("surrealdb connection is nil")
	}
	return dialector.Conn, nil
}

// LiveSelect starts a live query on the given table and returns the live query UUID.
// If diff is true, notifications will contain only the changed fields (diff mode).
func LiveSelect(db *gorm.DB, table string, diff bool) (*string, error) {
	sdb, err := getSurrealDB(db)
	if err != nil {
		return nil, err
	}
	uuid, err := surrealdb.Live(db.Statement.Context, sdb, models.Table(table), diff)
	if err != nil {
		return nil, err
	}
	if uuid == nil {
		return nil, fmt.Errorf("live query returned nil UUID")
	}
	s := uuid.String()
	return &s, nil
}

// LiveNotifications returns a channel that receives real-time change notifications
// for the given live query ID. The channel must be closed with CloseLiveNotifications.
func LiveNotifications(db *gorm.DB, liveQueryID string) (<-chan connection.Notification, error) {
	sdb, err := getSurrealDB(db)
	if err != nil {
		return nil, err
	}
	ch, err := sdb.LiveNotifications(liveQueryID)
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// CloseLiveNotifications closes the notification channel for a live query.
func CloseLiveNotifications(db *gorm.DB, liveQueryID string) error {
	sdb, err := getSurrealDB(db)
	if err != nil {
		return err
	}
	return sdb.CloseLiveNotifications(liveQueryID)
}

// KillLiveQuery terminates a live query and closes its notification channel.
func KillLiveQuery(db *gorm.DB, liveQueryID string) error {
	sdb, err := getSurrealDB(db)
	if err != nil {
		return err
	}
	return surrealdb.Kill(db.Statement.Context, sdb, liveQueryID)
}

// LiveQuery is a convenience wrapper for managing a single live query subscription.
type LiveQuery struct {
	DB     *gorm.DB
	ID     string
	Diff   bool
	Table  string
}

// NewLiveQuery creates a new live query subscription.
func NewLiveQuery(db *gorm.DB, table string, diff bool) (*LiveQuery, error) {
	id, err := LiveSelect(db, table, diff)
	if err != nil {
		return nil, err
	}
	return &LiveQuery{DB: db, ID: *id, Diff: diff, Table: table}, nil
}

// Notifications returns the notification channel for this live query.
func (l *LiveQuery) Notifications() (<-chan connection.Notification, error) {
	return LiveNotifications(l.DB, l.ID)
}

// Kill terminates this live query.
func (l *LiveQuery) Kill() error {
	return KillLiveQuery(l.DB, l.ID)
}

// Close closes the notification channel without killing the query.
func (l *LiveQuery) Close() error {
	return CloseLiveNotifications(l.DB, l.ID)
}
