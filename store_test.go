package turbopg

import (
	"context"
	"testing"

	_ "github.com/lib/pq"
)

// mockLogger implements Logger for testing
type mockLogger struct {
	errorCalled bool
	infoCalled  bool
	debugCalled bool
}

func (l *mockLogger) Error(msg string, fields ...Field) { l.errorCalled = true }
func (l *mockLogger) Info(msg string, fields ...Field)  { l.infoCalled = true }
func (l *mockLogger) Debug(msg string, fields ...Field) { l.debugCalled = true }

func TestInitialize(t *testing.T) {
	// Setup test database without pgvector extension
	db := setupTestDB(t)
	defer db.cleanup(t)

	// Test initialization
	ctx := context.Background()
	if err := Initialize(ctx, db.DB); err != nil {
		t.Errorf("Initialize() error = %v", err)
	}

	// Verify extension exists
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_extension WHERE extname = 'vector'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to check extension: %v", err)
	}
	if !exists {
		t.Error("vector extension was not created")
	}

	// Verify we can create and query vectors
	_, err = db.ExecContext(ctx, `
		CREATE TABLE test_vectors (
			id TEXT PRIMARY KEY,
			embedding vector(3)
		)
	`)
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	// Insert a test vector
	_, err = db.ExecContext(ctx, `
		INSERT INTO test_vectors (id, embedding) 
		VALUES ('test', '[1,2,3]')
	`)
	if err != nil {
		t.Fatalf("failed to insert test vector: %v", err)
	}

	// Query the vector
	var distance float64
	err = db.QueryRowContext(ctx, `
		SELECT embedding <-> '[4,5,6]'::vector 
		FROM test_vectors 
		WHERE id = 'test'
	`).Scan(&distance)
	if err != nil {
		t.Fatalf("failed to query vector: %v", err)
	}

	// We don't care about the exact distance, just that it worked
	if distance <= 0 {
		t.Error("invalid distance returned")
	}
}

func TestNew(t *testing.T) {
	// Set up test database
	testDB := setupTestDB(t)
	defer testDB.cleanup(t)

	tests := []struct {
		name   string
		config Config
		want   *Store
	}{
		{
			name:   "with default config",
			config: Config{},
			want: &Store{
				db:     testDB.DB,
				prefix: "",
				logger: &noopLogger{},
				dbURL:  "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
			},
		},
		{
			name: "with custom logger",
			config: Config{
				Logger: &mockLogger{},
			},
			want: &Store{
				db:     testDB.DB,
				prefix: "",
				logger: &mockLogger{},
				dbURL:  "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(testDB.DB, tt.config)
			if err != nil {
				t.Errorf("New() error = %v", err)
				return
			}

			// Compare fields except migrator since it's not easily comparable
			if got.db != tt.want.db {
				t.Errorf("New().db = %v, want %v", got.db, tt.want.db)
			}
			if got.prefix != tt.want.prefix {
				t.Errorf("New().prefix = %v, want %v", got.prefix, tt.want.prefix)
			}
			if got.dbURL != tt.want.dbURL {
				t.Errorf("New().dbURL = %v, want %v", got.dbURL, tt.want.dbURL)
			}
		})
	}
}

func TestNewDefault(t *testing.T) {
	// Set up test database
	testDB := setupTestDB(t)
	defer testDB.cleanup(t)

	store, err := NewDefault(testDB.DB)
	if err != nil {
		t.Fatalf("NewDefault() error = %v", err)
	}

	if store.prefix != "turbopg_" {
		t.Errorf("expected prefix 'turbopg_', got %q", store.prefix)
	}
	if store.db != testDB.DB {
		t.Error("db not set correctly")
	}
	if store.logger == nil {
		t.Error("logger not set")
	}
}
