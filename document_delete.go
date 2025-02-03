package turbopg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

// Delete removes documents by their IDs from a namespace
func (s *Store) Delete(ctx context.Context, namespace string, ids []DocumentID) error {
	// Validate namespace
	if err := ValidateNamespace(namespace); err != nil {
		return fmt.Errorf("invalid namespace name: %w", err)
	}

	// Check if namespace exists
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return err
	}

	// Nothing to do if no IDs provided
	if len(ids) == 0 {
		return nil
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, namespace)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			s.logger.Error("failed to rollback transaction", Field{Key: "error", Value: err.Error()})
		}
	}()

	// Build delete query with multiple IDs
	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE id = ANY($1)`,
		tableName)

	// Convert []DocumentID to []string for postgres ANY
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = string(id)
	}

	// Execute delete
	result, err := tx.ExecContext(ctx, query, pq.Array(strIDs))
	if err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}

	// Get number of deleted rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("deleted documents",
		Field{Key: "namespace", Value: namespace},
		Field{Key: "count", Value: rowsAffected},
	)

	return nil
}

// DeleteByFilter removes documents that match the given filter from a namespace
func (s *Store) DeleteByFilter(ctx context.Context, namespace string, filter FilterCondition) error {
	// Validate namespace
	if err := ValidateNamespace(namespace); err != nil {
		return fmt.Errorf("invalid namespace name: %w", err)
	}

	// Check if namespace exists
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return err
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, namespace)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			s.logger.Error("failed to rollback transaction", Field{Key: "error", Value: err.Error()})
		}
	}()

	// Build delete query with filter
	whereClause, args, err := s.buildSimpleFilterCondition(filter)
	if err != nil {
		return fmt.Errorf("build filter: %w", err)
	}

	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE %s`,
		tableName,
		whereClause)

	// Execute delete
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}

	// Get number of deleted rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("deleted documents by filter",
		Field{Key: "namespace", Value: namespace},
		Field{Key: "filter_field", Value: filter.Field},
		Field{Key: "filter_op", Value: filter.Op},
		Field{Key: "count", Value: rowsAffected},
	)

	return nil
}

