package pgdynmigrate

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"hash/crc32"

	_ "github.com/lib/pq"
)

func TestPostgresSource_Sequential(t *testing.T) {
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	source, err := NewPostgresSource(db.DB, "test_")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Add multiple migrations sequentially
	migrations := []struct {
		name string
		up   string
		down string
	}{
		{"first", "CREATE TABLE first (id INT);", "DROP TABLE first;"},
		{"second", "CREATE TABLE second (id INT);", "DROP TABLE second;"},
		{"third", "CREATE TABLE third (id INT);", "DROP TABLE third;"},
	}

	for _, m := range migrations {
		if err := source.AddMigration(ctx, m.name, m.up, m.down); err != nil {
			t.Fatalf("add migration %s: %v", m.name, err)
		}
	}

	// Verify versions are sequential starting from 1
	for i := 1; i <= len(migrations); i++ {
		var version int64
		err := db.QueryRowContext(ctx, `
			SELECT version FROM test_migration_requests 
			WHERE version = $1`, i).Scan(&version)
		if err != nil {
			t.Errorf("version %d not found: %v", i, err)
		}
		if version != int64(i) {
			t.Errorf("expected version %d, got %d", i, version)
		}
	}
}

func TestPostgresSource_Concurrent(t *testing.T) {
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	source, err := NewPostgresSource(db.DB, "test_")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Run multiple migrations concurrently
	const numMigrations = 10
	var wg sync.WaitGroup
	results := make(chan int64, numMigrations) // Channel for migration versions

	for i := 0; i < numMigrations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("migration_%d", i)
			up := fmt.Sprintf("CREATE TABLE table_%d (id INT);", i)
			down := fmt.Sprintf("DROP TABLE table_%d;", i)

			if err := source.AddMigration(ctx, name, up, down); err != nil {
				t.Errorf("migration %d failed: %v", i, err)
				return
			}

			// Get the version that was assigned
			var version int64
			err := db.QueryRowContext(ctx, `
				SELECT version FROM test_migration_requests 
				WHERE name = $1`, name).Scan(&version)
			if err != nil {
				t.Errorf("get version for migration %d: %v", i, err)
				return
			}
			results <- version
		}(i)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(results)

	// Collect all versions
	versions := make(map[int64]bool)
	numSuccess := 0
	for version := range results {
		if versions[version] {
			t.Errorf("duplicate version: %d", version)
		}
		versions[version] = true
		numSuccess++
	}

	// All migrations should succeed
	if numSuccess != numMigrations {
		t.Errorf("expected %d successful migrations, got %d", numMigrations, numSuccess)
	}

	// Verify versions are sequential from 1 to numMigrations
	for i := 1; i <= numMigrations; i++ {
		if !versions[int64(i)] {
			t.Errorf("missing version: %d", i)
		}
	}

	// Verify total count matches
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM test_migration_requests`).Scan(&count)
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != numMigrations {
		t.Errorf("database count %d doesn't match number of migrations %d", count, numMigrations)
	}
}

func TestPostgresSource_LockTimeout(t *testing.T) {
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	source, err := NewPostgresSource(db.DB, "test_")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Start a transaction and acquire the advisory lock
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	// Get the same lock key that the source will use
	lockKey := int64(crc32.ChecksumIEEE([]byte("test_")))
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey)
	if err != nil {
		t.Fatalf("acquire initial lock: %v", err)
	}

	// Try to add migration in another goroutine - should fail until we release the lock
	errCh := make(chan error, 1)
	go func() {
		err := source.AddMigration(ctx, "test", "CREATE TABLE test (id INT);", "DROP TABLE test;")
		errCh <- err
	}()

	// Wait a bit to ensure the goroutine is blocked
	time.Sleep(100 * time.Millisecond)

	// Release the lock by rolling back
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// Now the migration should succeed
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected success after lock release, got error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for migration")
	}

	// Verify the migration was added
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM test_migration_requests 
		WHERE name = 'test'`).Scan(&count)
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 migration, got %d", count)
	}
}

func TestPostgresSource_LockContention(t *testing.T) {
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	source, err := NewPostgresSource(db.DB, "test_")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Start a transaction and acquire the advisory lock
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			t.Logf("error rolling back transaction: %v", err)
		}
	}()

	// Get the same lock key that the source will use
	lockKey := int64(crc32.ChecksumIEEE([]byte("test_")))
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey)
	if err != nil {
		t.Fatalf("acquire initial lock: %v", err)
	}

	// Try to add migration - should fail due to lock
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		err := source.AddMigration(ctx, "test", "CREATE TABLE test (id INT);", "DROP TABLE test;")
		if err == nil {
			t.Error("expected error due to lock, got nil")
		}
		close(done)
	}()

	select {
	case <-done:
		// Expected - the migration should fail due to lock timeout
	case <-time.After(5 * time.Second):
		t.Error("test timed out")
	}

	// Verify no migration was added
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM test_migration_requests`).Scan(&count)
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 migrations, got %d", count)
	}
}

func TestPostgresSource_TransactionRollback(t *testing.T) {
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	source, err := NewPostgresSource(db.DB, "test_")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	// First migration should succeed
	err = source.AddMigration(ctx, "test1", "CREATE TABLE test1 (id INT);", "DROP TABLE test1;")
	if err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	// Force a version conflict by manually inserting a migration
	_, err = db.ExecContext(ctx, `
		INSERT INTO test_migration_requests (version, name, up_sql, down_sql)
		VALUES (2, 'manual', 'CREATE TABLE manual (id INT);', 'DROP TABLE manual;')`)
	if err != nil {
		t.Fatalf("insert manual migration: %v", err)
	}

	// This migration should fail due to version conflict
	err = source.AddMigration(ctx, "test2", "CREATE TABLE test2 (id INT);", "DROP TABLE test2;")
	if err == nil {
		t.Error("expected error due to version conflict, got nil")
	}

	// Verify version wasn't incremented beyond 2
	var version int64
	err = db.QueryRowContext(ctx, `
		SELECT latest_version FROM test_migration_request_version 
		WHERE id = 1`).Scan(&version)
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if version > 2 {
		t.Errorf("expected version <= 2, got %d", version)
	}

	// Verify only two migrations exist
	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM test_migration_requests`).Scan(&count)
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 migrations, got %d", count)
	}
}

func TestPostgresSource_Interface(t *testing.T) {
	db := setupTestDB(t)
	defer db.cleanup(t)

	ctx := context.Background()
	source, err := NewPostgresSource(db.DB, "test_")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Add some migrations
	migrations := []struct {
		version uint
		name    string
		up      string
		down    string
	}{
		{1, "first", "CREATE TABLE first (id INT);", "DROP TABLE first;"},
		{2, "second", "CREATE TABLE second (id INT);", "DROP TABLE second;"},
		{3, "third", "CREATE TABLE third (id INT);", "DROP TABLE third;"},
	}

	for _, m := range migrations {
		if err := source.AddMigration(ctx, m.name, m.up, m.down); err != nil {
			t.Fatalf("add migration %s: %v", m.name, err)
		}
	}

	// Test First()
	first, err := source.First()
	if err != nil {
		t.Fatalf("First(): %v", err)
	}
	if first != 1 {
		t.Errorf("First(): expected 1, got %d", first)
	}

	// Test Next()
	next, err := source.Next(1)
	if err != nil {
		t.Fatalf("Next(1): %v", err)
	}
	if next != 2 {
		t.Errorf("Next(1): expected 2, got %d", next)
	}

	// Test Prev()
	prev, err := source.Prev(3)
	if err != nil {
		t.Fatalf("Prev(3): %v", err)
	}
	if prev != 2 {
		t.Errorf("Prev(3): expected 2, got %d", prev)
	}

	// Test ReadUp()
	upReader, identifier, err := source.ReadUp(2)
	if err != nil {
		t.Fatalf("ReadUp(2): %v", err)
	}
	upSQL, err := io.ReadAll(upReader)
	if err != nil {
		t.Fatalf("read up SQL: %v", err)
	}
	if string(upSQL) != migrations[1].up {
		t.Errorf("ReadUp(2): expected %q, got %q", migrations[1].up, string(upSQL))
	}
	if identifier != "2.up.sql" {
		t.Errorf("ReadUp(2): expected identifier '2.up.sql', got %q", identifier)
	}

	// Test ReadDown()
	downReader, identifier, err := source.ReadDown(2)
	if err != nil {
		t.Fatalf("ReadDown(2): %v", err)
	}
	downSQL, err := io.ReadAll(downReader)
	if err != nil {
		t.Fatalf("read down SQL: %v", err)
	}
	if string(downSQL) != migrations[1].down {
		t.Errorf("ReadDown(2): expected %q, got %q", migrations[1].down, string(downSQL))
	}
	if identifier != "2.down.sql" {
		t.Errorf("ReadDown(2): expected identifier '2.down.sql', got %q", identifier)
	}
}
