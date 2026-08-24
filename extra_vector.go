package turbopg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type extraVecCol struct {
	dest  string
	field string
	col   string
}

// GetVectorColumnName returns the extra pgvector column for a named vector attribute.
func GetVectorColumnName(field string) string {
	sum := sha256.Sum256([]byte("vec\x00" + field))
	hash := hex.EncodeToString(sum[:8])
	suffix := "_x" + hash
	keep := postgresIdentMax - len(suffix)
	prefix := "v_" + field
	prefix = strings.Map(func(r rune) rune {
		if isLetter(r) || isNumber(r) || r == '_' {
			return r
		}
		return '_'
	}, prefix)
	if keep < 1 {
		return ("v" + hash)[:postgresIdentMax]
	}
	if len(prefix) > keep {
		prefix = prefix[:keep]
	}
	return prefix + suffix
}

func extraVectorFields(schema map[string]interface{}) []EmbedSpec {
	var out []EmbedSpec
	for _, spec := range EmbedSpecs(schema) {
		if spec.ComputedAttr() == "" {
			continue
		}
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

func extraVectorCols(schema map[string]interface{}) []extraVecCol {
	fields := extraVectorFields(schema)
	out := make([]extraVecCol, 0, len(fields))
	for _, spec := range fields {
		dest := spec.ComputedAttr()
		out = append(out, extraVecCol{
			dest:  dest,
			field: spec.Field,
			col:   GetVectorColumnName(dest),
		})
	}
	return out
}

func extraVectorSelectSQL(schema map[string]interface{}) (string, []extraVecCol) {
	cols := extraVectorCols(schema)
	if len(cols) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		parts = append(parts, SQLIdent(c.col))
	}
	return ", " + strings.Join(parts, ", "), cols
}

// vectorColumnSQL is the quoted pgvector column used for ANN on field.
// Primary "vector" stays the table's vector column; extra embed destinations
// use the hashed column named from ComputedAttr, not the schema field key.
func vectorColumnSQL(schema map[string]interface{}, field string) string {
	if field == "" || field == "vector" {
		return "vector"
	}
	for _, spec := range extraVectorFields(schema) {
		if spec.Field == field || spec.ComputedAttr() == field {
			return SQLIdent(GetVectorColumnName(spec.ComputedAttr()))
		}
	}
	return SQLIdent(GetVectorColumnName(field))
}

func (s *Store) ensureExtraVectorColumns(ctx context.Context, namespace string, schema map[string]interface{}) error {
	extras := extraVectorFields(schema)
	if len(extras) > MaxEmbedFields {
		return InvalidInputf("at most %d embedded attributes are supported", MaxEmbedFields)
	}
	table := GetNamespaceTableName(s.prefix, namespace)
	quotedTable := SQLIdent(table)
	for _, spec := range extras {
		dims := spec.Dims
		if dims <= 0 {
			return InvalidInputf("embedded attribute %q requires embed.dims", spec.Field)
		}
		col := GetVectorColumnName(spec.ComputedAttr())
		quotedCol := SQLIdent(col)
		sql := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s vector(%d)`,
			quotedTable, quotedCol, dims)
		if _, err := s.db.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("add vector column %s: %w", col, err)
		}
	}
	return s.ensureVectorIndexes(ctx, namespace)
}

func extraVectorDests(schema map[string]interface{}) []string {
	seen := map[string]struct{}{}
	var dests []string
	for _, spec := range extraVectorFields(schema) {
		dest := spec.ComputedAttr()
		if dest == "" {
			continue
		}
		if _, ok := seen[dest]; ok {
			continue
		}
		seen[dest] = struct{}{}
		dests = append(dests, dest)
	}
	return dests
}

func ivfOperator(metric string) string {
	if metric == "euclidean_squared" {
		return "vector_l2_ops"
	}
	return "vector_cosine_ops"
}

func (s *Store) ensureVectorIndexes(ctx context.Context, namespace string) error {
	ns, err := s.GetNamespace(ctx, namespace)
	if err != nil {
		return err
	}
	table := GetNamespaceTableName(s.prefix, namespace)
	quotedTable := SQLIdent(table)
	lists := 1
	metric := "cosine_distance"
	if ns.IndexConfig != nil {
		if ns.IndexConfig.Lists > 0 {
			lists = ns.IndexConfig.Lists
		}
		if ns.IndexConfig.DistanceMetric != "" {
			metric = ns.IndexConfig.DistanceMetric
		}
	}
	op := ivfOperator(metric)
	if ns.Dimensions > 0 {
		if err := s.ensureIVFFlatIndex(ctx, quotedTable, SQLIdent(table+"_vector_idx"), "vector", op, lists); err != nil {
			return err
		}
	}
	for _, spec := range extraVectorFields(ns.Schema) {
		col := GetVectorColumnName(spec.ComputedAttr())
		if err := s.ensureIVFFlatIndex(ctx, quotedTable, SQLIdent(col+"_idx"), SQLIdent(col), op, lists); err != nil {
			return err
		}
	}
	return nil
}

// ensureIVFFlatIndex creates an IVFFlat index once the table has at least
// `lists` rows. pgvector refuses to build the index on an empty table, and
// the HTTP server default of 100 lists would otherwise fail namespace create.
func (s *Store) ensureIVFFlatIndex(ctx context.Context, quotedTable, quotedIndex, quotedCol, operator string, lists int) error {
	if lists < 1 {
		lists = 1
	}
	var n int64
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, quotedTable)).Scan(&n); err != nil {
		return fmt.Errorf("count for ivfflat: %w", err)
	}
	if n < int64(lists) && lists > 1 {
		return nil
	}
	idxSQL := fmt.Sprintf(`
			CREATE INDEX IF NOT EXISTS %s ON %s USING ivfflat (%s %s) WITH (lists = %d)`,
		quotedIndex, quotedTable, quotedCol, operator, lists)
	if _, err := s.db.ExecContext(ctx, idxSQL); err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "lists") || strings.Contains(msg, "ivfflat") {
			s.logger.Debug("ivfflat index deferred",
				Field{Key: "index", Value: quotedIndex},
				Field{Key: "error", Value: err.Error()},
			)
			return nil
		}
		return fmt.Errorf("index %s: %w", quotedIndex, err)
	}
	return nil
}
