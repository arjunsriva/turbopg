package turbopg

import (
	"context"
	"database/sql"
	"encoding/json"
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

// ListNamespacesOptions holds options for listing namespaces
type ListNamespacesOptions struct {
	// Optional prefix to filter namespaces by
	Prefix string
	// Maximum number of namespaces to return
	Limit int
}

// ListNamespacesResponse holds the response from ListNamespaces
type ListNamespacesResponse struct {
	// List of namespaces
	Namespaces []string
	// Total number of namespaces (ignoring limit)
	Total int
}

// Namespace represents a namespace and its configuration
type Namespace struct {
	// Name of the namespace
	Name string
	// Number of dimensions in vectors
	Dimensions int
	// Index configuration
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

	// Write metadata to system table
	sysTable := GetSystemTableName(s.prefix, "namespaces")
	indexConfigJSON, err := json.Marshal(indexConfig)
	if err != nil {
		return fmt.Errorf("marshal index config: %w", err)
	}

	query = fmt.Sprintf(`
		INSERT INTO %s (namespace, dimensions, index_config, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (namespace) DO UPDATE SET
			dimensions = $2,
			index_config = $3,
			updated_at = NOW()`,
		sysTable)

	_, err = s.db.ExecContext(ctx, query, namespace, opts.Dimensions, indexConfigJSON)
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
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

	// Delete metadata from system table
	sysTable := GetSystemTableName(s.prefix, "namespaces")
	query = fmt.Sprintf(`
		DELETE FROM %s 
		WHERE namespace = $1`,
		sysTable)

	_, err = s.db.ExecContext(ctx, query, namespace)
	if err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}

	s.logger.Info("deleted namespace",
		Field{Key: "namespace", Value: namespace},
	)

	return nil
}

// ListNamespaces lists all namespaces in the store
func (s *Store) ListNamespaces(ctx context.Context, opts ListNamespacesOptions) (*ListNamespacesResponse, error) {
	// Build query to list namespaces from metadata table
	sysTable := GetSystemTableName(s.prefix, "namespaces")

	// Base query
	query := fmt.Sprintf(`
		SELECT namespace 
		FROM %s 
		WHERE 1=1`,
		sysTable)

	// Add prefix filter if specified
	var args []interface{}
	if opts.Prefix != "" {
		args = append(args, opts.Prefix+"%")
		query += fmt.Sprintf(" AND namespace LIKE $%d", len(args))
	}

	// Add ordering
	query += " ORDER BY namespace"

	// Get total count (including prefix filter)
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS t", query)
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("count namespaces: %w", err)
	}

	// Add limit if specified
	if opts.Limit > 0 {
		args = append(args, opts.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	defer rows.Close()

	// Extract namespace names
	var namespaces []string
	for rows.Next() {
		var namespace string
		if err := rows.Scan(&namespace); err != nil {
			return nil, fmt.Errorf("scan namespace: %w", err)
		}
		namespaces = append(namespaces, namespace)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate namespaces: %w", err)
	}

	return &ListNamespacesResponse{
		Namespaces: namespaces,
		Total:      total,
	}, nil
}

// GetNamespace gets information about a specific namespace
func (s *Store) GetNamespace(ctx context.Context, namespace string) (*Namespace, error) {
	// Validate namespace name
	if err := validation.ValidateNamespace(namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace name: %w", err)
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, namespace)

	// First check if the table exists
	var exists bool
	query := `
		SELECT EXISTS (
			SELECT FROM pg_tables
			WHERE schemaname = 'public'
			AND tablename = $1
		)`
	err := s.db.QueryRowContext(ctx, query, tableName).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check table existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("namespace %q does not exist", namespace)
	}

	// Get metadata from system table
	sysTable := GetSystemTableName(s.prefix, "namespaces")
	var (
		dimensions  int
		indexConfig IndexConfig
		configJSON  []byte
	)

	query = fmt.Sprintf(`
		SELECT dimensions, index_config
		FROM %s
		WHERE namespace = $1`,
		sysTable)

	err = s.db.QueryRowContext(ctx, query, namespace).Scan(&dimensions, &configJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("namespace %q metadata not found", namespace)
		}
		return nil, fmt.Errorf("get metadata: %w", err)
	}

	err = json.Unmarshal(configJSON, &indexConfig)
	if err != nil {
		return nil, fmt.Errorf("unmarshal index config: %w", err)
	}

	return &Namespace{
		Name:        namespace,
		Dimensions:  dimensions,
		IndexConfig: &indexConfig,
	}, nil
}
