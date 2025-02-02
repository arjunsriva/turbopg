package validation

import "strings"

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

// ValidateNamespace checks if a namespace name is valid according to PostgreSQL identifier rules
// and our additional constraints
func ValidateNamespace(namespace string) error {
	// 1. Length limits
	if len(namespace) == 0 {
		return ErrEmptyNamespace
	}
	if len(namespace) > 63 { // PostgreSQL identifier limit
		return ErrNamespaceTooLong
	}

	// 2. Character rules
	for i, r := range namespace {
		// Must start with letter or underscore
		if i == 0 && !(isLetter(r) || r == '_') {
			return ErrInvalidNamespaceStart
		}
		// Can only contain letters, numbers, underscore
		if !isLetter(r) && !isNumber(r) && r != '_' {
			return ErrInvalidNamespaceChar
		}
	}

	// 3. Reserved names
	reservedPrefixes := []string{
		"pg_",     // PostgreSQL system
		"vector_", // Our system tables
	}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(namespace, prefix) {
			return ErrReservedNamespace
		}
	}

	return nil
}

// Helper functions
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isNumber(r rune) bool {
	return r >= '0' && r <= '9'
}
