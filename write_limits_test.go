package turbopg

import "testing"

func TestWriteFilterCaps(t *testing.T) {
	_, db, ctx := SetupTestStore(t, "wcap_", false)
	defer db.cleanup(t)

	store, err := New(db.DB, Config{
		Prefix:            "wcap_",
		DBURL:             db.DatabaseURL(t),
		PatchByFilterMax:  1,
		DeleteByFilterMax: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ns := "capped"
	if err := store.CreateNamespace(ctx, ns, CreateNamespaceOptions{Dimensions: 2}); err != nil {
		t.Fatal(err)
	}
	docs := []Document{
		{ID: "1", Vector: []float32{1, 0}, Attributes: map[string]interface{}{"g": "a"}},
		{ID: "2", Vector: []float32{0, 1}, Attributes: map[string]interface{}{"g": "a"}},
		{ID: "3", Vector: []float32{1, 1}, Attributes: map[string]interface{}{"g": "a"}},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Write(ctx, Write{
		Namespace: ns,
		PatchByFilter: &PatchByFilterWrite{
			Filter:     FilterCondition{Field: "g", Op: FilterOpEq, Value: "a"},
			Attributes: map[string]interface{}{"g": "b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Patched != 1 || got.RowsRemaining != 2 {
		t.Fatalf("patched=%d remaining=%d", got.Patched, got.RowsRemaining)
	}
}

func TestTypedAttributes(t *testing.T) {
	store, db, ctx := SetupTestStore(t, "typed_", false)
	defer db.cleanup(t)
	ns := "t"
	if err := store.CreateNamespace(ctx, ns, CreateNamespaceOptions{Dimensions: 2}); err != nil {
		t.Fatal(err)
	}
	schema := map[string]interface{}{
		"uid":  map[string]interface{}{"type": "uuid"},
		"when": map[string]interface{}{"type": "datetime"},
	}
	if err := store.UpdateSchema(ctx, ns, schema); err != nil {
		t.Fatal(err)
	}
	err := store.Upsert(ctx, []Document{{
		ID:     "1",
		Vector: []float32{1, 0},
		Attributes: map[string]interface{}{
			"uid":  "not-a-uuid",
			"when": "2020-01-02T03:04:05Z",
		},
	}}, UpsertOptions{Namespace: ns})
	if err == nil {
		t.Fatal("expected uuid error")
	}
	err = store.Upsert(ctx, []Document{{
		ID:     "1",
		Vector: []float32{1, 0},
		Attributes: map[string]interface{}{
			"uid":  "550e8400-e29b-41d4-a716-446655440000",
			"when": "2020-01-02T03:04:05Z",
		},
	}}, UpsertOptions{Namespace: ns})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteRejectsDollarAttributes(t *testing.T) {
	store, db, ctx := SetupTestStore(t, "dol_", false)
	defer db.cleanup(t)
	ns := "t"
	if err := store.CreateNamespace(ctx, ns, CreateNamespaceOptions{Dimensions: 2}); err != nil {
		t.Fatal(err)
	}
	err := store.Upsert(ctx, []Document{{
		ID:         "1",
		Vector:     []float32{1, 0},
		Attributes: map[string]interface{}{"$dist": 1},
	}}, UpsertOptions{Namespace: ns})
	if err == nil {
		t.Fatal("expected $ attribute error")
	}
}
