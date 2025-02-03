package turbopg

import (
	"context"
	"testing"
)

func TestDelete(t *testing.T) {
	// Setup test database
	db := setupTestDB(t)

	// Initialize database
	ctx := context.Background()
	if err := Initialize(ctx, db.DB); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	// Create store with prefix
	store, err := New(db.DB, Config{
		Prefix: "test",
		DBURL:  db.DatabaseURL(t),
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create test namespace
	ns := "test_delete"
	err = store.CreateNamespace(ctx, ns, CreateNamespaceOptions{
		Dimensions: 4,
	})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// Insert test documents
	docs := []Document{
		{ID: "doc1", Vector: []float32{1, 2, 3, 4}},
		{ID: "doc2", Vector: []float32{5, 6, 7, 8}},
		{ID: "doc3", Vector: []float32{9, 10, 11, 12}},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatalf("failed to insert test documents: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		ids       []DocumentID
		wantErr   bool
	}{
		{
			name:      "delete single document",
			namespace: ns,
			ids:       []DocumentID{"doc1"},
			wantErr:   false,
		},
		{
			name:      "delete multiple documents",
			namespace: ns,
			ids:       []DocumentID{"doc2", "doc3"},
			wantErr:   false,
		},
		{
			name:      "delete non-existent documents",
			namespace: ns,
			ids:       []DocumentID{"doc4", "doc5"},
			wantErr:   false, // Should not error, just delete 0 rows
		},
		{
			name:      "delete from non-existent namespace",
			namespace: "nonexistent",
			ids:       []DocumentID{"doc1"},
			wantErr:   true,
		},
		{
			name:      "delete empty ID list",
			namespace: ns,
			ids:       []DocumentID{},
			wantErr:   false,
		},
		{
			name:      "invalid namespace name",
			namespace: "Invalid Name",
			ids:       []DocumentID{"doc1"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Delete(ctx, tt.namespace, tt.ids)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteByFilter(t *testing.T) {
	// Setup test database
	db := setupTestDB(t)

	// Initialize database
	ctx := context.Background()
	if err := Initialize(ctx, db.DB); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	// Create store with prefix
	store, err := New(db.DB, Config{
		Prefix: "test",
		DBURL:  db.DatabaseURL(t),
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create test namespace
	ns := "test_delete_filter"
	err = store.CreateNamespace(ctx, ns, CreateNamespaceOptions{
		Dimensions: 4,
	})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// Insert test documents with attributes
	docs := []Document{
		{
			ID:     "doc1",
			Vector: []float32{1, 2, 3, 4},
			Attributes: map[string]interface{}{
				"category": "electronics",
				"price":    100,
				"inStock":  true,
			},
		},
		{
			ID:     "doc2",
			Vector: []float32{5, 6, 7, 8},
			Attributes: map[string]interface{}{
				"category": "books",
				"price":    20,
				"inStock":  true,
			},
		},
		{
			ID:     "doc3",
			Vector: []float32{9, 10, 11, 12},
			Attributes: map[string]interface{}{
				"category": "electronics",
				"price":    200,
				"inStock":  false,
			},
		},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatalf("failed to insert test documents: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		filter    FilterCondition
		wantErr   bool
	}{
		{
			name:      "delete by exact match",
			namespace: ns,
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "electronics",
			},
			wantErr: false,
		},
		{
			name:      "delete by comparison",
			namespace: ns,
			filter: FilterCondition{
				Field: "price",
				Op:    FilterOpGt,
				Value: 50,
			},
			wantErr: false,
		},
		{
			name:      "delete by boolean",
			namespace: ns,
			filter: FilterCondition{
				Field: "inStock",
				Op:    FilterOpEq,
				Value: false,
			},
			wantErr: false,
		},
		{
			name:      "delete with no matches",
			namespace: ns,
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "clothing",
			},
			wantErr: false,
		},
		{
			name:      "delete from non-existent namespace",
			namespace: "nonexistent",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "electronics",
			},
			wantErr: true,
		},
		{
			name:      "invalid namespace name",
			namespace: "Invalid Name",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "electronics",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.DeleteByFilter(ctx, tt.namespace, tt.filter)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteByFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
