package turbopg

import (
	"fmt"
	"unicode/utf8"
)

const (
	// MaxDocumentIDBytes is TurboPuffer's documented id length cap.
	MaxDocumentIDBytes = 64
	// MaxTopK is the maximum query limit / top_k.
	MaxTopK = 10_000
	// MaxMultiQueries is the maximum number of queries in one multi-query request.
	MaxMultiQueries = 16
	// MaxEmbedFields is the number of native embed attributes we materialize
	// as pgvector columns (hosted allows 4).
	MaxEmbedFields = 4
	// DefaultPatchByFilterMax matches hosted patch_by_filter (protects Postgres).
	DefaultPatchByFilterMax = 50_000
	// DefaultDeleteByFilterMax matches hosted delete_by_filter.
	DefaultDeleteByFilterMax = 5_000_000
	// DefaultIVFFlatProbes is pgvector's typical probes setting.
	DefaultIVFFlatProbes = 10
	// DefaultServerIVFFlatLists is the HTTP-server default for new namespaces.
	DefaultServerIVFFlatLists = 100
)

// ValidateDocumentID checks TurboPuffer id limits (non-empty, ≤64 bytes).
func ValidateDocumentID(id DocumentID) error {
	s := string(id)
	if s == "" {
		return ErrEmptyDocumentID
	}
	if len(s) > MaxDocumentIDBytes {
		return InvalidInputf("document id exceeds %d bytes", MaxDocumentIDBytes)
	}
	return nil
}

// ValidateAttributeName rejects names that would collide with computed fields.
func ValidateAttributeName(name string) error {
	if name == "" {
		return InvalidInput("attribute name cannot be empty")
	}
	if name[0] == '$' {
		return InvalidInputf("attribute name %q cannot start with $", name)
	}
	if utf8.RuneCountInString(name) > 128 {
		return InvalidInputf("attribute name %q is too long", name)
	}
	return nil
}

func validateWriteDocuments(docs []Document) error {
	for i := range docs {
		if err := ValidateDocumentID(docs[i].ID); err != nil {
			return err
		}
		for k := range docs[i].Attributes {
			if k == "id" || k == "vector" {
				continue
			}
			if err := ValidateAttributeName(k); err != nil {
				return fmt.Errorf("document %s: %w", docs[i].ID, err)
			}
		}
	}
	return nil
}
