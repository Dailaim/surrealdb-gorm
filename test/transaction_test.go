package surrealdb_test

import (
	"fmt"
	"testing"

	driver "github.com/dailaim/surrealdb-gorm"
	"github.com/dailaim/surrealdb-gorm/models"
	"gorm.io/gorm"
)

// Account is a simple model used only in transaction tests.
type Account struct {
	models.BaseModel
	Owner   string `json:"owner"`
	Balance int    `json:"balance"`
}

func setupAccountDB(t *testing.T) {
	t.Helper()
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate Account: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM accounts")
	})
}

// TestTransactionCommit verifies that two UPDATE statements inside a
// transaction are both applied when there is no error.
func TestTransactionCommit(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	alice := Account{Owner: "alice", Balance: 1000}
	bob := Account{Owner: "bob", Balance: 0}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatalf("seed alice: %v", err)
	}
	if err := db.Create(&bob).Error; err != nil {
		t.Fatalf("seed bob: %v", err)
	}

	err := driver.Transaction(db, func(tx *driver.Tx) error {
		tx.Exec("UPDATE accounts SET balance -= $amt WHERE owner = $owner",
			map[string]interface{}{"amt": 300, "owner": "alice"})
		tx.Exec("UPDATE accounts SET balance += $amt WHERE owner = $owner",
			map[string]interface{}{"amt": 300, "owner": "bob"})
		return tx.Err()
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}

	var a, b Account
	db.Where("owner = ?", "alice").First(&a)
	db.Where("owner = ?", "bob").First(&b)

	if a.Balance != 700 {
		t.Errorf("alice balance: want 700, got %d", a.Balance)
	}
	if b.Balance != 300 {
		t.Errorf("bob balance: want 300, got %d", b.Balance)
	}
}

// TestTransactionCancel verifies that when fn returns an error before calling
// any Exec, no statements are sent to SurrealDB.
func TestTransactionCancel(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	carol := Account{Owner: "carol", Balance: 500}
	if err := db.Create(&carol).Error; err != nil {
		t.Fatalf("seed carol: %v", err)
	}

	err := driver.Transaction(db, func(tx *driver.Tx) error {
		tx.Exec("UPDATE accounts SET balance = 0 WHERE owner = $owner",
			map[string]interface{}{"owner": "carol"})
		// Simulate early error — transaction should NOT be sent.
		return errSimulated
	})
	if err != errSimulated {
		t.Fatalf("expected errSimulated, got: %v", err)
	}

	var c Account
	db.Where("owner = ?", "carol").First(&c)
	if c.Balance != 500 {
		t.Errorf("carol balance should be unchanged (500), got %d", c.Balance)
	}
}

// TestTransactionNoStatements verifies that calling Transaction with an empty
// fn is a no-op (no error).
func TestTransactionNoStatements(t *testing.T) {
	db := setupDB(t)
	err := driver.Transaction(db, func(tx *driver.Tx) error {
		return tx.Err()
	})
	if err != nil {
		t.Errorf("empty transaction should not error, got: %v", err)
	}
}

// TestTransactionParamNamespace verifies that the same param name can be used
// in multiple Exec calls without colliding.
func TestTransactionParamNamespace(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	dave := Account{Owner: "dave", Balance: 200}
	eve := Account{Owner: "eve", Balance: 200}
	if err := db.Create(&dave).Error; err != nil {
		t.Fatalf("seed dave: %v", err)
	}
	if err := db.Create(&eve).Error; err != nil {
		t.Fatalf("seed eve: %v", err)
	}

	// Both use $owner and $val — namespacing must prevent collisions.
	err := driver.Transaction(db, func(tx *driver.Tx) error {
		tx.Exec("UPDATE accounts SET balance = $val WHERE owner = $owner",
			map[string]interface{}{"owner": "dave", "val": 999})
		tx.Exec("UPDATE accounts SET balance = $val WHERE owner = $owner",
			map[string]interface{}{"owner": "eve", "val": 111})
		return tx.Err()
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}

	var d, e Account
	db.Where("owner = ?", "dave").First(&d)
	db.Where("owner = ?", "eve").First(&e)

	if d.Balance != 999 {
		t.Errorf("dave: want 999, got %d", d.Balance)
	}
	if e.Balance != 111 {
		t.Errorf("eve: want 111, got %d", e.Balance)
	}
}

// errSimulated is a sentinel error used in TestTransactionCancel.
var errSimulated = errSentinel("simulated error")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// ============================================================================
// GORM-style transactions (db.Transaction / db.Begin / db.Rollback)
// ============================================================================

// TestGORMTransactionCreate verifies db.Transaction with db.Create inside it:
// the model's ID must be set client-side immediately, and on commit the record
// must exist in DB.
func TestGORMTransactionCreate(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	var created Account
	err := db.Transaction(func(tx *gorm.DB) error {
		a := Account{Owner: "gorm_create", Balance: 42}
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		if a.ID == nil {
			return fmt.Errorf("expected ID to be set inside transaction, got nil")
		}
		created = a
		return nil
	})
	if err != nil {
		t.Fatalf("GORM transaction failed: %v", err)
	}

	// Verify record actually landed in DB after commit
	var found Account
	if err := db.First(&found, created.ID).Error; err != nil {
		t.Fatalf("record not found after commit: %v", err)
	}
	if found.Owner != "gorm_create" {
		t.Errorf("owner: want gorm_create, got %s", found.Owner)
	}
	if found.Balance != 42 {
		t.Errorf("balance: want 42, got %d", found.Balance)
	}
}

// TestGORMTransactionRollback verifies that db.Transaction rolls back when fn
// returns an error — nothing must be persisted.
func TestGORMTransactionRollback(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	frank := Account{Owner: "frank", Balance: 777}
	if err := db.Create(&frank).Error; err != nil {
		t.Fatalf("seed frank: %v", err)
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&frank).Update("balance", 0).Error; err != nil {
			return err
		}
		// Force rollback
		return fmt.Errorf("intentional rollback")
	})
	if err == nil || err.Error() != "intentional rollback" {
		t.Fatalf("expected intentional rollback error, got: %v", err)
	}

	var f Account
	db.First(&f, frank.ID)
	if f.Balance != 777 {
		t.Errorf("frank balance should be unchanged (777) after rollback, got %d", f.Balance)
	}
}

// TestGORMBeginCommit verifies manual db.Begin() / tx.Commit() flow.
func TestGORMBeginCommit(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	grace := Account{Owner: "grace", Balance: 100}
	if err := db.Create(&grace).Error; err != nil {
		t.Fatalf("seed grace: %v", err)
	}
	hank := Account{Owner: "hank", Balance: 0}
	if err := db.Create(&hank).Error; err != nil {
		t.Fatalf("seed hank: %v", err)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("Begin: %v", tx.Error)
	}
	tx.Model(&grace).Update("balance", 50)
	tx.Model(&hank).Update("balance", 50)
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var g, h Account
	db.First(&g, grace.ID)
	db.First(&h, hank.ID)
	if g.Balance != 50 {
		t.Errorf("grace: want 50, got %d", g.Balance)
	}
	if h.Balance != 50 {
		t.Errorf("hank: want 50, got %d", h.Balance)
	}
}

// TestGORMBeginRollback verifies manual db.Begin() / tx.Rollback() flow.
func TestGORMBeginRollback(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	iris := Account{Owner: "iris", Balance: 999}
	if err := db.Create(&iris).Error; err != nil {
		t.Fatalf("seed iris: %v", err)
	}

	tx := db.Begin()
	tx.Model(&iris).Update("balance", 0)
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	var i Account
	db.First(&i, iris.ID)
	if i.Balance != 999 {
		t.Errorf("iris balance should be unchanged (999) after rollback, got %d", i.Balance)
	}
}

// TestGORMReadYourOwnWrites verifies that reads inside a transaction see writes
// made earlier in the same transaction (requires SurrealDB v3+).
func TestGORMReadYourOwnWrites(t *testing.T) {
	db := setupDB(t)
	if err := db.AutoMigrate(&Account{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM accounts") })

	jack := Account{Owner: "jack_ryow", Balance: 100}
	if err := db.Create(&jack).Error; err != nil {
		t.Fatalf("seed jack: %v", err)
	}
	if jack.ID == nil {
		t.Fatal("jack.ID is nil after create")
	}

	var balanceInsideTx int
	err := db.Transaction(func(tx *gorm.DB) error {
		// Write inside tx
		if err := tx.Model(&jack).Update("balance", 42).Error; err != nil {
			return fmt.Errorf("update inside tx: %w", err)
		}
		// Read inside the SAME tx — should see 42, not 100 (read-your-own-writes)
		var a Account
		if err := tx.First(&a, jack.ID).Error; err != nil {
			return fmt.Errorf("select inside tx: %w", err)
		}
		balanceInsideTx = a.Balance
		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}
	if balanceInsideTx != 42 {
		t.Errorf("read-your-own-writes failed: want 42, got %d", balanceInsideTx)
	}

	// Also verify committed value
	var after Account
	db.First(&after, jack.ID)
	if after.Balance != 42 {
		t.Errorf("committed value: want 42, got %d", after.Balance)
	}
}
