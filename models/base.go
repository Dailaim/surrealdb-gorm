package models

import (
	"time"

	"github.com/dailaim/surrealdb-gorm/types"
)

type BaseModel struct {
	ID        *types.RecordID  `gorm:"primaryKey;type:record;<-:create" json:"id,omitempty"`
	CreatedAt time.Time        `json:"created_at,omitempty"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
	DeletedAt *types.DeletedAt `gorm:"index;softDelete:true" json:"deleted_at,omitempty"`
}

func (m *BaseModel) GetID() *types.RecordID {
	return m.ID
}
