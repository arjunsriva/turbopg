package turbopg

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// QueryOptions represents options for querying documents
type QueryOptions struct {
	// Namespace to query
	Namespace string

	// Optional: Vector to search by similarity
	// Note: When combining vector search with filters, documents with zero
	// similarity to the query vector may be excluded from results, even if
	// they match the filter conditions. This is an optimization by the
	// underlying pgvector implementation.
	Vector []float32

	// Optional: Filter to apply
	Filter Filter

	// Number of results to return
	TopK int

	// Optional: Distance metric to use for vector search
	// Default: "cosine"
	Metric string
}


// QueryResult represents a single search result with its score
type QueryResult struct {
	// Document that matched the query
	Document Document

	// Score represents the similarity/distance score
	// Lower is better for distance metrics (L2)
	// Higher is better for similarity metrics (cosine)
	Score float64
}




// SearchVector finds the top-K most similar vectors in a namespace
func (s *Store) SearchVector(ctx context.Context, namespace string, vector []float32, topK int, metric string) ([]QueryResult, error) {
	// Validate namespace
	ns, err := s.GetNamespace(ctx, namespace)
	if err != nil {
		return nil, err
	}

	// Validate vector dimensions
	if len(vector) != ns.Dimensions {
		return nil, fmt.Errorf("vector dimensions mismatch: got %d, want %d", len(vector), ns.Dimensions)
	}

	// Validate topK
	if topK <= 0 {
		return nil, fmt.Errorf("topK must be positive, got %d", topK)
	}

	// Choose operator based on metric
	var operator string
	switch metric {
	case "cosine":
		operator = "<->"
	case "euclidean":
		operator = "<->"
	case "euclidean_squared":
		operator = "<#>"
	default:
		return nil, fmt.Errorf("unsupported distance metric: %s", metric)
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, namespace)

	// Build query
	query := fmt.Sprintf(`
		SELECT id, vector, attributes, (vector %s $1) as distance
		FROM %s
		ORDER BY vector %s $1
		LIMIT $2`,
		operator, tableName, operator)

	// Convert vector to string format that pgvector expects: [1,2,3]
	vectorStr := fmt.Sprintf("[%s]", joinFloat32s(vector, ","))

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, vectorStr, topK)
	if err != nil {
		return nil, fmt.Errorf("execute search: %w", err)
	}
	defer rows.Close()

	// Parse results
	var results []QueryResult
	for rows.Next() {
		var (
			doc       Document
			vectorStr string
			attrsJSON []byte
			distance  float64
		)

		err := rows.Scan(&doc.ID, &vectorStr, &attrsJSON, &distance)
		if err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}

		// Parse vector string back to []float32
		// Remove brackets and split by comma
		vectorStr = strings.Trim(vectorStr, "[]")
		if vectorStr != "" {
			parts := strings.Split(vectorStr, ",")
			doc.Vector = make([]float32, len(parts))
			for i, p := range parts {
				val, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
				if err != nil {
					return nil, fmt.Errorf("parse vector value: %w", err)
				}
				doc.Vector[i] = float32(val)
			}
		}

		// Parse attributes JSON
		if err := json.Unmarshal(attrsJSON, &doc.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes: %w", err)
		}

		results = append(results, QueryResult{
			Document: doc,
			Score:    distance,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate results: %w", err)
	}

	s.logger.Info("vector search completed",
		Field{Key: "namespace", Value: namespace},
		Field{Key: "top_k", Value: topK},
		Field{Key: "metric", Value: metric},
		Field{Key: "results", Value: len(results)},
	)

	return results, nil
}

// SearchFiltered finds the top-K most similar vectors that match the given filter
func (s *Store) SearchFiltered(ctx context.Context, namespace string, vector []float32, filter FilterCondition, topK int, metric string) ([]QueryResult, error) {
	return s.Query(ctx, QueryOptions{
		Namespace: namespace,
		Vector:    vector,
		Filter:    filter,
		TopK:      topK,
		Metric:    metric,
	})
}


// Query searches for documents based on the provided options
func (s *Store) Query(ctx context.Context, opts QueryOptions) ([]QueryResult, error) {
	// Validate namespace
	ns, err := s.GetNamespace(ctx, opts.Namespace)
	if err != nil {
		return nil, err
	}

	// Validate vector dimensions if provided
	if opts.Vector != nil && len(opts.Vector) != ns.Dimensions {
		return nil, fmt.Errorf("vector dimensions mismatch: got %d, want %d", len(opts.Vector), ns.Dimensions)
	}

	// Validate topK
	if opts.TopK <= 0 {
		return nil, fmt.Errorf("topK must be positive, got %d", opts.TopK)
	}

	// Use default metric if not specified
	metric := opts.Metric
	if metric == "" {
		metric = "cosine"
	}

	// Choose operator based on metric
	var operator string
	switch metric {
	case "cosine":
		operator = "<=>"
	case "euclidean":
		operator = "<->"
	case "euclidean_squared":
		operator = "<#>"
	default:
		return nil, fmt.Errorf("unsupported distance metric: %s", metric)
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, opts.Namespace)

	// Build query
	var queryBuilder strings.Builder
	var distanceExpr string
	if opts.Vector != nil {
		distanceExpr = fmt.Sprintf("(vector %s $1) as distance", operator)
	} else {
		distanceExpr = "0 as distance"
	}
	queryBuilder.WriteString(fmt.Sprintf(`
		SELECT id, vector, attributes, %s
		FROM %s`,
		distanceExpr,
		tableName))

	// Add WHERE clause if filter provided
	var args []interface{}
	var argOffset int

	if opts.Vector != nil {
		vectorStr := fmt.Sprintf("[%s]", joinFloat32s(opts.Vector, ","))
		args = append(args, vectorStr)
		argOffset = 1
		s.logger.Info("added vector argument",
			Field{Key: "vector", Value: vectorStr},
			Field{Key: "argOffset", Value: argOffset},
		)
	}

	if opts.Filter != nil {
		whereClause, filterArgs, err := s.buildFilterCondition(opts.Filter)
		if err != nil {
			return nil, fmt.Errorf("build filter: %w", err)
		}
		if whereClause != "" {
			s.logger.Info("built filter condition",
				Field{Key: "whereClause", Value: whereClause},
				Field{Key: "filterArgs", Value: fmt.Sprintf("%v", filterArgs)},
				Field{Key: "argOffset", Value: argOffset},
			)
			// Adjust placeholders for accumulated args
			for i := 1; i <= strings.Count(whereClause, "$"); i++ {
				old := fmt.Sprintf("$%d", i)
				new := fmt.Sprintf("$%d", i+argOffset)
				whereClause = strings.Replace(whereClause, old, new, -1)
			}
			queryBuilder.WriteString("\nWHERE " + whereClause)
			args = append(args, filterArgs...)
			s.logger.Info("adjusted filter placeholders",
				Field{Key: "whereClause", Value: whereClause},
				Field{Key: "args", Value: fmt.Sprintf("%v", args)},
			)
		}
	}

	// Add ORDER BY if vector search
	if opts.Vector != nil {
		queryBuilder.WriteString(fmt.Sprintf("\nORDER BY vector %s $1", operator))
	}

	// Add LIMIT
	queryBuilder.WriteString(fmt.Sprintf("\nLIMIT $%d", len(args)+1))
	args = append(args, opts.TopK)

	// Execute query
	s.logger.Info("executing query",
		Field{Key: "query", Value: queryBuilder.String()},
		Field{Key: "args", Value: fmt.Sprintf("%v", args)},
	)
	rows, err := s.db.QueryContext(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()

	// Parse results
	var results []QueryResult
	for rows.Next() {
		var (
			doc       Document
			vectorStr string
			attrsJSON []byte
			distance  float64
		)

		err := rows.Scan(&doc.ID, &vectorStr, &attrsJSON, &distance)
		if err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}

		// Parse vector
		vectorStr = strings.Trim(vectorStr, "[]")
		if vectorStr != "" {
			parts := strings.Split(vectorStr, ",")
			doc.Vector = make([]float32, len(parts))
			for i, p := range parts {
				val, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
				if err != nil {
					return nil, fmt.Errorf("parse vector value: %w", err)
				}
				doc.Vector[i] = float32(val)
			}
		}

		// Parse attributes
		if err := json.Unmarshal(attrsJSON, &doc.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes: %w", err)
		}

		results = append(results, QueryResult{
			Document: doc,
			Score:    distance,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate results: %w", err)
	}

	s.logger.Info("query completed",
		Field{Key: "namespace", Value: opts.Namespace},
		Field{Key: "top_k", Value: opts.TopK},
		Field{Key: "has_vector", Value: opts.Vector != nil},
		Field{Key: "has_filter", Value: opts.Filter != nil},
		Field{Key: "results", Value: len(results)},
	)

	return results, nil
}
