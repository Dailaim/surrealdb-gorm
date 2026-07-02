package surrealdb_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindByIDList(t *testing.T) {
	db := setupDB(t)
	u1 := User{Name: "FBL1"}
	u2 := User{Name: "FBL2"}
	u3 := User{Name: "FBL3"}
	require.NoError(t, db.Create(&u1).Error)
	require.NoError(t, db.Create(&u2).Error)
	require.NoError(t, db.Create(&u3).Error)

	var users []User
	require.NoError(t, db.Find(&users, []interface{}{u1.ID, u2.ID}).Error)
	require.Len(t, users, 2, "multi-id Find must return exactly the requested records")
	got := map[string]bool{}
	for _, u := range users {
		got[u.Name] = true
	}
	require.True(t, got["FBL1"] && got["FBL2"])
	require.False(t, got["FBL3"], "must not return unrequested records")
}
