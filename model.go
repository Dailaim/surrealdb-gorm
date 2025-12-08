package surrealdb

import (
	"time"

	"github.com/dailaim/surrealdb-gorm/types"
)

// Model a basic GoLang struct which includes the following fields: ID, CreatedAt, UpdatedAt, DeletedAt
// It may be embedded into your model or you may build your own model without it
//
//	type User struct {
//	  surrealdb.Model
//	}
type Model struct {
	ID        *types.RecordID  `gorm:"primaryKey;type:record;<-:create" json:"id,omitempty"`
	CreatedAt time.Time        `json:"created_at,omitempty"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
	DeletedAt *types.DeletedAt `gorm:"index;softDelete:true" json:"deleted_at,omitempty"`
}

func (m *Model) GetID() *types.RecordID {
	return m.ID
}
