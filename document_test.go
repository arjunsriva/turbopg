package turbopg

import (
	"context"
	"fmt"
	"testing"
)

func TestUpsert(t *testing.T) {
	// Setup test database and initialize
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	if err := Initialize(ctx, db.DB); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	// Create store with prefix
	prefix := "test"
	store, err := New(db.DB, Config{
		Prefix: prefix,
		DBURL:  db.DatabaseURL(t),
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create test namespace
	namespace := "vectors"
	dimensions := 3
	err = store.CreateNamespace(ctx, namespace, CreateNamespaceOptions{
		Dimensions: dimensions,
	})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	tests := []struct {
		name    string
		docs    []Document
		opts    UpsertOptions
		wantErr bool
	}{
		{
			name: "single document",
			docs: []Document{
				{
					ID:     "doc1",
					Vector: []float32{1, 2, 3},
					Attributes: map[string]interface{}{
						"text": "hello world",
					},
				},
			},
			opts: UpsertOptions{
				Namespace: namespace,
			},
			wantErr: false,
		},
		{
			name: "multiple documents",
			docs: []Document{
				{
					ID:     "doc2",
					Vector: []float32{4, 5, 6},
					Attributes: map[string]interface{}{
						"text": "hello again",
					},
				},
				{
					ID:     "doc3",
					Vector: []float32{7, 8, 9},
					Attributes: map[string]interface{}{
						"text": "goodbye",
					},
				},
			},
			opts: UpsertOptions{
				Namespace: namespace,
			},
			wantErr: false,
		},
		{
			name: "update existing document",
			docs: []Document{
				{
					ID:     "doc1",
					Vector: []float32{10, 11, 12},
					Attributes: map[string]interface{}{
						"text": "updated",
					},
				},
			},
			opts: UpsertOptions{
				Namespace: namespace,
			},
			wantErr: false,
		},
		{
			name: "invalid vector dimensions",
			docs: []Document{
				{
					ID:     "doc4",
					Vector: []float32{1, 2}, // Wrong dimensions
					Attributes: map[string]interface{}{
						"text": "bad vector",
					},
				},
			},
			opts: UpsertOptions{
				Namespace: namespace,
			},
			wantErr: true,
		},
		{
			name: "empty document ID",
			docs: []Document{
				{
					ID:     "",
					Vector: []float32{1, 2, 3},
					Attributes: map[string]interface{}{
						"text": "no id",
					},
				},
			},
			opts: UpsertOptions{
				Namespace: namespace,
			},
			wantErr: true,
		},
		{
			name: "non-existent namespace",
			docs: []Document{
				{
					ID:     "doc5",
					Vector: []float32{1, 2, 3},
					Attributes: map[string]interface{}{
						"text": "bad namespace",
					},
				},
			},
			opts: UpsertOptions{
				Namespace: "not_exists",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Upsert(ctx, tt.docs, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("Upsert() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Verify documents were inserted/updated
			tableName := GetNamespaceTableName(prefix, namespace)
			for _, doc := range tt.docs {
				var exists bool
				query := `
					SELECT EXISTS (
						SELECT FROM ` + tableName + `
						WHERE id = $1
					)`
				err = db.QueryRowContext(ctx, query, doc.ID).Scan(&exists)
				if err != nil {
					t.Fatalf("failed to check if document exists: %v", err)
				}
				if !exists {
					t.Errorf("document %q was not inserted", doc.ID)
				}
			}
		})
	}
}

func TestUpsertBatch(t *testing.T) {
	// Setup test database and initialize
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	if err := Initialize(ctx, db.DB); err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	// Create store with prefix
	prefix := "test"
	store, err := New(db.DB, Config{
		Prefix: prefix,
		DBURL:  db.DatabaseURL(t),
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create test namespace
	namespace := "vectors"
	dimensions := 3
	err = store.CreateNamespace(ctx, namespace, CreateNamespaceOptions{
		Dimensions: dimensions,
	})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// Helper to create test documents
	createDocs := func(count int) []Document {
		docs := make([]Document, count)
		for i := range docs {
			docs[i] = Document{
				ID:     DocumentID(fmt.Sprintf("doc%d", i+1)),
				Vector: []float32{float32(i + 1), float32(i + 2), float32(i + 3)},
				Attributes: map[string]interface{}{
					"index": i,
					"text":  fmt.Sprintf("document %d", i+1),
				},
			}
		}
		return docs
	}

	tests := []struct {
		name    string
		docs    []Document
		opts    BatchUpsertOptions
		wantErr bool
	}{
		{
			name: "empty batch",
			docs: []Document{},
			opts: BatchUpsertOptions{
				UpsertOptions: UpsertOptions{
					Namespace: namespace,
				},
			},
			wantErr: false,
		},
		{
			name: "single batch",
			docs: createDocs(10),
			opts: BatchUpsertOptions{
				UpsertOptions: UpsertOptions{
					Namespace: namespace,
				},
				BatchSize: 5,
			},
			wantErr: false,
		},
		{
			name: "multiple batches",
			docs: createDocs(25),
			opts: BatchUpsertOptions{
				UpsertOptions: UpsertOptions{
					Namespace: namespace,
				},
				BatchSize: 10,
			},
			wantErr: false,
		},
		{
			name: "default batch size",
			docs: createDocs(15),
			opts: BatchUpsertOptions{
				UpsertOptions: UpsertOptions{
					Namespace: namespace,
				},
			},
			wantErr: false,
		},
		{
			name: "with validation",
			docs: append(createDocs(5), Document{
				ID:     "bad_vector",
				Vector: []float32{1, 2}, // Wrong dimensions
			}),
			opts: BatchUpsertOptions{
				UpsertOptions: UpsertOptions{
					Namespace: namespace,
				},
			},
			wantErr: true,
		},
		{
			name: "skip validation",
			docs: createDocs(5),
			opts: BatchUpsertOptions{
				UpsertOptions: UpsertOptions{
					Namespace:      namespace,
					SkipValidation: true,
				},
			},
			wantErr: false,
		},
		{
			name: "non-existent namespace",
			docs: createDocs(5),
			opts: BatchUpsertOptions{
				UpsertOptions: UpsertOptions{
					Namespace: "not_exists",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.UpsertBatch(ctx, tt.docs, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpsertBatch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Verify documents were inserted/updated
			tableName := GetNamespaceTableName(prefix, namespace)
			for _, doc := range tt.docs {
				var exists bool
				query := `
					SELECT EXISTS (
						SELECT FROM ` + tableName + `
						WHERE id = $1
					)`
				err = db.QueryRowContext(ctx, query, doc.ID).Scan(&exists)
				if err != nil {
					t.Fatalf("failed to check if document exists: %v", err)
				}
				if !exists {
					t.Errorf("document %q was not inserted", doc.ID)
				}
			}
		})
	}
}

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
		filter    Filter
		wantErr   bool
	}{
		{
			name:      "delete by exact match",
			namespace: ns,
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "electronics",
			},
			wantErr: false,
		},
		{
			name:      "delete by comparison",
			namespace: ns,
			filter: Filter{
				Field: "price",
				Op:    ">",
				Value: 50,
			},
			wantErr: false,
		},
		{
			name:      "delete by boolean",
			namespace: ns,
			filter: Filter{
				Field: "inStock",
				Op:    "=",
				Value: false,
			},
			wantErr: false,
		},
		{
			name:      "delete with no matches",
			namespace: ns,
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "clothing",
			},
			wantErr: false,
		},
		{
			name:      "delete from non-existent namespace",
			namespace: "nonexistent",
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "electronics",
			},
			wantErr: true,
		},
		{
			name:      "invalid namespace name",
			namespace: "Invalid Name",
			filter: Filter{
				Field: "category",
				Op:    "=",
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
