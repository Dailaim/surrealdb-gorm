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

type EdgeBaseModel[T any, U any] struct {
	Edge[T, U]
	CreatedAt time.Time        `json:"created_at,omitempty"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
	DeletedAt *types.DeletedAt `gorm:"index;softDelete:true" json:"deleted_at,omitempty"`
}

// NewEdgeBaseModel creates an EdgeBaseModel with In/Out already wired and
// timestamps initialised to the current time.
func NewEdgeBaseModel[T any, U any](inID, outID *types.RecordID) EdgeBaseModel[T, U] {
	now := time.Now()
	return EdgeBaseModel[T, U]{
		Edge:      NewEdge[T, U](inID, outID),
		CreatedAt: now,
		UpdatedAt: now,
	}
}
