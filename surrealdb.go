package surrealdb

import (
	"context"
	"fmt"

	"github.com/surrealdb/surrealdb.go"
	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
)

// Open returns a new SurrealDB dialector for GORM.
func Open(dsn string) gorm.Dialector {
	return &Dialector{DSN: dsn}
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
