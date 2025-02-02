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

func TestSearchVector(t *testing.T) {
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
	ns := "test_search"
	err = store.CreateNamespace(ctx, ns, CreateNamespaceOptions{
		Dimensions: 4,
		IndexConfig: &IndexConfig{
			DistanceMetric: "cosine_distance",
			Lists:          100,
		},
	})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// Insert test documents
	docs := []Document{
		{
			ID:     "doc1",
			Vector: []float32{1, 0, 0, 0}, // Base vector
			Attributes: map[string]interface{}{
				"name": "document 1",
			},
		},
		{
			ID:     "doc2",
			Vector: []float32{0.9, 0.1, 0, 0}, // Similar to doc1
			Attributes: map[string]interface{}{
				"name": "document 2",
			},
		},
		{
			ID:     "doc3",
			Vector: []float32{0, 1, 0, 0}, // Different direction
			Attributes: map[string]interface{}{
				"name": "document 3",
			},
		},
		{
			ID:     "doc4",
			Vector: []float32{0, 0, 1, 0}, // Orthogonal
			Attributes: map[string]interface{}{
				"name": "document 4",
			},
		},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatalf("failed to insert test documents: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		vector    []float32
		topK      int
		metric    string
		wantIDs   []DocumentID // Expected document IDs in order
		wantErr   bool
	}{
		{
			name:      "cosine similarity search",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			topK:      2,
			metric:    "cosine",
			wantIDs:   []DocumentID{"doc1", "doc2"}, // Most similar to query vector
			wantErr:   false,
		},
		{
			name:      "euclidean distance search",
			namespace: ns,
			vector:    []float32{0, 1, 0, 0},
			topK:      2,
			metric:    "euclidean",
			wantIDs:   []DocumentID{"doc3", "doc2"}, // Closest in Euclidean space
			wantErr:   false,
		},
		{
			name:      "euclidean squared distance search",
			namespace: ns,
			vector:    []float32{0, 0, 1, 0},
			topK:      1,
			metric:    "euclidean_squared",
			wantIDs:   []DocumentID{"doc4"}, // Closest in Euclidean squared space
			wantErr:   false,
		},
		{
			name:      "wrong dimensions",
			namespace: ns,
			vector:    []float32{1, 2, 3}, // Only 3 dimensions
			topK:      1,
			metric:    "cosine",
			wantErr:   true,
		},
		{
			name:      "invalid topK",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			topK:      0,
			metric:    "cosine",
			wantErr:   true,
		},
		{
			name:      "non-existent namespace",
			namespace: "nonexistent",
			vector:    []float32{1, 0, 0, 0},
			topK:      1,
			metric:    "cosine",
			wantErr:   true,
		},
		{
			name:      "invalid metric",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			topK:      1,
			metric:    "invalid",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.SearchVector(ctx, tt.namespace, tt.vector, tt.topK, tt.metric)
			if (err != nil) != tt.wantErr {
				t.Errorf("SearchVector() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			// Check number of results
			if len(results) != len(tt.wantIDs) {
				t.Errorf("SearchVector() got %d results, want %d", len(results), len(tt.wantIDs))
				return
			}

			// Check result order
			for i, want := range tt.wantIDs {
				if results[i].Document.ID != want {
					t.Errorf("SearchVector() result %d got ID %s, want %s", i, results[i].Document.ID, want)
				}
			}

			// Verify scores are in correct order (ascending for distance metrics)
			for i := 1; i < len(results); i++ {
				if results[i].Score < results[i-1].Score {
					t.Errorf("SearchVector() scores not in ascending order: %v before %v", results[i-1].Score, results[i].Score)
				}
			}
		})
	}
}

func TestSearchFiltered(t *testing.T) {
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
	ns := "test_search_filtered"
	err = store.CreateNamespace(ctx, ns, CreateNamespaceOptions{
		Dimensions: 4,
		IndexConfig: &IndexConfig{
			DistanceMetric: "cosine_distance",
			Lists:          100,
		},
	})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// Insert test documents with various attributes
	docs := []Document{
		{
			ID:     "doc1",
			Vector: []float32{1, 0, 0, 0},
			Attributes: map[string]interface{}{
				"category": "electronics",
				"price":    100,
				"inStock":  true,
			},
		},
		{
			ID:     "doc2",
			Vector: []float32{0.9, 0.1, 0, 0},
			Attributes: map[string]interface{}{
				"category": "electronics",
				"price":    200,
				"inStock":  false,
			},
		},
		{
			ID:     "doc3",
			Vector: []float32{0, 1, 0, 0},
			Attributes: map[string]interface{}{
				"category": "books",
				"price":    20,
				"inStock":  true,
			},
		},
		{
			ID:     "doc4",
			Vector: []float32{0, 0, 1, 0},
			Attributes: map[string]interface{}{
				"category": "books",
				"price":    15,
				"inStock":  true,
			},
		},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatalf("failed to insert test documents: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		vector    []float32
		filter    Filter
		topK      int
		metric    string
		wantIDs   []DocumentID // Expected document IDs in order
		wantErr   bool
	}{
		{
			name:      "filter by category with cosine",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "electronics",
			},
			topK:    2,
			metric:  "cosine",
			wantIDs: []DocumentID{"doc1", "doc2"}, // Electronics items most similar to query
			wantErr: false,
		},
		{
			name:      "filter by price range with euclidean",
			namespace: ns,
			vector:    []float32{0, 1, 0, 0},
			filter: Filter{
				Field: "price",
				Op:    "<",
				Value: 50,
			},
			topK:    2,
			metric:  "euclidean",
			wantIDs: []DocumentID{"doc3", "doc4"}, // Cheap items closest to query
			wantErr: false,
		},
		{
			name:      "filter by boolean with euclidean squared",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: Filter{
				Field: "inStock",
				Op:    "=",
				Value: true,
			},
			topK:    3,
			metric:  "euclidean_squared",
			wantIDs: []DocumentID{"doc1", "doc3", "doc4"}, // In-stock items
			wantErr: false,
		},
		{
			name:      "no matches",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "clothing", // Non-existent category
			},
			topK:    1,
			metric:  "cosine",
			wantIDs: []DocumentID{}, // No results expected
			wantErr: false,
		},
		{
			name:      "wrong dimensions",
			namespace: ns,
			vector:    []float32{1, 2, 3}, // Only 3 dimensions
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "electronics",
			},
			topK:    1,
			metric:  "cosine",
			wantErr: true,
		},
		{
			name:      "invalid topK",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "electronics",
			},
			topK:    0,
			metric:  "cosine",
			wantErr: true,
		},
		{
			name:      "non-existent namespace",
			namespace: "nonexistent",
			vector:    []float32{1, 0, 0, 0},
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "electronics",
			},
			topK:    1,
			metric:  "cosine",
			wantErr: true,
		},
		{
			name:      "invalid metric",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: Filter{
				Field: "category",
				Op:    "=",
				Value: "electronics",
			},
			topK:    1,
			metric:  "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.SearchFiltered(ctx, tt.namespace, tt.vector, tt.filter, tt.topK, tt.metric)
			if (err != nil) != tt.wantErr {
				t.Errorf("SearchFiltered() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			// Check number of results
			if len(results) != len(tt.wantIDs) {
				t.Errorf("SearchFiltered() got %d results, want %d", len(results), len(tt.wantIDs))
				return
			}

			// Check result order
			for i, want := range tt.wantIDs {
				if results[i].Document.ID != want {
					t.Errorf("SearchFiltered() result %d got ID %s, want %s", i, results[i].Document.ID, want)
				}
			}

			// Verify scores are in correct order (ascending for distance metrics)
			for i := 1; i < len(results); i++ {
				if results[i].Score < results[i-1].Score {
					t.Errorf("SearchFiltered() scores not in ascending order: %v before %v", results[i-1].Score, results[i].Score)
				}
			}

			// Verify all results match the filter
			for _, result := range results {
				value := result.Document.Attributes[tt.filter.Field]
				switch tt.filter.Op {
				case "=":
					if value != tt.filter.Value {
						t.Errorf("SearchFiltered() result has wrong value for field %s: got %v, want %v", tt.filter.Field, value, tt.filter.Value)
					}
				case "<":
					numValue, ok := value.(float64)
					if !ok {
						t.Errorf("SearchFiltered() value is not a number: %v", value)
						continue
					}
					filterValue, ok := tt.filter.Value.(int)
					if !ok {
						t.Errorf("SearchFiltered() filter value is not a number: %v", tt.filter.Value)
						continue
					}
					if numValue >= float64(filterValue) {
						t.Errorf("SearchFiltered() result value %v is not less than %v", numValue, filterValue)
					}
				}
			}
		})
	}
}
