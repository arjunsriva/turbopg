package turbopg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
		Lists:          1,
	}
}

// NormalizeDistanceMetric maps TurboPuffer and library metric aliases to the
// values stored in IndexConfig.DistanceMetric.
func NormalizeDistanceMetric(metric string) (string, error) {
	switch metric {
	case "", "cosine", "cosine_distance":
		return "cosine_distance", nil
	case "euclidean_squared", "euclidean_squared_distance":
		return "euclidean_squared", nil
	case "euclidean", "l2":
		return "euclidean_squared", nil
	default:
		return "", fmt.Errorf("invalid distance metric: %s", metric)
	}
}

// QueryMetricOperator returns the pgvector distance operator for a metric name.
func QueryMetricOperator(metric string) (string, error) {
	switch metric {
	case "", "cosine", "cosine_distance":
		return "<=>", nil
	case "euclidean", "l2", "euclidean_squared", "euclidean_squared_distance":
		return "<->", nil
	default:
		return "", fmt.Errorf("unsupported distance metric: %s", metric)
	}
}

// CreateNamespaceOptions holds options for namespace creation
type CreateNamespaceOptions struct {
	// Number of dimensions for vectors in this namespace. Zero creates an FTS-only
	// namespace (nullable vector column, no IVF index).
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
	// Cursor is the last namespace name from the previous page (exclusive).
	Cursor string
}

// ListNamespacesResponse holds the response from ListNamespaces
type ListNamespacesResponse struct {
	// List of namespaces
	Namespaces []string
	// Total number of namespaces (ignoring limit)
	Total int
	// NextCursor is set when another page exists.
	NextCursor string
}

// Namespace represents a namespace and its configuration
type Namespace struct {
	// Name of the namespace
	Name string
	// Number of dimensions in vectors
	Dimensions int
	// Index configuration
	IndexConfig *IndexConfig
	// Schema is the stored TurboPuffer attribute schema.
	Schema map[string]interface{}
	// CreatedAt is when the namespace was first created
	CreatedAt time.Time
	// UpdatedAt is when the namespace metadata was last written
	UpdatedAt time.Time
}

// NamespaceStats holds summary information about a namespace.
type NamespaceStats struct {
	Name             string
	ApproximateCount int64
	Dimensions       int
	DistanceMetric   string
	IndexConfig      *IndexConfig
	Schema           map[string]interface{}
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CreateNamespace creates a new namespace with the given configuration
func (s *Store) CreateNamespace(ctx context.Context, namespace string, opts CreateNamespaceOptions) error {
	// Validate namespace name
	if err := ValidateNamespace(namespace); err != nil {
		return fmt.Errorf("invalid namespace name: %w", err)
	}

	// Dimensions 0 is an FTS-only namespace (nullable vector, no IVF index).
	if opts.Dimensions < 0 {
		return fmt.Errorf("dimensions must be >= 0, got %d", opts.Dimensions)
	}

	// Use default index config if none provided
	indexConfig := opts.IndexConfig
	if indexConfig == nil {
		indexConfig = NewDefaultIndexConfig()
	}
	if indexConfig.Lists <= 0 {
		if s.defaultLists > 0 {
			indexConfig.Lists = s.defaultLists
		} else {
			indexConfig.Lists = NewDefaultIndexConfig().Lists
		}
	}
	normalized, err := NormalizeDistanceMetric(indexConfig.DistanceMetric)
	if err != nil {
		return err
	}
	indexConfig.DistanceMetric = normalized

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, namespace)
	quotedTable := SQLIdent(tableName)

	var upSQL string
	if opts.Dimensions == 0 {
		upSQL = fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			vector vector,
			attributes JSONB
		);`, quotedTable)
	} else {
		upSQL = fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			vector vector(%d),
			attributes JSONB
		);`, quotedTable, opts.Dimensions)
	}

	// Create down migration
	downSQL := fmt.Sprintf(`
		DROP TABLE IF EXISTS %s CASCADE;`,
		quotedTable)

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
	err = s.db.QueryRowContext(ctx, query, tableName).Scan(&exists)
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
	sysTable := SQLIdent(GetSystemTableName(s.prefix, "namespaces"))
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

	if err := s.ensureVectorIndexes(ctx, namespace); err != nil {
		return err
	}
	return nil
}

// DeleteNamespace deletes a namespace and all its data.
// This operation is idempotent - deleting a non-existent namespace is not an error.
func (s *Store) DeleteNamespace(ctx context.Context, namespace string) error {
	// Validate namespace name
	if err := ValidateNamespace(namespace); err != nil {
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

	quotedTable := SQLIdent(tableName)

	// Create up migration (delete)
	upSQL := fmt.Sprintf(`
		DROP TABLE IF EXISTS %s CASCADE;`,
		quotedTable)

	// Create down migration (recreate)
	// Note: We can't fully recreate the table since we don't store the dimensions and index config
	// This is a limitation - the down migration will need manual adjustment if needed
	downSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			vector vector(1),  -- placeholder dimension
			attributes JSONB
		);`,
		quotedTable)

	// Add migration
	if err := s.migrator.Append(ctx, fmt.Sprintf("delete_%s", namespace), upSQL, downSQL); err != nil {
		return fmt.Errorf("add migration: %w", err)
	}

	// Run migration
	if err := s.migrator.Up(ctx); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migration: %w", err)
	}

	// Delete metadata from system table
	sysTable := SQLIdent(GetSystemTableName(s.prefix, "namespaces"))
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
	sysTable := SQLIdent(GetSystemTableName(s.prefix, "namespaces"))

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
	if opts.Cursor != "" {
		args = append(args, opts.Cursor)
		query += fmt.Sprintf(" AND namespace > $%d", len(args))
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

	next := ""
	if opts.Limit > 0 && len(namespaces) == opts.Limit {
		next = namespaces[len(namespaces)-1]
	}

	return &ListNamespacesResponse{
		Namespaces: namespaces,
		Total:      total,
		NextCursor: next,
	}, nil
}

// GetNamespace gets information about a specific namespace
func (s *Store) GetNamespace(ctx context.Context, namespace string) (*Namespace, error) {
	// Validate namespace name
	if err := ValidateNamespace(namespace); err != nil {
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
		return nil, NamespaceNotFound(namespace)
	}

	// Get metadata from system table
	sysTable := SQLIdent(GetSystemTableName(s.prefix, "namespaces"))
	var (
		dimensions  int
		indexConfig IndexConfig
		configJSON  []byte
		schemaJSON  []byte
		createdAt   time.Time
		updatedAt   time.Time
	)

	query = fmt.Sprintf(`
		SELECT dimensions, index_config, attr_schema, created_at, updated_at
		FROM %s
		WHERE namespace = $1`,
		sysTable)

	err = s.db.QueryRowContext(ctx, query, namespace).Scan(&dimensions, &configJSON, &schemaJSON, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, NamespaceNotFound(namespace)
		}
		return nil, fmt.Errorf("get metadata: %w", err)
	}

	err = json.Unmarshal(configJSON, &indexConfig)
	if err != nil {
		return nil, fmt.Errorf("unmarshal index config: %w", err)
	}

	schema := map[string]interface{}{}
	if len(schemaJSON) > 0 {
		if err := json.Unmarshal(schemaJSON, &schema); err != nil {
			return nil, fmt.Errorf("unmarshal schema: %w", err)
		}
	}

	return &Namespace{
		Name:        namespace,
		Dimensions:  dimensions,
		IndexConfig: &indexConfig,
		Schema:      schema,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

// EnsureNamespace returns the namespace, creating it if it does not exist.
func (s *Store) EnsureNamespace(ctx context.Context, namespace string, opts CreateNamespaceOptions) (*Namespace, error) {
	ns, err := s.GetNamespace(ctx, namespace)
	if err == nil {
		return ns, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}
	if err := s.CreateNamespace(ctx, namespace, opts); err != nil {
		return nil, err
	}
	return s.GetNamespace(ctx, namespace)
}

// GetNamespaceStats returns metadata plus an approximate document count.
func (s *Store) GetNamespaceStats(ctx context.Context, namespace string) (*NamespaceStats, error) {
	ns, err := s.GetNamespace(ctx, namespace)
	if err != nil {
		return nil, err
	}
	count, err := s.CountDocuments(ctx, namespace, nil)
	if err != nil {
		return nil, err
	}
	metric := ""
	if ns.IndexConfig != nil {
		metric = ns.IndexConfig.DistanceMetric
	}
	return &NamespaceStats{
		Name:             ns.Name,
		ApproximateCount: count,
		Dimensions:       ns.Dimensions,
		DistanceMetric:   metric,
		IndexConfig:      ns.IndexConfig,
		Schema:           ns.Schema,
		CreatedAt:        ns.CreatedAt,
		UpdatedAt:        ns.UpdatedAt,
	}, nil
}

// GetSchema returns the stored attribute schema, merged with id/vector defaults.
func (s *Store) GetSchema(ctx context.Context, namespace string) (map[string]interface{}, error) {
	ns, err := s.GetNamespace(ctx, namespace)
	if err != nil {
		return nil, err
	}
	metric := "cosine_distance"
	if ns.IndexConfig != nil && ns.IndexConfig.DistanceMetric != "" {
		metric = ns.IndexConfig.DistanceMetric
	}
	out := map[string]interface{}{
		"id": map[string]interface{}{"type": "string", "filterable": true},
	}
	if ns.Dimensions > 0 {
		out["vector"] = map[string]interface{}{
			"type": fmt.Sprintf("[%d]f32", ns.Dimensions),
			"ann": map[string]interface{}{
				"distance_metric": metric,
			},
		}
	}
	for k, v := range ns.Schema {
		out[k] = v
	}
	inferred, err := s.inferAttributeSchema(ctx, namespace, out)
	if err != nil {
		return nil, err
	}
	for k, v := range inferred {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out, nil
}

func (s *Store) inferAttributeSchema(ctx context.Context, namespace string, existing map[string]interface{}) (map[string]interface{}, error) {
	tableName := SQLIdent(GetNamespaceTableName(s.prefix, namespace))
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT attributes FROM %s WHERE attributes IS NOT NULL LIMIT 256`, tableName))
	if err != nil {
		return nil, fmt.Errorf("infer schema: %w", err)
	}
	defer rows.Close()

	out := map[string]interface{}{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		var attrs map[string]interface{}
		if err := json.Unmarshal(raw, &attrs); err != nil {
			continue
		}
		for k, v := range attrs {
			if k == "id" || k == "vector" {
				continue
			}
			if _, exists := existing[k]; exists {
				continue
			}
			if _, exists := out[k]; exists {
				continue
			}
			out[k] = map[string]interface{}{
				"type":       inferJSONType(v),
				"filterable": true,
			}
		}
	}
	return out, rows.Err()
}

func inferJSONType(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "string"
	case bool:
		return "bool"
	case string:
		return "string"
	case json.Number:
		if _, err := t.Int64(); err == nil {
			if i, _ := t.Int64(); i >= 0 {
				return "uint"
			}
			return "int"
		}
		return "float"
	case float64:
		if t == float64(int64(t)) {
			if t >= 0 {
				return "uint"
			}
			return "int"
		}
		return "float"
	case []interface{}:
		if len(t) == 0 {
			return "[]string"
		}
		inner := inferJSONType(t[0])
		if strings.HasPrefix(inner, "[") {
			return "[]" + inner
		}
		return "[]" + inner
	case map[string]interface{}:
		return "{}f16"
	default:
		return "string"
	}
}

// UpdateSchema replaces the stored attribute schema for a namespace.
func (s *Store) UpdateSchema(ctx context.Context, namespace string, schema map[string]interface{}) error {
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return err
	}
	if schema == nil {
		schema = map[string]interface{}{}
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}
	sysTable := SQLIdent(GetSystemTableName(s.prefix, "namespaces"))
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET attr_schema = $2, updated_at = NOW() WHERE namespace = $1`, sysTable),
		namespace, raw)
	if err != nil {
		return fmt.Errorf("update schema: %w", err)
	}
	return s.applySchemaIndexes(ctx, namespace, schema)
}
