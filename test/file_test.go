package surrealdb_test

import (
	"os"
	"testing"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/dailaim/surrealdb-gorm/types"
	"github.com/stretchr/testify/require"
)

type FileDoc struct {
	models.BaseModel
	Name       string      `json:"name"`
	Attachment types.File  `json:"attachment"`
	Thumbnail  *types.File `json:"thumbnail"`
}

// TestFileType round-trips SurrealDB v3 file pointers (CBOR tag 55) through the
// full driver: create, read, update, and an optional (pointer) field.
//
// Gated: the server must run with the experimental files feature
// (`surreal start --allow-experimental files`); point SURREALDB_DSN at it and
// set SURREALDB_FILES_TEST=1.
func TestFileType(t *testing.T) {
	if os.Getenv("SURREALDB_FILES_TEST") == "" {
		t.Skip("set SURREALDB_FILES_TEST=1 against a server started with --allow-experimental files")
	}
	db := setupDB(t)
	db.Exec("REMOVE TABLE IF EXISTS file_docs")
	require.NoError(t, db.AutoMigrate(&FileDoc{}))
	t.Cleanup(func() { db.Exec("REMOVE TABLE IF EXISTS file_docs") })

	thumb := types.NewFile("thumbs", "small.png")
	rec := FileDoc{
		Name:       "doc",
		Attachment: types.NewFile("mybucket", "report.pdf"),
		Thumbnail:  &thumb,
	}
	require.NoError(t, db.Create(&rec).Error)
	require.NotNil(t, rec.ID)

	// Read back: value field, pointer field, key normalization.
	var got FileDoc
	require.NoError(t, db.First(&got, "id = ?", rec.ID).Error)
	require.Equal(t, "mybucket", got.Attachment.Bucket)
	require.Equal(t, "/report.pdf", got.Attachment.Key)
	require.NotNil(t, got.Thumbnail)
	require.Equal(t, "thumbs", got.Thumbnail.Bucket)
	require.Equal(t, "/small.png", got.Thumbnail.Key)

	// Update the file field to a different pointer.
	require.NoError(t, db.Model(&got).Update("attachment", types.NewFile("mybucket", "report-v2.pdf")).Error)
	var updated FileDoc
	require.NoError(t, db.First(&updated, "id = ?", rec.ID).Error)
	require.Equal(t, "/report-v2.pdf", updated.Attachment.Key)
}
