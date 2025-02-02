package turbopg

import (
	"context"
	"log"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
)

func TestMain(m *testing.M) {
	// Disable testcontainers logging
	testcontainers.Logger = log.New(os.NewFile(0, os.DevNull), "", log.LstdFlags)
	
	// Run tests
	os.Exit(m.Run())
}

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

func TestInitializeSystemTables(t *testing.T) {
	// Setup test database
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

	// Table should already exist since New() calls initializeSystemTables
	tableName := GetSystemTableName(prefix, "namespaces")
	
	// Verify table exists
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
		t.Error("system table was not created")
	}

	// Verify table schema
	type column struct {
		name     string
		dataType string
		nullable string
	}
	query = `
		SELECT 
			column_name,
			data_type,
			is_nullable
		FROM information_schema.columns
		WHERE table_name = $1
		ORDER BY ordinal_position;`
	
	rows, err := db.QueryContext(ctx, query, tableName)
	if err != nil {
		t.Fatalf("failed to get table schema: %v", err)
	}
	defer rows.Close()

	var columns []column
	for rows.Next() {
		var col column
		if err := rows.Scan(&col.name, &col.dataType, &col.nullable); err != nil {
			t.Fatalf("failed to scan column: %v", err)
		}
		columns = append(columns, col)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("error iterating rows: %v", err)
	}

	// Verify expected columns
	expected := []column{
		{"namespace", "text", "NO"},
		{"dimensions", "integer", "NO"},
		{"index_config", "jsonb", "NO"},
		{"created_at", "timestamp with time zone", "NO"},
		{"updated_at", "timestamp with time zone", "NO"},
	}

	if len(columns) != len(expected) {
		t.Errorf("got %d columns, want %d", len(columns), len(expected))
	}

	for i, exp := range expected {
		if i >= len(columns) {
			t.Errorf("missing column %s", exp.name)
			continue
		}
		got := columns[i]
		if got.name != exp.name {
			t.Errorf("column %d name = %q, want %q", i, got.name, exp.name)
		}
		if got.dataType != exp.dataType {
			t.Errorf("column %s type = %q, want %q", exp.name, got.dataType, exp.dataType)
		}
		if got.nullable != exp.nullable {
			t.Errorf("column %s nullable = %q, want %q", exp.name, got.nullable, exp.nullable)
		}
	}

	// Test idempotency by calling initialize again
	if err := store.initializeSystemTables(ctx); err != nil {
		t.Errorf("second call to initializeSystemTables failed: %v", err)
	}
}
