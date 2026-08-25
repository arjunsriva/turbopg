package turbopg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Table prefix constants
const (
	SystemPrefix    = "sys_"
	NamespacePrefix = "ns_"

	// MaxNamespaceLength matches TurboPuffer's documented namespace name limit.
	MaxNamespaceLength = 128
	// postgresIdentMax is NAMEDATALEN-1 on default PostgreSQL builds.
	postgresIdentMax = 63
)

// GetSystemTableName returns the fully qualified name for a system table
// Example: prefix="myapp_" table="migration_requests" -> "myapp_sys_migration_requests"
func GetSystemTableName(prefix, table string) string {
	return fmt.Sprintf("%s%s%s", prefix, SystemPrefix, table)
}

// GetNamespaceTableName returns the fully qualified name for a namespace table
// Example: prefix="myapp_" namespace="documents" -> "myapp_ns_documents"
func GetNamespaceTableName(prefix, namespace string) string {
	name := fmt.Sprintf("%s%s%s", prefix, NamespacePrefix, namespace)
	if utf8.RuneCountInString(name) <= postgresIdentMax {
		return name
	}
	sum := sha256.Sum256([]byte(namespace))
	hash := hex.EncodeToString(sum[:8])
	base := prefix + NamespacePrefix
	keep := postgresIdentMax - len(base) - 1 - len(hash)
	if keep < 0 {
		trimmed := (base + hash)
		if len(trimmed) > postgresIdentMax {
			return trimmed[:postgresIdentMax]
		}
		return trimmed
	}
	ns := namespace
	if len(ns) > keep {
		ns = ns[:keep]
	}
	return base + ns + "_" + hash
}

// SQLIdent quotes a PostgreSQL identifier so names with hyphens or mixed case
// can be used safely in SQL.
func SQLIdent(name string) string {
	return pq.QuoteIdentifier(name)
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

// ValidateNamespace checks whether a namespace name is valid for TurboPuffer-compatible
// use: [A-Za-z0-9-_.]{1,128}, with a few reserved prefixes blocked.
func ValidateNamespace(namespace string) error {
	if len(namespace) == 0 {
		return ErrEmptyNamespace
	}
	if utf8.RuneCountInString(namespace) > MaxNamespaceLength {
		return ErrNamespaceTooLong
	}

	for i, r := range namespace {
		if i == 0 && !(isLetter(r) || isNumber(r) || r == '_') {
			return ErrInvalidNamespaceStart
		}
		if !isLetter(r) && !isNumber(r) && r != '_' && r != '-' && r != '.' {
			return ErrInvalidNamespaceChar
		}
	}

	reservedPrefixes := []string{
		"pg_",
		"vector_",
	}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(namespace, prefix) {
			return ErrReservedNamespace
		}
	}

	return nil
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isNumber(r rune) bool {
	return r >= '0' && r <= '9'
}
