package turbopg

import (
	"testing"
)

func TestSearchBM25(t *testing.T) {
	store, db, ctx := SetupTestStore(t, "test_bm25", false)
	defer db.cleanup(t)

	ok, err := store.HasTextSearch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pg_textsearch to be available")
	}

	ns := "fts"
	if err := store.CreateNamespace(ctx, ns, CreateNamespaceOptions{Dimensions: 0}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.UpdateSchema(ctx, ns, map[string]interface{}{
		"content": map[string]interface{}{
			"type":             "string",
			"full_text_search": true,
		},
	}); err != nil {
		t.Fatalf("schema: %v", err)
	}

	docs := []Document{
		{ID: "1", Attributes: map[string]interface{}{"content": "the quick brown fox jumps"}},
		{ID: "2", Attributes: map[string]interface{}{"content": "lazy walrus sleeping on ice"}},
		{ID: "3", Attributes: map[string]interface{}{"content": "unrelated vegetables and fruit"}},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	results, err := store.Query(ctx, QueryOptions{
		Namespace: ns,
		TopK:      2,
		Rank:      RankSpec{Kind: RankBM25, Field: "content", Query: "walrus"},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected BM25 hits")
	}
	if results[0].Document.ID != "2" {
		t.Fatalf("top hit=%s want 2", results[0].Document.ID)
	}
	if !results[0].HasScore || results[0].Score <= 0 {
		t.Fatalf("expected positive BM25 $dist, got %+v", results[0])
	}

	ordered, err := store.Query(ctx, QueryOptions{
		Namespace: ns,
		TopK:      3,
		Rank:      RankSpec{Kind: RankAttribute, Field: "id", Desc: false},
	})
	if err != nil {
		t.Fatalf("attr rank: %v", err)
	}
	if len(ordered) != 3 || ordered[0].Document.ID != "1" {
		t.Fatalf("attr order=%v", ordered)
	}
	if ordered[0].HasScore {
		t.Fatal("attribute ranking should omit score")
	}
}

func TestSearchHybridNamespace(t *testing.T) {
	store, db, ctx := SetupTestStore(t, "test_hyb", false)
	defer db.cleanup(t)

	ns := "hybrid"
	if err := store.CreateNamespace(ctx, ns, CreateNamespaceOptions{Dimensions: 3}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSchema(ctx, ns, map[string]interface{}{
		"content": map[string]interface{}{"type": "string", "full_text_search": true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(ctx, []Document{
		{ID: "a", Vector: []float32{1, 0, 0}, Attributes: map[string]interface{}{"content": "alpha walrus"}},
		{ID: "b", Vector: []float32{0, 1, 0}, Attributes: map[string]interface{}{"content": "beta fox"}},
	}, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatal(err)
	}

	bm25, err := store.Query(ctx, QueryOptions{
		Namespace: ns,
		TopK:      1,
		Rank:      RankSpec{Kind: RankBM25, Field: "content", Query: "walrus"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bm25) != 1 || bm25[0].Document.ID != "a" {
		t.Fatalf("bm25=%v", bm25)
	}

	ann, err := store.Query(ctx, QueryOptions{
		Namespace: ns,
		TopK:      1,
		Vector:    []float32{0, 1, 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ann) != 1 || ann[0].Document.ID != "b" {
		t.Fatalf("ann=%v", ann)
	}
}
