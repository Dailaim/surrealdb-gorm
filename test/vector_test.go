package surrealdb_test

import (
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/stretchr/testify/require"
)

type VecDoc struct {
	models.BaseModel
	Title     string    `json:"title"`
	Embedding []float64 `json:"embedding" gorm:"type:array<float>;index:idx_vec_emb,class:HNSW,option:dimension=4 dist=euclidean efc=150 m=12"`
}

// TestVectorIndex verifies AutoMigrate creates an HNSW vector index from the
// struct tag and that a kNN similarity query works against it.
func TestVectorIndex(t *testing.T) {
	db := setupDB(t)
	db.Exec("REMOVE TABLE IF EXISTS vec_docs")
	require.NoError(t, db.AutoMigrate(&VecDoc{}))
	t.Cleanup(func() { db.Exec("REMOVE TABLE IF EXISTS vec_docs") })

	// The HNSW index must exist after migration.
	require.True(t, db.Migrator().HasIndex(&VecDoc{}, "idx_vec_emb"))

	// Seed a few vectors.
	require.NoError(t, db.Create(&VecDoc{Title: "a", Embedding: []float64{1, 2, 3, 4}}).Error)
	require.NoError(t, db.Create(&VecDoc{Title: "b", Embedding: []float64{2, 3, 4, 5}}).Error)
	require.NoError(t, db.Create(&VecDoc{Title: "c", Embedding: []float64{9, 9, 9, 9}}).Error)

	// kNN search using the HNSW index operator <|K,EF|> (v3 syntax).
	var titles []string
	err := db.Raw("SELECT title FROM vec_docs WHERE embedding <|2,100|> [1,2,3,4]").Scan(&titles).Error
	require.NoError(t, err)
	require.NotEmpty(t, titles, "expected nearest-neighbor results")
	t.Logf("kNN results: %v", titles)
}
