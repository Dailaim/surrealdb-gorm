package surrealdb

import "gorm.io/gorm"

func RawCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	executeSQL(db)
}
