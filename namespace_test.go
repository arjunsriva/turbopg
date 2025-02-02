package turbopg

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestCreateNamespace(t *testing.T) {
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

	tests := []struct {
		name      string
		namespace string
		opts      CreateNamespaceOptions
		wantErr   bool
	}{
		{
			name:      "valid namespace with defaults",
			namespace: "vectors",
			opts: CreateNamespaceOptions{
				Dimensions: 128,
			},
			wantErr: false,
		},
		{
			name:      "valid namespace with custom index",
			namespace: "custom",
			opts: CreateNamespaceOptions{
				Dimensions: 256,
				IndexConfig: &IndexConfig{
					DistanceMetric: "euclidean_squared",
					Lists:          200,
				},
			},
			wantErr: false,
		},
		{
			name:      "invalid namespace name",
			namespace: "pg_test",
			opts: CreateNamespaceOptions{
				Dimensions: 128,
			},
			wantErr: true,
		},
		{
			name:      "invalid dimensions",
			namespace: "bad_dims",
			opts: CreateNamespaceOptions{
				Dimensions: 0,
			},
			wantErr: true,
		},
		{
			name:      "invalid distance metric",
			namespace: "bad_metric",
			opts: CreateNamespaceOptions{
				Dimensions: 128,
				IndexConfig: &IndexConfig{
					DistanceMetric: "invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create namespace
			err := store.CreateNamespace(ctx, tt.namespace, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Verify table exists
			tableName := GetNamespaceTableName(prefix, tt.namespace)
			var exists bool
			query := `
				SELECT EXISTS (
					SELECT FROM pg_tables
					WHERE schemaname = 'public'
					AND tablename = $1
				)`
			err = db.QueryRowContext(ctx, query, tableName).Scan(&exists)
			if err != nil {
				t.Fatalf("failed to check if table exists: %v", err)
			}
			if !exists {
				t.Error("table was not created")
			}

			// Verify index exists
			indexName := fmt.Sprintf("%s_vector_idx", tableName)
			query = `
				SELECT EXISTS (
					SELECT FROM pg_indexes
					WHERE schemaname = 'public'
					AND tablename = $1
					AND indexname = $2
				)`
			err = db.QueryRowContext(ctx, query, tableName, indexName).Scan(&exists)
			if err != nil {
				t.Fatalf("failed to check if index exists: %v", err)
			}
			if !exists {
				t.Error("index was not created")
			}
		})
	}
}

func TestDeleteNamespace(t *testing.T) {
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

	// Create a test namespace first
	err = store.CreateNamespace(ctx, "vectors", CreateNamespaceOptions{
		Dimensions: 128,
	})
	if err != nil {
		t.Fatalf("failed to create test namespace: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		wantErr   bool
	}{
		{
			name:      "delete existing namespace",
			namespace: "vectors",
			wantErr:   false,
		},
		{
			name:      "delete non-existent namespace",
			namespace: "not_exists",
			wantErr:   false, // Should not error, following idempotency principle
		},
		{
			name:      "invalid namespace name",
			namespace: "pg_test",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Delete namespace
			err := store.DeleteNamespace(ctx, tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Verify table no longer exists
			tableName := GetNamespaceTableName(prefix, tt.namespace)
			var exists bool
			query := `
				SELECT EXISTS (
					SELECT FROM pg_tables
					WHERE schemaname = 'public'
					AND tablename = $1
				)`
			err = db.QueryRowContext(ctx, query, tableName).Scan(&exists)
			if err != nil {
				t.Fatalf("failed to check if table exists: %v", err)
			}
			if exists {
				t.Error("table still exists after deletion")
			}

			// Verify indexes are gone
			query = `
				SELECT EXISTS (
					SELECT FROM pg_indexes
					WHERE schemaname = 'public'
					AND tablename = $1
				)`
			err = db.QueryRowContext(ctx, query, tableName).Scan(&exists)
			if err != nil {
				t.Fatalf("failed to check if indexes exist: %v", err)
			}
			if exists {
				t.Error("indexes still exist after deletion")
			}
		})
	}
}

func TestListNamespaces(t *testing.T) {
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

	// Create some test namespaces
	namespaces := []struct {
		name string
		dims int
	}{
		{"vectors1", 128},
		{"vectors2", 256},
		{"other", 512},
	}

	for _, ns := range namespaces {
		err := store.CreateNamespace(ctx, ns.name, CreateNamespaceOptions{
			Dimensions: ns.dims,
		})
		if err != nil {
			t.Fatalf("failed to create namespace %q: %v", ns.name, err)
		}
	}

	tests := []struct {
		name    string
		opts    ListNamespacesOptions
		want    []string
		total   int
		wantErr bool
	}{
		{
			name: "list all namespaces",
			opts: ListNamespacesOptions{},
			want: []string{"other", "vectors1", "vectors2"},
			total: 3,
		},
		{
			name: "list with prefix filter",
			opts: ListNamespacesOptions{
				Prefix: "vectors",
			},
			want: []string{"vectors1", "vectors2"},
			total: 2,
		},
		{
			name: "list with limit",
			opts: ListNamespacesOptions{
				Limit: 2,
			},
			want: []string{"other", "vectors1"},
			total: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.ListNamespaces(ctx, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListNamespaces() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if got.Total != tt.total {
				t.Errorf("ListNamespaces() total = %v, want %v", got.Total, tt.total)
			}

			if !reflect.DeepEqual(got.Namespaces, tt.want) {
				t.Errorf("ListNamespaces() namespaces = %v, want %v", got.Namespaces, tt.want)
			}
		})
	}
}

func TestGetNamespace(t *testing.T) {
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

	// Create a test namespace with custom config
	err = store.CreateNamespace(ctx, "vectors", CreateNamespaceOptions{
		Dimensions: 128,
		IndexConfig: &IndexConfig{
			DistanceMetric: "euclidean_squared",
			Lists:          200,
		},
	})
	if err != nil {
		t.Fatalf("failed to create test namespace: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		want      *Namespace
		wantErr   bool
	}{
		{
			name:      "get existing namespace",
			namespace: "vectors",
			want: &Namespace{
				Name:       "vectors",
				Dimensions: 128,
				IndexConfig: &IndexConfig{
					DistanceMetric: "euclidean_squared",
					Lists:          200,
				},
			},
			wantErr: false,
		},
		{
			name:      "get non-existent namespace",
			namespace: "not_exists",
			want:      nil,
			wantErr:   true,
		},
		{
			name:      "get invalid namespace name",
			namespace: "pg_test",
			want:      nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetNamespace(ctx, tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetNamespace() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
