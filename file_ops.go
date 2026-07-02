package surrealdb

import (
	"context"
	"fmt"
	"time"

	"github.com/surrealdb/surrealdb.go"
	"gorm.io/gorm"

	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

// FileInfo is a file listing/metadata entry returned by ListFiles / FileHead.
type FileInfo struct {
	File    TypesM.File `json:"file"`
	Size    int64       `json:"size"`
	Updated time.Time   `json:"updated"`
}

// File content operations (SurrealDB v3 experimental files feature).
//
// A types.File value is only a pointer (bucket + key); these helpers move the
// actual bytes in and out of the bucket via the file:: functions. The bucket
// must exist first — see Migrator.DefineBucket.

func fileConn(db *gorm.DB) (*Dialector, context.Context, error) {
	d, ok := db.Dialector.(*Dialector)
	if !ok || d.Conn == nil {
		return nil, nil, fmt.Errorf("surrealdb: connection not initialized")
	}
	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}
	return d, ctx, nil
}

// PutFile stores content at the file pointer f. The file's bucket must be
// defined (see DefineBucket).
func PutFile(db *gorm.DB, f TypesM.File, content []byte) error {
	d, ctx, err := fileConn(db)
	if err != nil {
		return err
	}
	res, err := surrealdb.Query[interface{}](ctx, d.Conn, "file::put($f, $c)",
		map[string]interface{}{"f": f, "c": content})
	if err != nil {
		return &Error{Op: "file::put", Err: err}
	}
	if len(*res) > 0 && (*res)[0].Status != "OK" {
		return newStatusError("file::put", "", (*res)[0].Status, (*res)[0].Result)
	}
	return nil
}

// GetFile retrieves the content stored at the file pointer f.
func GetFile(db *gorm.DB, f TypesM.File) ([]byte, error) {
	d, ctx, err := fileConn(db)
	if err != nil {
		return nil, err
	}
	res, err := surrealdb.Query[[]byte](ctx, d.Conn, "RETURN file::get($f)",
		map[string]interface{}{"f": f})
	if err != nil {
		return nil, &Error{Op: "file::get", Err: err}
	}
	if len(*res) == 0 {
		return nil, nil
	}
	if (*res)[0].Status != "OK" {
		return nil, newStatusError("file::get", "", (*res)[0].Status, (*res)[0].Result)
	}
	return (*res)[0].Result, nil
}

// FileExists reports whether the file pointer f has stored content.
func FileExists(db *gorm.DB, f TypesM.File) (bool, error) {
	d, ctx, err := fileConn(db)
	if err != nil {
		return false, err
	}
	res, err := surrealdb.Query[bool](ctx, d.Conn, "RETURN file::exists($f)",
		map[string]interface{}{"f": f})
	if err != nil {
		return false, &Error{Op: "file::exists", Err: err}
	}
	if len(*res) == 0 {
		return false, nil
	}
	if (*res)[0].Status != "OK" {
		return false, newStatusError("file::exists", "", (*res)[0].Status, (*res)[0].Result)
	}
	return (*res)[0].Result, nil
}

// DeleteFile removes the content stored at the file pointer f.
func DeleteFile(db *gorm.DB, f TypesM.File) error {
	d, ctx, err := fileConn(db)
	if err != nil {
		return err
	}
	res, err := surrealdb.Query[interface{}](ctx, d.Conn, "file::delete($f)",
		map[string]interface{}{"f": f})
	if err != nil {
		return &Error{Op: "file::delete", Err: err}
	}
	if len(*res) > 0 && (*res)[0].Status != "OK" {
		return newStatusError("file::delete", "", (*res)[0].Status, (*res)[0].Result)
	}
	return nil
}

// CopyFile copies the content of f to targetKey within the same bucket.
func CopyFile(db *gorm.DB, f TypesM.File, targetKey string) error {
	d, ctx, err := fileConn(db)
	if err != nil {
		return err
	}
	res, err := surrealdb.Query[interface{}](ctx, d.Conn, "$f.copy($k)",
		map[string]interface{}{"f": f, "k": targetKey})
	if err != nil {
		return &Error{Op: "file::copy", Err: err}
	}
	if len(*res) > 0 && (*res)[0].Status != "OK" {
		return newStatusError("file::copy", "", (*res)[0].Status, (*res)[0].Result)
	}
	return nil
}

// RenameFile renames f to targetKey within the same bucket.
func RenameFile(db *gorm.DB, f TypesM.File, targetKey string) error {
	d, ctx, err := fileConn(db)
	if err != nil {
		return err
	}
	res, err := surrealdb.Query[interface{}](ctx, d.Conn, "$f.rename($k)",
		map[string]interface{}{"f": f, "k": targetKey})
	if err != nil {
		return &Error{Op: "file::rename", Err: err}
	}
	if len(*res) > 0 && (*res)[0].Status != "OK" {
		return newStatusError("file::rename", "", (*res)[0].Status, (*res)[0].Result)
	}
	return nil
}

// FileHead returns metadata (size, updated) for the file pointer f.
func FileHead(db *gorm.DB, f TypesM.File) (*FileInfo, error) {
	d, ctx, err := fileConn(db)
	if err != nil {
		return nil, err
	}
	res, err := surrealdb.Query[FileInfo](ctx, d.Conn, "RETURN file::head($f)",
		map[string]interface{}{"f": f})
	if err != nil {
		return nil, &Error{Op: "file::head", Err: err}
	}
	if len(*res) == 0 {
		return nil, nil
	}
	if (*res)[0].Status != "OK" {
		return nil, newStatusError("file::head", "", (*res)[0].Status, (*res)[0].Result)
	}
	info := (*res)[0].Result
	return &info, nil
}

// ListFiles lists the files in a bucket. opts is an optional SurrealQL options
// object, e.g. map[string]any{"prefix": "/img/", "limit": 100}.
func ListFiles(db *gorm.DB, bucket string, opts map[string]interface{}) ([]FileInfo, error) {
	d, ctx, err := fileConn(db)
	if err != nil {
		return nil, err
	}
	query := "RETURN file::list($b)"
	params := map[string]interface{}{"b": bucket}
	if opts != nil {
		query = "RETURN file::list($b, $o)"
		params["o"] = opts
	}
	res, err := surrealdb.Query[[]FileInfo](ctx, d.Conn, query, params)
	if err != nil {
		return nil, &Error{Op: "file::list", Err: err}
	}
	if len(*res) == 0 {
		return nil, nil
	}
	if (*res)[0].Status != "OK" {
		return nil, newStatusError("file::list", "", (*res)[0].Status, (*res)[0].Result)
	}
	return (*res)[0].Result, nil
}
