package surrealdb

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
)

// Open returns a new SurrealDB dialector for GORM from a DSN string, e.g.
// "ws://localhost:8000/rpc?namespace=test&database=test&username=root&password=root".
func Open(dsn string) gorm.Dialector {
	return &Dialector{DSN: dsn}
}

// Config describes a SurrealDB connection with explicit fields instead of a DSN
// query string, so credentials aren't embedded in a URL that may leak into logs.
type Config struct {
	// Endpoint is the SurrealDB RPC endpoint, e.g. "ws://localhost:8000/rpc".
	Endpoint string
	// Namespace and Database select the working namespace/database (USE).
	Namespace string
	Database  string
	// Username and Password are the signin credentials.
	Username string
	Password string
	// ReconnectInterval tunes the auto-reconnecting WebSocket connection: 0 uses
	// the default (5s), a positive value sets the reconnect check interval, and a
	// negative value disables reconnection.
	ReconnectInterval time.Duration
}

// New returns a GORM dialector from an explicit Config. Prefer this over Open
// when you don't want credentials embedded in a DSN string.
//
//	db, err := gorm.Open(surrealdb.New(surrealdb.Config{
//	    Endpoint:  "ws://localhost:8000/rpc",
//	    Namespace: "test",
//	    Database:  "test",
//	    Username:  "root",
//	    Password:  "root",
//	}), &gorm.Config{})
func New(cfg Config) gorm.Dialector {
	return &Dialector{DSN: cfg.dsn(), ReconnectInterval: cfg.ReconnectInterval}
}

// dsn assembles the DSN string that Dialector.Initialize expects, URL-encoding
// the credentials. Namespace/database/username/password are carried as query
// parameters, matching the format accepted by Open.
func (c Config) dsn() string {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme == "" {
		// Best-effort fallback: Initialize will surface a clear parse/connect error.
		u = &url.URL{Path: c.Endpoint}
	}
	q := u.Query()
	q.Set("namespace", c.Namespace)
	q.Set("database", c.Database)
	q.Set("username", c.Username)
	q.Set("password", c.Password)
	u.RawQuery = q.Encode()
	return u.String()
}

// Relate creates a graph relationship between two records using SurrealDB's
// native RELATE statement.
//
// Example:
//
//	db := gorm.Open(surrealdb.Open(dsn), &gorm.Config{})
//	dialector := db.Dialector.(*surrealdb.Dialector)
//	result, err := surrealdb.Relate(ctx, dialector,
//		"users:alice", "follows", "users:bob",
//		map[string]interface{}{"since": "2024-01-01"})
func Relate(ctx context.Context, d *Dialector, in, relation, out string, data ...map[string]interface{}) (map[string]interface{}, error) {
	if d.Conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}
	inRID, err := sdkModels.ParseRecordID(in)
	if err != nil {
		return nil, fmt.Errorf("invalid in record id %q: %w", in, err)
	}
	outRID, err := sdkModels.ParseRecordID(out)
	if err != nil {
		return nil, fmt.Errorf("invalid out record id %q: %w", out, err)
	}
	var payload map[string]interface{}
	if len(data) > 0 {
		payload = data[0]
	}
	rel := &surrealdb.Relationship{
		In:       *inRID,
		Out:      *outRID,
		Relation: sdkModels.Table(relation),
		Data:     payload,
	}
	res, err := surrealdb.Relate[map[string]interface{}](ctx, d.Conn, rel)
	if err != nil {
		return nil, err
	}
	return *res, nil
}
