package turbopg

import (
	"fmt"
	"strings"
)

// Table prefix constants
const (
	SystemPrefix    = "sys_"
	NamespacePrefix = "ns_"
)

// GetSystemTableName returns the fully qualified name for a system table
// Example: prefix="myapp_" table="migration_requests" -> "myapp_sys_migration_requests"
func GetSystemTableName(prefix, table string) string {
	return fmt.Sprintf("%s%s%s", prefix, SystemPrefix, table)
}

// GetNamespaceTableName returns the fully qualified name for a namespace table
// Example: prefix="myapp_" namespace="documents" -> "myapp_ns_documents"
func GetNamespaceTableName(prefix, namespace string) string {
	return fmt.Sprintf("%s%s%s", prefix, NamespacePrefix, namespace)
}

// GetNamespaceFromTableName extracts the namespace name from a fully qualified table name
// Example: "myapp_ns_documents" -> "documents"
func GetNamespaceFromTableName(prefix, tableName string) (string, bool) {
	fullPrefix := prefix + NamespacePrefix
	if len(tableName) <= len(fullPrefix) {
		return "", false
	}
	if tableName[:len(fullPrefix)] != fullPrefix {
		return "", false
	}
	return tableName[len(fullPrefix):], true
}

// IsSystemTable checks if a table name is a system table
func IsSystemTable(prefix, tableName string) bool {
	fullPrefix := prefix + SystemPrefix
	return len(tableName) > len(fullPrefix) && tableName[:len(fullPrefix)] == fullPrefix
}





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
