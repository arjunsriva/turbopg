package turbopg

// DocumentID is the unique identifier for a document
type DocumentID string

// Document represents a single document with its vector and attributes
type Document struct {
	// ID is the unique identifier for this document
	ID DocumentID

	// Vector is the embedding for this document
	Vector []float32

	// ExtraVectors are named pgvector columns (schema embed destinations other
	// than the primary vector column).
	ExtraVectors map[string][]float32

	// Attributes is a map of arbitrary metadata associated with this document
	Attributes map[string]interface{}
}
