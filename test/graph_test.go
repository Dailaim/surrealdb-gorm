package surrealdb_test

import (
	"context"
	"testing"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/dailaim/surrealdb-gorm/models"
	"github.com/dailaim/surrealdb-gorm/types"
)

type Buyer struct {
	models.Schemaless
	Name     string
	Wishlist []Wishlist `gorm:"-"`
	Products []Product  `gorm:"many2many:wishlist;"`
}

// this is arc for many to many
type Wishlist struct {
	models.EdgeSchemaless[Buyer, Product]
	Name string
	Year int
}

type Product struct {
	models.Schemaless
	Name     string
	Wishlist []Wishlist `gorm:"-"`
	Buyers   []Buyer    `gorm:"many2many:wishlist;"`
}

func TestGraphManyToMany(t *testing.T) {
	db := setupDB(t)

	db.AutoMigrate(&Buyer{}, &Product{})
	db.AutoMigrate(&Wishlist{})

	buyer := Buyer{Name: "Alice"}
	product := Product{Name: "Product 1"}

	if err := db.Create(&buyer).Error; err != nil {
		t.Fatalf("Failed to create buyer: %v", err)
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("Failed to create product: %v", err)
	}

	// --- 1. Manual edge creation (with extra fields) ---
	wishlist := Wishlist{
		EdgeSchemaless: models.EdgeSchemaless[Buyer, Product]{
			Edge: models.Edge[Buyer, Product]{
				In:  &types.Link[Buyer]{ID: buyer.ID},
				Out: &types.Link[Product]{ID: product.ID},
			},
		},
		Name: "Alice's Wishlist",
		Year: 2024,
	}
	if err := db.Create(&wishlist).Error; err != nil {
		t.Fatalf("Failed to create wishlist edge: %v", err)
	}
	if wishlist.ID == nil {
		t.Fatal("Wishlist edge ID not populated after create")
	}
	t.Logf("[manual] created edge: %v  (in=%v -> out=%v)", wishlist.ID, buyer.ID, product.ID)

	// --- 2. Association.Append (GORM many2many API) ---
	buyer2 := Buyer{Name: "Bob"}
	product2 := Product{Name: "Product 2"}
	if err := db.Create(&buyer2).Error; err != nil {
		t.Fatalf("Failed to create buyer2: %v", err)
	}
	if err := db.Create(&product2).Error; err != nil {
		t.Fatalf("Failed to create product2: %v", err)
	}
	if err := db.Model(&buyer2).Association("Products").Append(&product2); err != nil {
		t.Fatalf("Association.Append failed: %v", err)
	}
	if err := db.Where("in = ?", buyer2.ID).Find(&buyer2.Wishlist).Error; err != nil {
		t.Fatalf("GORM query of buyer2 wishlists failed: %v", err)
	}
	if len(buyer2.Wishlist) == 0 {
		t.Error("expected at least 1 wishlist edge for buyer2, got 0")
	}
	t.Logf("[association] buyer2=%v appended product2=%v Wishlist=%v", buyer2.ID, product2.ID, buyer2.Wishlist)

	// --- 3. Preload via graph traversal ---
	var loadedBuyer Buyer
	if err := db.Preload("Products").First(&loadedBuyer, "id = ?", buyer.ID).Error; err != nil {
		t.Fatalf("Preload failed: %v", err)
	}
	if len(loadedBuyer.Products) == 0 {
		t.Error("Expected at least one product via Preload, got 0")
	}
	t.Logf("[preload] buyer loaded %d product(s)", len(loadedBuyer.Products))
}

// TestGraphManualEdgeNative verifica via SDK nativo que un edge creado con db.Create
// realmente existe en SurrealDB con los campos correctos.
func TestGraphManualEdgeNative(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&Buyer{}, &Product{}, &Wishlist{})

	buyer := Buyer{Name: "NativeAlice"}
	product := Product{Name: "NativeProduct"}
	if err := db.Create(&buyer).Error; err != nil {
		t.Fatalf("create buyer: %v", err)
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}

	edge := Wishlist{
		EdgeSchemaless: models.EdgeSchemaless[Buyer, Product]{
			Edge: models.Edge[Buyer, Product]{
				In:  &types.Link[Buyer]{ID: buyer.ID},
				Out: &types.Link[Product]{ID: product.ID},
			},
		},
		Name: "Native Wishlist",
		Year: 2025,
	}
	if err := db.Create(&edge).Error; err != nil {
		t.Fatalf("create edge: %v", err)
	}
	if edge.ID == nil {
		t.Fatal("edge ID not populated after create")
	}

	// Verificación nativa: el edge debe existir con in, out y name correctos.
	ctx := context.Background()
	results, err := surrealdb.Query[[]map[string]interface{}](ctx, getDialector(db).Conn,
		"SELECT * FROM wishlists WHERE id = $id",
		map[string]interface{}{"id": &edge.ID.RecordID},
	)
	if err != nil {
		t.Fatalf("native query: %v", err)
	}
	if len(*results) == 0 || len((*results)[0].Result) == 0 {
		t.Fatal("edge not found in SurrealDB via native query")
	}
	row := (*results)[0].Result[0]
	if row["in"] == nil {
		t.Error("edge 'in' is nil")
	}
	if row["out"] == nil {
		t.Error("edge 'out' is nil")
	}
	if row["name"] != "Native Wishlist" {
		t.Errorf("name: expected 'Native Wishlist', got %v", row["name"])
	}
	t.Logf("[native manual] row: %v", row)
}

// TestGraphAssociationEdgeNative verifica via SDK nativo que Association.Append
// escribe correctamente el edge en SurrealDB.
func TestGraphAssociationEdgeNative(t *testing.T) {
	db := setupDB(t)
	db.AutoMigrate(&Buyer{}, &Product{}, &Wishlist{})

	buyer := Buyer{Name: "NativeBob"}
	product := Product{Name: "NativeProduct2"}
	if err := db.Create(&buyer).Error; err != nil {
		t.Fatalf("create buyer: %v", err)
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}

	if err := db.Model(&buyer).Association("Products").Append(&product); err != nil {
		t.Fatalf("Association.Append: %v", err)
	}

	// Verificación nativa: debe existir un edge wishlist con in=buyer, out=product.
	ctx := context.Background()
	results, err := surrealdb.Query[[]map[string]interface{}](ctx, getDialector(db).Conn,
		"SELECT * FROM wishlists WHERE in = $in AND out = $out",
		map[string]interface{}{
			"in":  &buyer.ID.RecordID,
			"out": &product.ID.RecordID,
		},
	)
	if err != nil {
		t.Fatalf("native query: %v", err)
	}
	if len(*results) == 0 || len((*results)[0].Result) == 0 {
		t.Fatal("association edge not found in SurrealDB via native query")
	}
	row := (*results)[0].Result[0]
	if row["in"] == nil || row["out"] == nil {
		t.Errorf("edge missing in/out: %v", row)
	}
	t.Logf("[native association] row: %v", row)

	// También verificar via GORM
	var wishlist []Wishlist
	if err := db.Where("in = ?", buyer.ID).Find(&wishlist).Error; err != nil {
		t.Fatalf("GORM find wishlists: %v", err)
	}
	if len(wishlist) == 0 {
		t.Error("GORM: expected at least 1 wishlist edge, got 0")
	}
	t.Logf("[gorm association] wishlists: %v", wishlist)
}
