package turbopg

import (
	"context"
	"fmt"

	"github.com/arjunsriva/turbopg/internal/validation"
	"github.com/golang-migrate/migrate/v4"
)

// IndexConfig holds configuration for the vector similarity index
type IndexConfig struct {
	// Type of distance metric to use for vector similarity
	// Must be one of: "cosine_distance" or "euclidean_squared"
	DistanceMetric string

	// Number of IVF lists for approximate search
	// Higher values = faster search, less accurate
	// Lower values = slower search, more accurate
	Lists int
}

// NewDefaultIndexConfig returns the default index configuration
func NewDefaultIndexConfig() *IndexConfig {
	return &IndexConfig{
		DistanceMetric: "cosine_distance",
		Lists:          100, // TurboPuffer default
	}
}

// CreateNamespaceOptions holds options for namespace creation
type CreateNamespaceOptions struct {
	// Required: number of dimensions for vectors in this namespace
	Dimensions int

	// Optional: index configuration
	IndexConfig *IndexConfig
}

// CreateNamespace creates a new namespace with the given configuration
func (s *Store) CreateNamespace(ctx context.Context, namespace string, opts CreateNamespaceOptions) error {
	// Validate namespace name
	if err := validation.ValidateNamespace(namespace); err != nil {
		return fmt.Errorf("invalid namespace name: %w", err)
	}

	// Validate dimensions
	if opts.Dimensions <= 0 {
		return fmt.Errorf("dimensions must be positive, got %d", opts.Dimensions)
	}

	// Use default index config if none provided
	indexConfig := opts.IndexConfig
	if indexConfig == nil {
		indexConfig = NewDefaultIndexConfig()
	}

	// Validate distance metric
	switch indexConfig.DistanceMetric {
	case "cosine_distance", "euclidean_squared":
		// valid
	default:
		return fmt.Errorf("invalid distance metric: %s", indexConfig.DistanceMetric)
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, namespace)

	// Choose operator based on distance metric
	operator := "vector_cosine_ops"
	if indexConfig.DistanceMetric == "euclidean_squared" {
		operator = "vector_l2_ops"
	}

	// Create up migration
	upSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			vector vector(%d),
			attributes JSONB
		);

		CREATE INDEX IF NOT EXISTS %s_vector_idx 
		ON %s 
		USING ivfflat (vector %s)
		WITH (lists = %d);`,
		tableName, opts.Dimensions,
		tableName, tableName, operator, indexConfig.Lists)

	// Create down migration
	downSQL := fmt.Sprintf(`
		DROP TABLE IF EXISTS %s CASCADE;`,
		tableName)

	// Add migration
	if err := s.migrator.Append(ctx, fmt.Sprintf("create_%s", namespace), upSQL, downSQL); err != nil {
		return fmt.Errorf("add migration: %w", err)
	}

	// Run migration
	if err := s.migrator.Up(ctx); err != nil {
		s.logger.Info("migration result",
			Field{Key: "error", Value: err.Error()},
			Field{Key: "is_no_change", Value: err == migrate.ErrNoChange},
		)
		if err != migrate.ErrNoChange {
			return fmt.Errorf("run migration: %w", err)
		}
	}

	// Verify table exists
	var exists bool
	query := `
		SELECT EXISTS (
			SELECT FROM pg_tables
			WHERE schemaname = 'public'
			AND tablename = $1
		)`
	err := s.db.QueryRowContext(ctx, query, tableName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check table existence: %w", err)
	}
	if !exists {
		s.logger.Info("table not found",
			Field{Key: "table", Value: tableName},
			Field{Key: "sql", Value: upSQL},
		)
		return fmt.Errorf("table %s was not created", tableName)
	}

	s.logger.Info("created namespace",
		Field{Key: "namespace", Value: namespace},
		Field{Key: "dimensions", Value: opts.Dimensions},
		Field{Key: "distance_metric", Value: indexConfig.DistanceMetric},
	)

	return nil
}

// DeleteNamespace deletes a namespace and all its data.
// This operation is idempotent - deleting a non-existent namespace is not an error.
func (s *Store) DeleteNamespace(ctx context.Context, namespace string) error {
	// Validate namespace name
	if err := validation.ValidateNamespace(namespace); err != nil {
		return fmt.Errorf("invalid namespace name: %w", err)
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, namespace)

	// For deletion, we first check if the table exists
	var exists bool
	query := `
		SELECT EXISTS (
			SELECT FROM pg_tables
			WHERE schemaname = 'public'
			AND tablename = $1
		)`
	err := s.db.QueryRowContext(ctx, query, tableName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check table existence: %w", err)
	}

	// If table doesn't exist, we're done (idempotent)
	if !exists {
		s.logger.Info("namespace does not exist, skipping deletion",
			Field{Key: "namespace", Value: namespace},
		)
		return nil
	}

	// Create up migration (delete)
	upSQL := fmt.Sprintf(`
		DROP TABLE IF EXISTS %s CASCADE;`,
		tableName)

	// Create down migration (recreate)
	// Note: We can't fully recreate the table since we don't store the dimensions and index config
	// This is a limitation - the down migration will need manual adjustment if needed
	downSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			vector vector(1),  -- placeholder dimension
			attributes JSONB
		);`,
		tableName)

	// Add migration
	if err := s.migrator.Append(ctx, fmt.Sprintf("delete_%s", namespace), upSQL, downSQL); err != nil {
		return fmt.Errorf("add migration: %w", err)
	}

	// Run migration
	if err := s.migrator.Up(ctx); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migration: %w", err)
	}

	s.logger.Info("deleted namespace",
		Field{Key: "namespace", Value: namespace},
	)

	return nil
}
