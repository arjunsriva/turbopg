package pgdynmigrate

import (
	"context"
	"testing"

	_ "github.com/lib/pq"
)

func TestMigrator(t *testing.T) {
	// Setup test database
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()

	// Create migrator
	m, err := New(db.DB, "test_")
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}

	// Add a test migration
	err = m.Append(ctx, "create_test_table",
		"CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT);",
		"DROP TABLE test_table;",
	)
	if err != nil {
		t.Fatalf("append migration: %v", err)
	}

	// Apply migrations
	if err := m.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	// Verify table exists
	var exists bool
	err = db.QueryRowContext(ctx, `
        SELECT EXISTS (
            SELECT FROM pg_tables
            WHERE schemaname = 'public'
            AND tablename = 'test_table'
        )
    `).Scan(&exists)
	if err != nil {
		t.Fatalf("check table exists: %v", err)
	}
	if !exists {
		t.Error("test_table was not created")
	}
}
