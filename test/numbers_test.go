package surrealdb_test

import (
	"context"
	"fmt"
	"testing"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/dailaim/surrealdb-gorm/types"
)

// TestSurrealNumberTypes verifica que SurrealDB maneja correctamente
// int, float y decimal, y que nuestros helpers los convierten sin pérdida.
func TestSurrealNumberTypes(t *testing.T) {
	db := setupDB(t)

	ctx := context.Background()
	conn := getDialector(db).Conn

	// ------------------------------------------------------------------
	// 1. INT
	// ------------------------------------------------------------------
	resInt, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		"CREATE numbers_test SET age = 42",
		nil,
	)
	if err != nil {
		t.Fatalf("create int: %v", err)
	}
	if len(*resInt) == 0 || len((*resInt)[0].Result) == 0 {
		t.Fatal("no result for int")
	}
	rowInt := (*resInt)[0].Result[0]
	ageRaw := rowInt["age"]
	age, err := types.ToInt64(ageRaw)
	if err != nil {
		t.Fatalf("ToInt64(age=%v type=%T): %v", ageRaw, ageRaw, err)
	}
	if age != 42 {
		t.Errorf("age: expected 42, got %d", age)
	}
	if !types.IsSurrealNumber(ageRaw) {
		t.Errorf("age should be reported as SurrealNumber")
	}

	// ------------------------------------------------------------------
	// 2. FLOAT (surrealDB lo marca con sufijo 'f' cuando lo devuelve)
	// ------------------------------------------------------------------
	resFloat, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		"CREATE numbers_test SET temperature = 36.6",
		nil,
	)
	if err != nil {
		t.Fatalf("create float: %v", err)
	}
	rowFloat := (*resFloat)[0].Result[0]
	tempRaw := rowFloat["temperature"]
	temp, err := types.ToFloat64(tempRaw)
	if err != nil {
		t.Fatalf("ToFloat64(temperature=%v type=%T): %v", tempRaw, tempRaw, err)
	}
	if temp < 36.599 || temp > 36.601 {
		t.Errorf("temperature: expected 36.6, got %f", temp)
	}

	// ------------------------------------------------------------------
	// 3. DECIMAL (surrealDB usa suffix 'dec' o string)
	// ------------------------------------------------------------------
	resDec, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		"CREATE numbers_test SET price = 99.99dec",
		nil,
	)
	if err != nil {
		t.Fatalf("create decimal: %v", err)
	}
	rowDec := (*resDec)[0].Result[0]
	priceRaw := rowDec["price"]
	price, err := types.ToFloat64(priceRaw)
	if err != nil {
		t.Fatalf("ToFloat64(price=%v type=%T): %v", priceRaw, priceRaw, err)
	}
	if price != 99.99 {
		t.Errorf("price: expected 99.99, got %f", price)
	}

	// ------------------------------------------------------------------
	// 4. Valores extremos de int64
	// ------------------------------------------------------------------
	resBig, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		fmt.Sprintf("CREATE numbers_test SET big = %d", int64(9007199254740991)),
		nil,
	)
	if err != nil {
		t.Fatalf("create big int: %v", err)
	}
	rowBig := (*resBig)[0].Result[0]
	bigRaw := rowBig["big"]
	big, err := types.ToInt64(bigRaw)
	if err != nil {
		t.Fatalf("ToInt64(big=%v type=%T): %v", bigRaw, bigRaw, err)
	}
	if big != 9007199254740991 {
		t.Errorf("big int: expected 9007199254740991, got %d", big)
	}

	// ------------------------------------------------------------------
	// 5. Edge case: float64 que viene como json.Number
	// ------------------------------------------------------------------
	resJSONNum, err := surrealdb.Query[[]map[string]interface{}](ctx, conn,
		"CREATE numbers_test SET score = 7.5",
		nil,
	)
	if err != nil {
		t.Fatalf("create score: %v", err)
	}
	rowJSON := (*resJSONNum)[0].Result[0]
	scoreRaw := rowJSON["score"]
	score, err := types.ToFloat64(scoreRaw)
	if err != nil {
		t.Fatalf("ToFloat64(score=%v type=%T): %v", scoreRaw, scoreRaw, err)
	}
	if score != 7.5 {
		t.Errorf("score: expected 7.5, got %f", score)
	}

	// ------------------------------------------------------------------
	// 6. Verificar que valores NO numéricos devuelven error
	// ------------------------------------------------------------------
	if _, err := types.ToInt64("hello"); err == nil {
		t.Error("expected error converting 'hello' to int64")
	}
	if _, err := types.ToFloat64(nil); err == nil {
		t.Error("expected error converting nil to float64")
	}
	if types.IsSurrealNumber("not a number") {
		t.Error("expected IsSurrealNumber('not a number') = false")
	}

	// ------------------------------------------------------------------
	// 7. Cleanup
	// ------------------------------------------------------------------
	if err := db.Exec("DELETE FROM numbers_test").Error; err != nil {
		t.Logf("cleanup warning: %v", err)
	}

	t.Logf("[number-types] int=%d float=%f decimal=%f big=%d score=%f — all converted correctly",
		age, temp, price, big, score)
}
