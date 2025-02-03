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
