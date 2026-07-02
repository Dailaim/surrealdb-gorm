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
	Name       string     `json:"name"`
	Attachment types.File `json:"attachment"`
}

// TestFileType round-trips a SurrealDB v3 file pointer (CBOR tag 55). It is
// gated because the server must run with the experimental files feature
// (`surreal start --allow-experimental files`); point SURREALDB_DSN at it.
func TestFileType(t *testing.T) {
	if os.Getenv("SURREALDB_FILES_TEST") == "" {
		t.Skip("set SURREALDB_FILES_TEST=1 against a server started with --allow-experimental files")
	}
	db := setupDB(t)
	db.Exec("REMOVE TABLE IF EXISTS file_docs")
	require.NoError(t, db.AutoMigrate(&FileDoc{}))
	t.Cleanup(func() { db.Exec("REMOVE TABLE IF EXISTS file_docs") })

	rec := FileDoc{Name: "doc", Attachment: types.NewFile("mybucket", "report.pdf")}
	require.NoError(t, db.Create(&rec).Error)
	require.NotNil(t, rec.ID)

	var got FileDoc
	require.NoError(t, db.First(&got, "id = ?", rec.ID).Error)
	require.Equal(t, "doc", got.Name)
	require.Equal(t, "mybucket", got.Attachment.Bucket, "bucket should round-trip")
	require.Equal(t, "/report.pdf", got.Attachment.Key, "key should round-trip (normalized with leading /)")
}
