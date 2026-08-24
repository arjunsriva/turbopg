package turbopg

import (
	"errors"
	"fmt"
)

var (
	// Document errors
	ErrEmptyDocumentID         = errors.New("document ID cannot be empty")
	ErrInvalidVectorDimensions = errors.New("vector dimensions do not match namespace configuration")

	// Namespace errors
	ErrNamespaceNotFound = errors.New("namespace not found")

	// ErrTextSearchUnavailable is returned when BM25 ranking is requested but
	// the pg_textsearch extension is not installed (PostgreSQL 17+ required).
	ErrTextSearchUnavailable = errors.New("BM25 search requires the pg_textsearch extension (PostgreSQL 17+). See https://github.com/timescale/pg_textsearch")

	// ErrDuplicateWriteID is returned when the same document ID appears more
	// than once in a single Write (upserts, patches, or deletes).
	ErrDuplicateWriteID = errors.New("duplicate document id in write request")
)

// NamespaceError represents an error related to namespace validation
type NamespaceError string

func (e NamespaceError) Error() string { return string(e) }

const (
	ErrEmptyNamespace        = NamespaceError("namespace cannot be empty")
	ErrNamespaceTooLong      = NamespaceError("namespace cannot exceed 128 characters")
	ErrInvalidNamespaceStart = NamespaceError("namespace must start with a letter, number, or underscore")
	ErrInvalidNamespaceChar  = NamespaceError("namespace can only contain letters, numbers, underscores, hyphens, and dots")
	ErrReservedNamespace     = NamespaceError("namespace cannot start with reserved prefix")
)

// NamespaceNotFound returns an error that unwraps to ErrNamespaceNotFound.
func NamespaceNotFound(name string) error {
	return fmt.Errorf("namespace %q does not exist: %w", name, ErrNamespaceNotFound)
}

// IsNotFound reports whether err is a missing-namespace error.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNamespaceNotFound)
}

// InvalidInputError is a client-caused request error (HTTP 400).
type InvalidInputError struct {
	Msg string
	Err error
}

func (e *InvalidInputError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		if e.Msg != "" {
			return e.Msg + ": " + e.Err.Error()
		}
		return e.Err.Error()
	}
	return e.Msg
}

func (e *InvalidInputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// InvalidInput returns a client-caused error with a stable message.
func InvalidInput(msg string) error {
	return &InvalidInputError{Msg: msg}
}

// InvalidInputf returns a client-caused error with a formatted message.
func InvalidInputf(format string, args ...interface{}) error {
	return &InvalidInputError{Msg: fmt.Sprintf(format, args...)}
}

// IsInvalidInput reports whether err is a client-caused request error.
func IsInvalidInput(err error) bool {
	if err == nil {
		return false
	}
	var inv *InvalidInputError
	if errors.As(err, &inv) {
		return true
	}
	return errors.Is(err, ErrInvalidVectorDimensions) ||
		errors.Is(err, ErrDuplicateWriteID) ||
		errors.Is(err, ErrEmptyDocumentID)
}
