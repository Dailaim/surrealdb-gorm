package models

import (
	"github.com/dailaim/surrealdb-gorm/types"
)

// Model a basic GoLang struct which includes the following fields: ID, CreatedAt, UpdatedAt, DeletedAt
// It may be embedded into your model or you may build your own model without it
//
//	type User struct {
//	  surrealdb.Model
//	}
type Relation struct {
	ID *types.RecordID `gorm:"primaryKey;type:record;<-:create" json:"id,omitempty"`
}

func (m *Relation) GetID() *types.RecordID {
	return m.ID
}
