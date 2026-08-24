package turbopg

import (
	"testing"
)

func TestPatchAndCopy(t *testing.T) {
	store, db, ctx := SetupTestStore(t, "patchcopy_", false)
	defer db.cleanup(t)

	src := "src"
	if err := store.CreateNamespace(ctx, src, CreateNamespaceOptions{Dimensions: 3}); err != nil {
		t.Fatal(err)
	}
	docs := []Document{
		{ID: "1", Vector: []float32{1, 0, 0}, Attributes: map[string]interface{}{"name": "a", "price": 10}},
		{ID: "2", Vector: []float32{0, 1, 0}, Attributes: map[string]interface{}{"name": "b", "price": 20}},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: src}); err != nil {
		t.Fatal(err)
	}

	n, err := store.Patch(ctx, []Document{{ID: "1", Attributes: map[string]interface{}{"name": "aa"}}}, UpsertOptions{Namespace: src})
	if err != nil || n != 1 {
		t.Fatalf("patch n=%d err=%v", n, err)
	}

	n, err = store.PatchByFilter(ctx, src, FilterCondition{Field: "name", Op: FilterOpEq, Value: "aa"}, map[string]interface{}{"name": "patched"}, nil)
	if err != nil || n != 1 {
		t.Fatalf("patch_by_filter n=%d err=%v", n, err)
	}

	sum, err := store.SumAttribute(ctx, src, "price", nil)
	if err != nil || sum != 30 {
		t.Fatalf("sum=%v err=%v", sum, err)
	}

	copied, err := store.CopyNamespace(ctx, "dest", src)
	if err != nil || copied != 2 {
		t.Fatalf("copy n=%d err=%v", copied, err)
	}

	schema := map[string]interface{}{"name": map[string]interface{}{"type": "string"}}
	if err := store.UpdateSchema(ctx, src, schema); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSchema(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["vector"]; !ok {
		t.Fatalf("schema=%v", got)
	}

	_, err = store.EnsureNamespace(ctx, src, CreateNamespaceOptions{Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := store.GetNamespaceStats(ctx, src)
	if err != nil || stats.ApproximateCount != 2 {
		t.Fatalf("stats=%v err=%v", stats, err)
	}

	deleted, err := store.DeleteByFilter(ctx, src, LogicalFilter{
		Op: LogicalOpAnd,
		Filters: []Filter{
			FilterCondition{Field: "name", Op: FilterOpEq, Value: "patched"},
		},
	})
	if err != nil || deleted != 1 {
		t.Fatalf("delete n=%d err=%v", deleted, err)
	}
}
