package surrealdb_test

import (
	"fmt"
	"testing"
)

func TestPanic(t *testing.T) {
	model := &OffsetModel{}
	id := model.GetID()
	if id == nil {
		fmt.Println("ID is nil!")
	} else {
		fmt.Printf("ID is %v\n", id.String())
	}
}
