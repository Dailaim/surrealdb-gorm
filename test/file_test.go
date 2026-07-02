package surrealdb_test

import (
	"os"
	"testing"

	surrealdb "github.com/dailaim/surrealdb-gorm"
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

// TestFileContentIO stores and retrieves the actual bytes behind a file pointer
// (the field only holds the pointer; content lives in the bucket).
func TestFileContentIO(t *testing.T) {
	if os.Getenv("SURREALDB_FILES_TEST") == "" {
		t.Skip("set SURREALDB_FILES_TEST=1 against a server started with --allow-experimental files")
	}
	db := setupDB(t)
	m := surrealdb.Migrator{Migrator: db.Migrator().(surrealdb.Migrator).Migrator}
	require.NoError(t, m.DefineBucket("assets", "memory"))
	t.Cleanup(func() { _ = m.RemoveBucket("assets") })

	f := types.NewFile("assets", "greeting.txt")
	content := []byte("hola mundo")

	require.NoError(t, surrealdb.PutFile(db, f, content))

	exists, err := surrealdb.FileExists(db, f)
	require.NoError(t, err)
	require.True(t, exists)

	got, err := surrealdb.GetFile(db, f)
	require.NoError(t, err)
	require.Equal(t, content, got, "file content should round-trip")

	// head + list
	head, err := surrealdb.FileHead(db, f)
	require.NoError(t, err)
	require.Equal(t, int64(len(content)), head.Size)

	listing, err := surrealdb.ListFiles(db, "assets", nil)
	require.NoError(t, err)
	require.Len(t, listing, 1)
	require.Equal(t, "/greeting.txt", listing[0].File.Key)

	// copy + rename
	require.NoError(t, surrealdb.CopyFile(db, f, "greeting-copy.txt"))
	cp := types.NewFile("assets", "greeting-copy.txt")
	exists, err = surrealdb.FileExists(db, cp)
	require.NoError(t, err)
	require.True(t, exists, "copy should exist")

	require.NoError(t, surrealdb.RenameFile(db, cp, "greeting-final.txt"))
	exists, err = surrealdb.FileExists(db, types.NewFile("assets", "greeting-final.txt"))
	require.NoError(t, err)
	require.True(t, exists, "renamed file should exist")

	require.NoError(t, surrealdb.DeleteFile(db, f))
	exists, err = surrealdb.FileExists(db, f)
	require.NoError(t, err)
	require.False(t, exists, "file should be gone after delete")
}
