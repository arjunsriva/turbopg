package turbopg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arjunsriva/turbopg/internal/pgdynmigrate"
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

	return &Store{
		db:       db,
		prefix:   cfg.Prefix,
		logger:   cfg.Logger,
		dbURL:    cfg.DBURL,
		migrator: m,
	}, nil
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
