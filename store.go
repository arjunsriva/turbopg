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
	// IVFFlatProbes is SET ivfflat.probes for ANN queries. Zero means DefaultIVFFlatProbes.
	IVFFlatProbes int
	// DefaultLists is used when creating a namespace with Lists <= 0.
	// Zero means NewDefaultIndexConfig().Lists (library default 1).
	DefaultLists int
	// PatchByFilterMax caps patch_by_filter rows per request. Zero means DefaultPatchByFilterMax.
	PatchByFilterMax int
	// DeleteByFilterMax caps delete_by_filter rows per request. Zero means DefaultDeleteByFilterMax.
	DeleteByFilterMax int
}

// Store represents a vector store backed by PostgreSQL
type Store struct {
	db                *sql.DB
	prefix            string
	logger            Logger
	dbURL             string
	migrator          *pgdynmigrate.Migrator
	ivfProbes         int
	defaultLists      int
	patchByFilterMax  int
	deleteByFilterMax int
}

// New creates a new Store with the given configuration
func New(db *sql.DB, cfg Config) (*Store, error) {
	if cfg.Logger == nil {
		cfg.Logger = &NoOpLogger{}
	}
	if cfg.DBURL == "" {
		cfg.DBURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}

	// Create migrator with store prefix
	m, err := pgdynmigrate.New(db, GetSystemTableName(cfg.Prefix, ""))
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}

	probes := cfg.IVFFlatProbes
	if probes <= 0 {
		probes = DefaultIVFFlatProbes
	}
	patchMax := cfg.PatchByFilterMax
	if patchMax <= 0 {
		patchMax = DefaultPatchByFilterMax
	}
	deleteMax := cfg.DeleteByFilterMax
	if deleteMax <= 0 {
		deleteMax = DefaultDeleteByFilterMax
	}
	store := &Store{
		db:                db,
		prefix:            cfg.Prefix,
		logger:            cfg.Logger,
		dbURL:             cfg.DBURL,
		migrator:          m,
		ivfProbes:         probes,
		defaultLists:      cfg.DefaultLists,
		patchByFilterMax:  patchMax,
		deleteByFilterMax: deleteMax,
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
		Logger: &NoOpLogger{},
	})
}

// Initialize sets up the database for use with turbopg.
// This should be called once before using the store.
// It will:
// - Verify the database connection
// - Create the pgvector extension if it doesn't exist
// - Create pg_textsearch when the server has it (PostgreSQL 17+, BM25)
// - Verify pgvector is working correctly
func Initialize(ctx context.Context, db *sql.DB) error {
	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create pgvector extension
	if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("failed to create vector extension: %w", err)
	}

	// Optional: Timescale pg_textsearch for BM25. Missing the extension is not
	// fatal — vector search still works; BM25 queries return ErrTextSearchUnavailable.
	if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_textsearch"); err != nil {
		_ = err
	}
	if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		_ = err
	}

	// Verify extension by creating a small test vector
	var result bool
	err := db.QueryRowContext(ctx, "SELECT '[1,2,3]'::vector <-> '[4,5,6]'::vector < 100").Scan(&result)
	if err != nil {
		return fmt.Errorf("vector extension verification failed: %w", err)
	}

	return nil
}

// Ping reports whether the store can reach Postgres.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store not initialized")
	}
	return s.db.PingContext(ctx)
}

// DBStats returns pool stats for operator metrics.
func (s *Store) DBStats() sql.DBStats {
	if s == nil || s.db == nil {
		return sql.DBStats{}
	}
	return s.db.Stats()
}

// initializeSystemTables creates the necessary system tables for turbopg
func (s *Store) initializeSystemTables(ctx context.Context) error {
	// Create namespace metadata table
	sysTable := SQLIdent(GetSystemTableName(s.prefix, "namespaces"))
	upSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			namespace TEXT PRIMARY KEY,
			dimensions INTEGER NOT NULL,
			index_config JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`, sysTable)

	downSQL := fmt.Sprintf(`
		DROP TABLE IF EXISTS %s;`, sysTable)

	if err := s.migrator.Append(ctx, "create_system_tables", upSQL, downSQL); err != nil {
		return fmt.Errorf("add system tables migration: %w", err)
	}

	schemaUp := fmt.Sprintf(`
		ALTER TABLE %s ADD COLUMN IF NOT EXISTS attr_schema JSONB NOT NULL DEFAULT '{}'::jsonb;
		ALTER TABLE %s ADD COLUMN IF NOT EXISTS extra_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;`,
		sysTable, sysTable)
	schemaDown := fmt.Sprintf(`
		ALTER TABLE %s DROP COLUMN IF EXISTS extra_metadata;
		ALTER TABLE %s DROP COLUMN IF EXISTS attr_schema;`,
		sysTable, sysTable)
	if err := s.migrator.Append(ctx, "namespace_attr_schema", schemaUp, schemaDown); err != nil {
		return fmt.Errorf("add schema columns migration: %w", err)
	}

	if err := s.migrator.Up(ctx); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run system tables migration: %w", err)
	}

	return nil
}

// executeQueryAndParse is a helper function to execute a query and parse results.
func (s *Store) executeQueryAndParse(ctx context.Context, query string, args []interface{}, rowProcessor func(*sql.Rows) (*QueryResult, error), exact bool) ([]QueryResult, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("checkout connection: %w", err)
	}
	defer conn.Close()

	var rows *sql.Rows
	if exact {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("begin exact search: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, "SET LOCAL enable_indexscan = off"); err != nil {
			s.logger.Debug("set enable_indexscan", Field{Key: "error", Value: err.Error()})
		}
		if _, err := tx.ExecContext(ctx, "SET LOCAL enable_bitmapscan = off"); err != nil {
			s.logger.Debug("set enable_bitmapscan", Field{Key: "error", Value: err.Error()})
		}
		rows, err = tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("execute query: %w", err)
		}
	} else {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET ivfflat.probes = %d", s.ivfProbes)); err != nil {
			s.logger.Debug("set ivfflat.probes", Field{Key: "error", Value: err.Error()})
		}
		rows, err = conn.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("execute query: %w", err)
		}
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		result, err := rowProcessor(rows)
		if err != nil {
			return nil, err
		}
		if result != nil { // Allow rowProcessor to return nil to skip rows if needed
			results = append(results, *result)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate results: %w", err)
	}
	return results, nil
}
