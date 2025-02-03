package turbopg

import "errors"

var (
	// Document errors
	ErrEmptyDocumentID         = errors.New("document ID cannot be empty")
	ErrInvalidVectorDimensions = errors.New("vector dimensions do not match namespace configuration")
)


// NamespaceError represents an error related to namespace validation
type NamespaceError string

func (e NamespaceError) Error() string { return string(e) }

const (
	ErrEmptyNamespace        = NamespaceError("namespace cannot be empty")
	ErrNamespaceTooLong      = NamespaceError("namespace cannot exceed 63 characters")
	ErrInvalidNamespaceStart = NamespaceError("namespace must start with letter or underscore")
	ErrInvalidNamespaceChar  = NamespaceError("namespace can only contain letters, numbers, and underscores")
	ErrReservedNamespace     = NamespaceError("namespace cannot start with reserved prefix")
)
