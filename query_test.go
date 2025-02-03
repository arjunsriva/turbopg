package turbopg

import (
	"context"
	"math"
	"testing"
)



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
		filter    FilterCondition
		topK      int
		metric    string
		wantIDs   []DocumentID
		wantErr   bool
	}{
		{
			name:      "filter by category with cosine",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "electronics",
			},
			topK:    2,
			metric:  "cosine",
			wantIDs: []DocumentID{"doc1", "doc2"},
			wantErr: false,
		},
		{
			name:      "filter by price range with euclidean",
			namespace: ns,
			vector:    []float32{0, 1, 0, 0},
			filter: FilterCondition{
				Field: "price",
				Op:    FilterOpLt,
				Value: 50,
			},
			topK:    2,
			metric:  "euclidean",
			wantIDs: []DocumentID{"doc3", "doc4"},
			wantErr: false,
		},
		{
			name:      "filter by boolean with euclidean squared",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: FilterCondition{
				Field: "inStock",
				Op:    FilterOpEq,
				Value: true,
			},
			topK:    3,
			metric:  "euclidean_squared",
			wantIDs: []DocumentID{"doc1", "doc3", "doc4"},
			wantErr: false,
		},
		{
			name:      "no matches",
			namespace: ns,
			vector:    []float32{1, 0, 0, 0},
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "clothing",
			},
			topK:    1,
			metric:  "cosine",
			wantIDs: []DocumentID{},
			wantErr: false,
		},
		{
			name:      "wrong dimensions",
			namespace: ns,
			vector:    []float32{1, 2, 3},
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
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
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
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
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
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
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
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
				case FilterOpEq:
					if value != tt.filter.Value {
						t.Errorf("SearchFiltered() result has wrong value for field %s: got %v, want %v", tt.filter.Field, value, tt.filter.Value)
					}
				case FilterOpLt:
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

func TestQuery(t *testing.T) {
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
		Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create test namespace
	ns := "test_query"
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
				"tags":     []string{"phone", "mobile"},
				"brand":    "apple",
			},
		},
		{
			ID:     "doc2",
			Vector: []float32{0.9, 0.1, 0, 0},
			Attributes: map[string]interface{}{
				"category": "electronics",
				"price":    200,
				"inStock":  false,
				"tags":     []string{"laptop", "computer"},
				"brand":    "dell",
			},
		},
		{
			ID:     "doc3",
			Vector: []float32{0, 1, 0, 0},
			Attributes: map[string]interface{}{
				"category": "books",
				"price":    20,
				"inStock":  true,
				"tags":     []string{"fiction", "novel"},
				"author":   "john doe",
			},
		},
		{
			ID:     "doc4",
			Vector: []float32{0, 0, 1, 0},
			Attributes: map[string]interface{}{
				"category": "books",
				"price":    15,
				"inStock":  true,
				"tags":     []string{"non-fiction", "science"},
				"author":   "jane smith",
			},
		},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatalf("failed to insert test documents: %v", err)
	}

	tests := []struct {
		name    string
		opts    QueryOptions
		wantIDs []DocumentID
		wantErr bool
	}{
		{
			name: "simple equality filter",
			opts: QueryOptions{
				Namespace: ns,
				Filter: FilterCondition{
					Field: "category",
					Op:    FilterOpEq,
					Value: "electronics",
				},
				TopK: 10,
			},
			wantIDs: []DocumentID{"doc1", "doc2"},
			wantErr: false,
		},
		{
			name: "numeric comparison with vector search",
			opts: QueryOptions{
				Namespace: ns,
				Vector:    []float32{1, 0, 0, 0},
				Filter: FilterCondition{
					Field: "price",
					Op:    FilterOpLt,
					Value: 50,
				},
				TopK:   2,
				Metric: "cosine",
			},
			wantIDs: []DocumentID{},  // Empty since no docs match both criteria
			wantErr: false,
		},
		{
			name: "complex AND filter",
			opts: QueryOptions{
				Namespace: ns,
				Filter: LogicalFilter{
					Op: LogicalOpAnd,
					Filters: []Filter{
						FilterCondition{
							Field: "category",
							Op:    FilterOpEq,
							Value: "electronics",
						},
						FilterCondition{
							Field: "price",
							Op:    FilterOpGte,
							Value: 150,
						},
						FilterCondition{
							Field: "inStock",
							Op:    FilterOpEq,
							Value: false,
						},
					},
				},
				TopK: 10,
			},
			wantIDs: []DocumentID{"doc2"},
			wantErr: false,
		},
		{
			name: "complex OR filter",
			opts: QueryOptions{
				Namespace: ns,
				Filter: LogicalFilter{
					Op: LogicalOpOr,
					Filters: []Filter{
						FilterCondition{
							Field: "category",
							Op:    FilterOpEq,
							Value: "books",
						},
						FilterCondition{
							Field: "price",
							Op:    FilterOpGt,
							Value: 150,
						},
					},
				},
				TopK: 10,
			},
			wantIDs: []DocumentID{"doc2", "doc3", "doc4"},
			wantErr: false,
		},
		{
			name: "nested logical filters",
			opts: QueryOptions{
				Namespace: ns,
				Filter: LogicalFilter{
					Op: LogicalOpOr,
					Filters: []Filter{
						LogicalFilter{
							Op: LogicalOpAnd,
							Filters: []Filter{
								FilterCondition{
									Field: "category",
									Op:    FilterOpEq,
									Value: "electronics",
								},
								FilterCondition{
									Field: "price",
									Op:    FilterOpLt,
									Value: 150,
								},
							},
						},
						LogicalFilter{
							Op: LogicalOpAnd,
							Filters: []Filter{
								FilterCondition{
									Field: "category",
									Op:    FilterOpEq,
									Value: "books",
								},
								FilterCondition{
									Field: "price",
									Op:    FilterOpLt,
									Value: 20,
								},
							},
						},
					},
				},
				TopK: 10,
			},
			wantIDs: []DocumentID{"doc1", "doc4"},
			wantErr: false,
		},
		{
			name: "vector similarity with price filter",
			opts: QueryOptions{
				Namespace: ns,
				Vector:    []float32{0, 1, 0, 0},  // Exactly matches doc3's vector
				Filter: FilterCondition{
					Field: "price",
					Op:    FilterOpLt,
					Value: 150,
				},
				TopK:   2,
				Metric: "cosine",
			},
			wantIDs: []DocumentID{"doc3"},  // Only expect doc3 which has both price < 150 and exact vector match
			wantErr: false,
		},
		{
			name: "invalid vector dimensions",
			opts: QueryOptions{
				Namespace: ns,
				Vector:    []float32{1, 2, 3}, // Only 3 dimensions
				TopK:      1,
			},
			wantErr: true,
		},
		{
			name: "invalid topK",
			opts: QueryOptions{
				Namespace: ns,
				TopK:      0,
			},
			wantErr: true,
		},
		{
			name: "non-existent namespace",
			opts: QueryOptions{
				Namespace: "nonexistent",
				TopK:      1,
			},
			wantErr: true,
		},
		{
			name: "invalid metric",
			opts: QueryOptions{
				Namespace: ns,
				Vector:    []float32{1, 0, 0, 0},
				TopK:      1,
				Metric:    "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.Query(ctx, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("Query() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			// Check number of results
			if len(results) != len(tt.wantIDs) {
				t.Errorf("Query() got %d results, want %d", len(results), len(tt.wantIDs))
				return
			}

			// Check result IDs
			for i, want := range tt.wantIDs {
				if results[i].Document.ID != want {
					t.Errorf("Query() result %d got ID %s, want %s", i, results[i].Document.ID, want)
				}
			}

			// Verify scores are in correct order if vector search
			if tt.opts.Vector != nil {
				for i := 1; i < len(results); i++ {
					if results[i].Score < results[i-1].Score {
						t.Errorf("Query() scores not in ascending order: %v before %v", results[i-1].Score, results[i].Score)
					}
				}
			}
		})
	}
}



// TestZeroSimilarityHandling tests how pgvector handles vectors with zero similarity
func TestZeroSimilarityHandling(t *testing.T) {
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
		Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Create test namespace
	ns := "test_zero_sim"
	err = store.CreateNamespace(ctx, ns, CreateNamespaceOptions{
		Dimensions: 3,
		IndexConfig: &IndexConfig{
			DistanceMetric: "cosine_distance",
			Lists:          100,
		},
	})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	// Insert test documents with orthogonal vectors
	docs := []Document{
		{
			ID:     "doc1",
			Vector: []float32{1, 0, 0},
			Attributes: map[string]interface{}{
				"name": "doc1",
			},
		},
		{
			ID:     "doc2",
			Vector: []float32{0, 1, 0},
			Attributes: map[string]interface{}{
				"name": "doc2",
			},
		},
		{
			ID:     "doc3",
			Vector: []float32{0, 0, 1},
			Attributes: map[string]interface{}{
				"name": "doc3",
			},
		},
	}
	if err := store.Upsert(ctx, docs, UpsertOptions{Namespace: ns}); err != nil {
		t.Fatalf("failed to insert test documents: %v", err)
	}

	// Test cases
	tests := []struct {
		name          string
		queryVector   []float32
		expectDocs    []DocumentID
		expectScores  []float64
		expectOrdered bool
	}{
		{
			name:          "orthogonal_query_1_0_0",
			queryVector:   []float32{1, 0, 0},
			expectDocs:    []DocumentID{"doc1", "doc2", "doc3"},
			expectScores:  []float64{0, 1, 1}, // 0 for perfect match, 1 for orthogonal
			expectOrdered: true,
		},
		{
			name:          "orthogonal_query_0_1_0",
			queryVector:   []float32{0, 1, 0},
			expectDocs:    []DocumentID{"doc2", "doc1", "doc3"},
			expectScores:  []float64{0, 1, 1},
			expectOrdered: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.Query(ctx, QueryOptions{
				Namespace: ns,
				Vector:    tt.queryVector,
				TopK:      3,
				Metric:    "cosine",
			})
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}

			// Check number of results
			if len(results) != len(tt.expectDocs) {
				t.Errorf("got %d results, want %d", len(results), len(tt.expectDocs))
				return
			}

			// If we expect ordered results, check order and scores
			if tt.expectOrdered {
				for i, want := range tt.expectDocs {
					if results[i].Document.ID != want {
						t.Errorf("result %d: got ID %s, want %s", i, results[i].Document.ID, want)
					}
					if !almostEqual(results[i].Score, tt.expectScores[i], 0.0001) {
						t.Errorf("result %d: got score %f, want %f", i, results[i].Score, tt.expectScores[i])
					}
				}
			}
		})
	}
}

// almostEqual checks if two float64 values are equal within a small epsilon
func almostEqual(a, b float64, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}
