package turbopg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arjunsriva/turbopg/internal/validation"
	"github.com/lib/pq"
)

// DocumentID is the unique identifier for a document
type DocumentID string

// Document represents a single document with its vector and attributes
type Document struct {
	// ID is the unique identifier for this document
	ID DocumentID

	// Vector is the embedding for this document
	Vector []float32

	// Attributes is a map of arbitrary metadata associated with this document
	Attributes map[string]interface{}
}

// UpsertOptions holds options for document upsert operations
type UpsertOptions struct {
	// Namespace to upsert documents into
	Namespace string

	// Optional: Skip validation of vectors and attributes
	// Default: false (do validation)
	SkipValidation bool
}

// BatchUpsertOptions holds options for batch document upsert operations
type BatchUpsertOptions struct {
	UpsertOptions

	// Optional: Maximum number of documents per batch
	// Default: 1000
	BatchSize int
}

// Default batch settings
const (
	DefaultBatchSize = 1000
	MaxBatchSize     = 5000
)

// validateDocument checks if a document is valid
func validateDocument(doc *Document, dimensions int) error {
	if doc.ID == "" {
		return ErrEmptyDocumentID
	}

	if len(doc.Vector) != dimensions {
		return ErrInvalidVectorDimensions
	}

	if doc.Attributes == nil {
		doc.Attributes = make(map[string]interface{})
	}

	return nil
}

// Upsert inserts or updates a batch of documents in a namespace
func (s *Store) Upsert(ctx context.Context, docs []Document, opts UpsertOptions) error {
	// Validate namespace
	ns, err := s.GetNamespace(ctx, opts.Namespace)
	if err != nil {
		return err
	}

	// Validate documents unless explicitly skipped
	if !opts.SkipValidation {
		for i := range docs {
			if err := validateDocument(&docs[i], ns.Dimensions); err != nil {
				return err
			}
		}
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, opts.Namespace)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			s.logger.Error("failed to rollback transaction", Field{Key: "error", Value: err.Error()})
		}
	}()

	// Prepare upsert statement
	stmt := fmt.Sprintf(`
		INSERT INTO %s (id, vector, attributes)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET
			vector = EXCLUDED.vector,
			attributes = EXCLUDED.attributes`,
		tableName)

	// Execute upserts
	for _, doc := range docs {
		// Convert vector to string format that pgvector expects: [1,2,3]
		vectorStr := fmt.Sprintf("[%s]", joinFloat32s(doc.Vector, ","))

		// Convert attributes to JSON
		attrsJSON, err := json.Marshal(doc.Attributes)
		if err != nil {
			return fmt.Errorf("marshal attributes: %w", err)
		}

		// Execute upsert
		_, err = tx.ExecContext(ctx, stmt, doc.ID, vectorStr, attrsJSON)
		if err != nil {
			return fmt.Errorf("upsert document: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("upserted documents",
		Field{Key: "namespace", Value: opts.Namespace},
		Field{Key: "count", Value: len(docs)},
	)

	return nil
}

// joinFloat32s joins float32 values with a separator
func joinFloat32s(values []float32, sep string) string {
	if len(values) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%g", values[0]))
	for _, v := range values[1:] {
		b.WriteString(sep)
		b.WriteString(fmt.Sprintf("%g", v))
	}
	return b.String()
}

// UpsertBatch inserts or updates documents in batches
func (s *Store) UpsertBatch(ctx context.Context, docs []Document, opts BatchUpsertOptions) error {
	if len(docs) == 0 {
		return nil
	}

	// Use default batch size if not specified
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize > MaxBatchSize {
		batchSize = MaxBatchSize
	}

	// Validate namespace once for all batches
	ns, err := s.GetNamespace(ctx, opts.Namespace)
	if err != nil {
		return err
	}

	// Build table name
	tableName := GetNamespaceTableName(s.prefix, opts.Namespace)

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

	// Prepare statement once for all batches
	stmt := fmt.Sprintf(`
		INSERT INTO %s (id, vector, attributes)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET
			vector = EXCLUDED.vector,
			attributes = EXCLUDED.attributes`,
		tableName)

	preparedStmt, err := tx.PrepareContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer preparedStmt.Close()

	// Process documents in batches
	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		// Validate batch unless explicitly skipped
		if !opts.SkipValidation {
			for j := range batch {
				if err := validateDocument(&batch[j], ns.Dimensions); err != nil {
					return fmt.Errorf("validate document at index %d: %w", i+j, err)
				}
			}
		}

		// Execute batch
		for _, doc := range batch {
			// Convert vector to string format that pgvector expects: [1,2,3]
			vectorStr := fmt.Sprintf("[%s]", joinFloat32s(doc.Vector, ","))

			// Convert attributes to JSON
			attrsJSON, err := json.Marshal(doc.Attributes)
			if err != nil {
				return fmt.Errorf("marshal attributes: %w", err)
			}

			// Execute upsert using prepared statement
			_, err = preparedStmt.ExecContext(ctx, doc.ID, vectorStr, attrsJSON)
			if err != nil {
				return fmt.Errorf("upsert document: %w", err)
			}
		}

		s.logger.Info("upserted batch",
			Field{Key: "namespace", Value: opts.Namespace},
			Field{Key: "batch_size", Value: len(batch)},
			Field{Key: "progress", Value: fmt.Sprintf("%d/%d", end, len(docs))},
		)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("completed batch upsert",
		Field{Key: "namespace", Value: opts.Namespace},
		Field{Key: "total_documents", Value: len(docs)},
	)

	return nil
}

// Delete removes documents by their IDs from a namespace
func (s *Store) Delete(ctx context.Context, namespace string, ids []DocumentID) error {
	// Validate namespace
	if err := validation.ValidateNamespace(namespace); err != nil {
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

// Filter represents a condition for filtering documents
type Filter struct {
	// Field is the name of the attribute field to filter on
	Field string
	// Op is the comparison operator (e.g., "=", ">", "<", ">=", "<=", "!=", "LIKE", "IN")
	Op string
	// Value is the value to compare against
	Value interface{}
}

// DeleteByFilter removes documents that match the given filter from a namespace
func (s *Store) DeleteByFilter(ctx context.Context, namespace string, filter Filter) error {
	// Validate namespace
	if err := validation.ValidateNamespace(namespace); err != nil {
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
	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE attributes->>'%s' %s $1`,
		tableName,
		filter.Field,
		filter.Op)

	// Execute delete
	result, err := tx.ExecContext(ctx, query, filter.Value)
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
