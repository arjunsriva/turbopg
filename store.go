package turbopg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arjunsriva/turbopg/internal/pgdynmigrate"
	"github.com/golang-migrate/migrate/v4"
)

// Config holds the configuration for the vector store
type Config struct {
	// Prefix for all tables created by this store
	Prefix string
	// Optional logger, defaults to no-op logger
	Logger Logger
	// Database URL for migrations
	DBURL string
}

// Store represents a vector store backed by PostgreSQL
type Store struct {
	db       *sql.DB
	prefix   string
	logger   Logger
	dbURL    string
	migrator *pgdynmigrate.Migrator
}

// New creates a new Store with the given configuration
func New(db *sql.DB, cfg Config) (*Store, error) {
	if cfg.Logger == nil {
		cfg.Logger = &noopLogger{}
	}
	if cfg.DBURL == "" {
		cfg.DBURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}

	// Create migrator with store prefix
	m, err := pgdynmigrate.New(db, GetSystemTableName(cfg.Prefix, ""))
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}

	store := &Store{
		db:       db,
		prefix:   cfg.Prefix,
		logger:   cfg.Logger,
		dbURL:    cfg.DBURL,
		migrator: m,
	}

	// Initialize system tables
	if err := store.initializeSystemTables(context.Background()); err != nil {
		return nil, fmt.Errorf("initialize system tables: %w", err)
	}

	return store, nil
}

// NewDefault creates a new Store with default configuration
func NewDefault(db *sql.DB) (*Store, error) {
	return New(db, Config{
		Prefix: "turbopg_",
		DBURL:  "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
		Logger: &noopLogger{},
	})
}

// Initialize sets up the database for use with turbopg.
// This should be called once before using the store.
// It will:
// - Verify the database connection
// - Create the pgvector extension if it doesn't exist
// - Verify the extension is working correctly
func Initialize(ctx context.Context, db *sql.DB) error {
	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create pgvector extension
	if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("failed to create vector extension: %w", err)
	}

	// Verify extension by creating a small test vector
	var result bool
	err := db.QueryRowContext(ctx, "SELECT '[1,2,3]'::vector <-> '[4,5,6]'::vector < 100").Scan(&result)
	if err != nil {
		return fmt.Errorf("vector extension verification failed: %w", err)
	}

	return nil
}

// initializeSystemTables creates the necessary system tables for turbopg
func (s *Store) initializeSystemTables(ctx context.Context) error {
	// Create namespace metadata table
	upSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			namespace TEXT PRIMARY KEY,
			dimensions INTEGER NOT NULL,
			index_config JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`, GetSystemTableName(s.prefix, "namespaces"))

	downSQL := fmt.Sprintf(`
		DROP TABLE IF EXISTS %s;`, GetSystemTableName(s.prefix, "namespaces"))

	if err := s.migrator.Append(ctx, "create_system_tables", upSQL, downSQL); err != nil {
		return fmt.Errorf("add system tables migration: %w", err)
	}

	if err := s.migrator.Up(ctx); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run system tables migration: %w", err)
	}

	return nil
}
