// Package pgdynmigrate provides a PostgreSQL-specific implementation of the golang-migrate
// source.Driver interface that allows for dynamic creation of migrations at runtime.
//
// The package is designed for systems that need to programmatically generate and apply
// database migrations in a distributed environment, while ensuring safe concurrent access
// and maintaining sequential version numbers.
//
// Key features:
//   - Dynamic migration creation at runtime
//   - Safe concurrent access using PostgreSQL advisory locks
//   - Guaranteed sequential version numbers
//   - Full transaction safety
//   - Compatible with golang-migrate
//
// Example usage:
//
//	db, err := sql.Open("postgres", "postgres://localhost:5432/mydb?sslmode=disable")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	source, err := pgdynmigrate.NewPostgresSource(db, "my_app_")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	err = source.AddMigration(ctx, "create_users",
//	    "CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT);",
//	    "DROP TABLE users;")
//	if err != nil {
//	    log.Fatal(err)
//	}
package pgdynmigrate

import (
	"context"
	"database/sql"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/lib/pq"
)

func init() {
	source.Register("dynmigrate", &PostgresSource{})
}

// PostgresSource implements the source.Driver interface for PostgreSQL databases.
// It provides functionality to dynamically create and manage migrations at runtime
// while ensuring safe concurrent access and sequential versioning.
type PostgresSource struct {
	db     *sql.DB
	prefix string
}

// Our tables for managing migrations
const createTablesSQL = `
CREATE TABLE IF NOT EXISTS %smigration_requests (
    id SERIAL,
    version BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    up_sql TEXT NOT NULL,
    down_sql TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS %smigration_request_version (
    id INTEGER PRIMARY KEY DEFAULT 1,
    latest_version BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT single_row CHECK (id = 1)
);

-- Initialize version tracking if not exists
INSERT INTO %smigration_request_version (id, latest_version)
VALUES (1, 0)
ON CONFLICT (id) DO NOTHING;`

// NewPostgresSource creates a new PostgresSource with the given database connection
// and table name prefix. It initializes the necessary tables if they don't exist.
//
// The prefix is used to namespace the migration tables, allowing multiple applications
// to manage their migrations independently in the same database.
func NewPostgresSource(db *sql.DB, prefix string) (*PostgresSource, error) {
	s := &PostgresSource{
		db:     db,
		prefix: prefix,
	}

	// Initialize tables
	if _, err := db.Exec(fmt.Sprintf(createTablesSQL, prefix, prefix, prefix)); err != nil {
		return nil, fmt.Errorf("create tables: %w", err)
	}

	return s, nil
}

// WithInstance creates a new source.Driver from an existing database connection.
// This is the recommended way to create a new source when using with golang-migrate.
func WithInstance(db *sql.DB, prefix string) (source.Driver, error) {
	return NewPostgresSource(db, prefix)
}

// Open implements source.Driver but is not supported.
// Use WithInstance instead to create a new driver.
func (s *PostgresSource) Open(url string) (source.Driver, error) {
	return nil, fmt.Errorf("not implemented - use WithInstance")
}

func (s *PostgresSource) Close() error {
	return nil
}

func (s *PostgresSource) First() (version uint, err error) {
	var v int64
	var exists bool
	err = s.db.QueryRow(fmt.Sprintf(`
        SELECT EXISTS (
            SELECT version FROM %smigration_requests 
            ORDER BY version ASC LIMIT 1
        )`, s.prefix)).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, os.ErrNotExist
	}

	err = s.db.QueryRow(fmt.Sprintf(`
        SELECT version FROM %smigration_requests 
        ORDER BY version ASC LIMIT 1`, s.prefix)).Scan(&v)
	if err != nil {
		return 0, err
	}
	return uint(v), nil
}

func (s *PostgresSource) Prev(version uint) (uint, error) {
	var v int64
	var exists bool
	err := s.db.QueryRow(fmt.Sprintf(`
        SELECT EXISTS (
            SELECT version FROM %smigration_requests 
            WHERE version < $1 
            ORDER BY version DESC LIMIT 1
        )`, s.prefix), version).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, os.ErrNotExist
	}

	err = s.db.QueryRow(fmt.Sprintf(`
        SELECT version FROM %smigration_requests 
        WHERE version < $1 
        ORDER BY version DESC LIMIT 1`, s.prefix), version).Scan(&v)
	if err != nil {
		return 0, err
	}
	return uint(v), nil
}

func (s *PostgresSource) Next(version uint) (uint, error) {
	var v int64
	var exists bool
	err := s.db.QueryRow(fmt.Sprintf(`
        SELECT EXISTS (
            SELECT version FROM %smigration_requests 
            WHERE version > $1 
            ORDER BY version ASC LIMIT 1
        )`, s.prefix), version).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, os.ErrNotExist
	}

	err = s.db.QueryRow(fmt.Sprintf(`
        SELECT version FROM %smigration_requests 
        WHERE version > $1 
        ORDER BY version ASC LIMIT 1`, s.prefix), version).Scan(&v)
	if err != nil {
		return 0, err
	}
	return uint(v), nil
}

func (s *PostgresSource) ReadUp(version uint) (io.ReadCloser, string, error) {
	var sql string
	var exists bool
	err := s.db.QueryRow(fmt.Sprintf(`
        SELECT EXISTS (
            SELECT up_sql FROM %smigration_requests 
            WHERE version = $1
        )`, s.prefix), version).Scan(&exists)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "", os.ErrNotExist
	}

	err = s.db.QueryRow(fmt.Sprintf(`
        SELECT up_sql FROM %smigration_requests 
        WHERE version = $1`, s.prefix), version).Scan(&sql)
	if err != nil {
		return nil, "", err
	}

	identifier := fmt.Sprintf("%d.up.sql", version)
	return io.NopCloser(strings.NewReader(sql)), identifier, nil
}

func (s *PostgresSource) ReadDown(version uint) (io.ReadCloser, string, error) {
	var sql string
	var exists bool
	err := s.db.QueryRow(fmt.Sprintf(`
        SELECT EXISTS (
            SELECT down_sql FROM %smigration_requests 
            WHERE version = $1
        )`, s.prefix), version).Scan(&exists)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "", os.ErrNotExist
	}

	err = s.db.QueryRow(fmt.Sprintf(`
        SELECT down_sql FROM %smigration_requests 
        WHERE version = $1`, s.prefix), version).Scan(&sql)
	if err != nil {
		return nil, "", err
	}

	identifier := fmt.Sprintf("%d.down.sql", version)
	return io.NopCloser(strings.NewReader(sql)), identifier, nil
}

// AddMigration adds a new migration to the database with the given name and SQL commands.
// It automatically assigns the next available version number and ensures safe concurrent
// access using PostgreSQL advisory locks.
//
// The operation is fully transactional - if any part fails, the entire operation is
// rolled back and no version number is consumed.
//
// If another process is currently adding a migration, this method will block until
// the lock is available or the context is cancelled.
func (s *PostgresSource) AddMigration(ctx context.Context, name, upSQL, downSQL string) error {
	tx, err := s.db.BeginTx(ctx, nil) // Default isolation level is fine
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Ignore ErrTxDone since it means the tx was already committed or rolled back
			if err != sql.ErrTxDone {
				log.Printf("error rolling back transaction: %v", err)
			}
		}
	}()

	// Acquire advisory lock - this is a PostgreSQL feature that's perfect for this use case
	// We use a consistent hash of our prefix as the lock key to prevent conflicts between different apps
	// This will block until the lock is available
	lockKey := int64(crc32.ChecksumIEEE([]byte(s.prefix)))
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}

	// Get next version
	var version int64
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE %smigration_request_version
		SET latest_version = latest_version + 1
		WHERE id = 1
		RETURNING latest_version`, s.prefix)).Scan(&version)
	if err != nil {
		return fmt.Errorf("increment version: %w", err)
	}

	// Insert migration request
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %smigration_requests (version, name, up_sql, down_sql)
		VALUES ($1, $2, $3, $4)`, s.prefix),
		version, name, upSQL, downSQL)
	if err != nil {
		return fmt.Errorf("insert migration: %w", err)
	}

	// The advisory lock is automatically released when the transaction commits/rolls back
	return tx.Commit()
}
