package turbopg

import "errors"

var (
	// Document errors
	ErrEmptyDocumentID         = errors.New("document ID cannot be empty")
	ErrInvalidVectorDimensions = errors.New("vector dimensions do not match namespace configuration")
)
