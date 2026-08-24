package turbopg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CountDocuments returns the number of documents in a namespace, optionally filtered.
func (s *Store) CountDocuments(ctx context.Context, namespace string, filter Filter) (int64, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return 0, fmt.Errorf("invalid namespace name: %w", err)
	}
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return 0, err
	}

	tableName := SQLIdent(GetNamespaceTableName(s.prefix, namespace))
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	var args []interface{}
	if filter != nil {
		where, filterArgs, err := buildFilterSQL(filter)
		if err != nil {
			return 0, fmt.Errorf("build filter: %w", err)
		}
		if where != "" {
			query += " WHERE " + where
			args = filterArgs
		}
	}

	var count int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count documents: %w", err)
	}
	return count, nil
}

// SumAttribute sums a numeric attribute across documents matching filter.
func (s *Store) SumAttribute(ctx context.Context, namespace, field string, filter Filter) (float64, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return 0, fmt.Errorf("invalid namespace name: %w", err)
	}
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return 0, err
	}
	if field == "" {
		return 0, fmt.Errorf("sum field is required")
	}

	tableName := SQLIdent(GetNamespaceTableName(s.prefix, namespace))
	col := filterColumn(field, true)
	query := fmt.Sprintf("SELECT COALESCE(SUM(%s), 0) FROM %s", col, tableName)
	var args []interface{}
	if filter != nil {
		where, filterArgs, err := buildFilterSQL(filter)
		if err != nil {
			return 0, fmt.Errorf("build filter: %w", err)
		}
		if where != "" {
			query += " WHERE " + where
			args = filterArgs
		}
	}

	var sum float64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&sum); err != nil {
		return 0, fmt.Errorf("sum attribute: %w", err)
	}
	return sum, nil
}

// AggregateSpec is a Count or Sum aggregation used with group_by.
type AggregateSpec struct {
	Label string
	Op    string
	Field string
}

// AggregateGrouped runs Count/Sum aggregations grouped by attribute fields.
func (s *Store) AggregateGrouped(ctx context.Context, namespace string, groupBy []string, filter Filter, specs []AggregateSpec) ([]map[string]interface{}, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return nil, fmt.Errorf("invalid namespace name: %w", err)
	}
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return nil, err
	}
	if len(groupBy) == 0 {
		return nil, fmt.Errorf("group_by requires at least one field")
	}
	if len(groupBy) > 8 {
		return nil, InvalidInput("group_by supports at most 8 fields")
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("aggregate_by is required")
	}

	tableName := SQLIdent(GetNamespaceTableName(s.prefix, namespace))
	selects := make([]string, 0, len(groupBy)+len(specs))
	for i, field := range groupBy {
		selects = append(selects, fmt.Sprintf("%s AS g%d", filterColumn(field, false), i))
	}
	for i, spec := range specs {
		switch spec.Op {
		case "Count":
			selects = append(selects, fmt.Sprintf("COUNT(*) AS a%d", i))
		case "Sum":
			selects = append(selects, fmt.Sprintf("COALESCE(SUM(%s), 0) AS a%d", filterColumn(spec.Field, true), i))
		default:
			return nil, fmt.Errorf("unsupported aggregation %q", spec.Op)
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(selects, ", "), tableName)
	var args []interface{}
	if filter != nil {
		where, filterArgs, err := buildFilterSQL(filter)
		if err != nil {
			return nil, fmt.Errorf("build filter: %w", err)
		}
		if where != "" {
			query += " WHERE " + where
			args = filterArgs
		}
	}
	groupCols := make([]string, len(groupBy))
	for i := range groupBy {
		groupCols[i] = fmt.Sprintf("%d", i+1)
	}
	query += " GROUP BY " + strings.Join(groupCols, ", ")

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("grouped aggregate: %w", err)
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		dest := make([]interface{}, len(groupBy)+len(specs))
		groupVals := make([]sql.NullString, len(groupBy))
		aggVals := make([]sql.NullFloat64, len(specs))
		for i := range groupBy {
			dest[i] = &groupVals[i]
		}
		for i := range specs {
			dest[len(groupBy)+i] = &aggVals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan aggregation group: %w", err)
		}
		row := map[string]interface{}{}
		for i, field := range groupBy {
			if groupVals[i].Valid {
				row[field] = groupVals[i].String
			} else {
				row[field] = nil
			}
		}
		for i, spec := range specs {
			if spec.Op == "Count" {
				row[spec.Label] = int64(aggVals[i].Float64)
			} else if aggVals[i].Valid {
				row[spec.Label] = aggVals[i].Float64
			} else {
				row[spec.Label] = 0
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
