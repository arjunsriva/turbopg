package turbopg

import (
	"context"
)

// UpsertOptions holds options for document upsert operations
type UpsertOptions struct {
	// Namespace to upsert documents into
	Namespace string

	// Optional: Skip validation of vectors and attributes
	// Default: false (do validation)
	SkipValidation bool

	// Optional: TurboPuffer upsert_condition evaluated against the current document.
	Condition Filter
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

	if dimensions == 0 {
		if len(doc.Vector) > 0 {
			return ErrInvalidVectorDimensions
		}
	} else if len(doc.Vector) != dimensions {
		return ErrInvalidVectorDimensions
	}

	if doc.Attributes == nil {
		doc.Attributes = make(map[string]interface{})
	}

	return nil
}

// Upsert inserts or updates a batch of documents in a namespace
func (s *Store) Upsert(ctx context.Context, docs []Document, opts UpsertOptions) error {
	_, err := s.Write(ctx, Write{
		Namespace:       opts.Namespace,
		Upserts:         docs,
		UpsertCondition: opts.Condition,
		SkipValidation:  opts.SkipValidation,
	})
	return err
}

// UpsertBatch inserts or updates documents. BatchSize is accepted for
// compatibility; all documents run in one Store.Write transaction.
func (s *Store) UpsertBatch(ctx context.Context, docs []Document, opts BatchUpsertOptions) error {
	_, err := s.Write(ctx, Write{
		Namespace:       opts.Namespace,
		Upserts:         docs,
		UpsertCondition: opts.Condition,
		SkipValidation:  opts.SkipValidation,
	})
	return err
}
