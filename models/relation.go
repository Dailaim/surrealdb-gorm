package models

import (
	"github.com/dailaim/surrealdb-gorm/types"
)

type RelationSchemafull struct {
	Schemafull
	In  *types.RecordID `json:"in,omitempty"`
	Out *types.RecordID `json:"out,omitempty"`
}

type RelationSchemaless struct {
	Schemaless
	In  *types.RecordID `json:"in,omitempty"`
	Out *types.RecordID `json:"out,omitempty"`
}
