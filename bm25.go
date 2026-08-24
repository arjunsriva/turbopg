package turbopg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const defaultTextConfig = "english"

// GetBM25IndexName returns a PostgreSQL identifier for a BM25 expression index
// on a JSONB attribute.
func GetBM25IndexName(table, field string) string {
	sum := sha256.Sum256([]byte(table + "\x00" + field))
	hash := hex.EncodeToString(sum[:8])
	suffix := "_bm25_" + hash
	keep := postgresIdentMax - len(suffix)
	prefix := table
	if keep < 1 {
		trimmed := "b" + hash
		if len(trimmed) > postgresIdentMax {
			return trimmed[:postgresIdentMax]
		}
		return trimmed
	}
	if len(prefix) > keep {
		prefix = prefix[:keep]
	}
	return prefix + suffix
}

func GetFilterIndexName(table, field string) string {
	sum := sha256.Sum256([]byte(table + "\x00filt\x00" + field))
	hash := hex.EncodeToString(sum[:8])
	suffix := "_f_" + hash
	keep := postgresIdentMax - len(suffix)
	prefix := table
	if keep < 1 {
		trimmed := "f" + hash
		if len(trimmed) > postgresIdentMax {
			return trimmed[:postgresIdentMax]
		}
		return trimmed
	}
	if len(prefix) > keep {
		prefix = prefix[:keep]
	}
	return prefix + suffix
}

func jsonTextExpr(field string) string {
	if field == "id" {
		return "id"
	}
	return fmt.Sprintf("(attributes->>'%s')", escapeJSONKey(field))
}

func postgresTextConfig(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "english":
		return "english"
	case "simple":
		return "simple"
	case "arabic", "danish", "dutch", "finnish", "french", "german", "greek",
		"hungarian", "italian", "norwegian", "portuguese", "romanian", "russian",
		"spanish", "swedish", "tamil", "turkish":
		return strings.ToLower(name)
	default:
		return defaultTextConfig
	}
}

// HasTextSearch reports whether pg_textsearch is installed in this database.
func (s *Store) HasTextSearch(ctx context.Context) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_textsearch')`).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (s *Store) requireTextSearch(ctx context.Context) error {
	ok, err := s.HasTextSearch(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTextSearchUnavailable
	}
	return nil
}

// EnsureBM25Index creates a pg_textsearch BM25 index on attributes->>field.
func (s *Store) EnsureBM25Index(ctx context.Context, namespace, field, textConfig string) error {
	if err := s.requireTextSearch(ctx); err != nil {
		return err
	}
	if field == "" || field == "id" || field == "vector" {
		return fmt.Errorf("invalid BM25 field %q", field)
	}
	if err := ValidateNamespace(namespace); err != nil {
		return err
	}
	table := GetNamespaceTableName(s.prefix, namespace)
	idx := GetBM25IndexName(table, field)
	cfg := postgresTextConfig(textConfig)
	sql := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s USING bm25 (%s) WITH (text_config=%s)`,
		SQLIdent(idx), SQLIdent(table), jsonTextExpr(field), quoteStringLiteral(cfg),
	)
	if _, err := s.db.ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("create bm25 index on %s: %w", field, err)
	}
	return nil
}

func quoteStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

type bm25IndexOpts struct {
	enabled    bool
	textConfig string
}

func parseFTSSchema(def interface{}) bm25IndexOpts {
	switch t := def.(type) {
	case bool:
		return bm25IndexOpts{enabled: t, textConfig: defaultTextConfig}
	case map[string]interface{}:
		return parseFTSValue(ParseAttributeDef(t).FullTextSearch)
	default:
		return bm25IndexOpts{}
	}
}

func parseFTSValue(fts interface{}) bm25IndexOpts {
	if fts == nil {
		return bm25IndexOpts{}
	}
	opts := bm25IndexOpts{enabled: true, textConfig: defaultTextConfig}
	switch cfg := fts.(type) {
	case bool:
		opts.enabled = cfg
	case map[string]interface{}:
		if lang, _ := cfg["language"].(string); lang != "" {
			opts.textConfig = postgresTextConfig(lang)
		}
		if stemming, ok := cfg["stemming"].(bool); ok && !stemming {
			opts.textConfig = "simple"
		}
	}
	return opts
}

func schemaFilterable(def interface{}) bool {
	return ParseAttributeDef(def).IsFilterable(def)
}

func (s *Store) applySchemaIndexes(ctx context.Context, namespace string, schema map[string]interface{}) error {
	table := GetNamespaceTableName(s.prefix, namespace)
	quotedTable := SQLIdent(table)
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return err
	}
	if err := s.ensureExtraVectorColumns(ctx, namespace, schema); err != nil {
		return err
	}
	for field, def := range schema {
		if field == "id" || field == "vector" {
			continue
		}
		fts := parseFTSSchema(def)
		if fts.enabled {
			if err := s.EnsureBM25Index(ctx, namespace, field, fts.textConfig); err != nil {
				return err
			}
		}
		parsed := ParseAttributeDef(def)
		if parsed.Regex || parsed.Glob {
			if err := s.ensureTrigramIndex(ctx, table, quotedTable, field); err != nil {
				return err
			}
		}
		if schemaFilterable(def) {
			idx := GetFilterIndexName(table, field)
			sql := fmt.Sprintf(
				`CREATE INDEX IF NOT EXISTS %s ON %s (%s)`,
				SQLIdent(idx), quotedTable, jsonTextExpr(field),
			)
			if _, err := s.db.ExecContext(ctx, sql); err != nil {
				return fmt.Errorf("create filter index on %s: %w", field, err)
			}
		}
	}
	return nil
}

func (s *Store) ensureTrigramIndex(ctx context.Context, table, quotedTable, field string) error {
	if _, err := s.db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
		return fmt.Errorf("pg_trgm: %w", err)
	}
	idx := GetFilterIndexName(table, field+"_trgm")
	sql := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s USING gin (%s gin_trgm_ops)`,
		SQLIdent(idx), quotedTable, jsonTextExpr(field),
	)
	if _, err := s.db.ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("create trigram index on %s: %w", field, err)
	}
	return nil
}
