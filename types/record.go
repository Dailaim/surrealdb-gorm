package types

import "github.com/surrealdb/surrealdb.go/pkg/models"

type RecordID struct {
	models.RecordID
}

func (r *RecordID) StringToRecordID(s string) error {
	parsed, err := models.ParseRecordID(s)
	if err != nil {
		return err
	}
	r.RecordID = *parsed
	return nil
}

// Value implements driver.Valuer

func (RecordID) GormDataType() string {
	return "record"
}
