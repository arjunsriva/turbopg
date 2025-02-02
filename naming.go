package turbopg

import (
	"fmt"
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
