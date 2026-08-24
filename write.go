package turbopg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// Write is one unit of document mutation. Store.Write runs it in a single
// Postgres transaction, in TurboPuffer mixed-write order:
// delete_by_filter, patch_by_filter, deletes, patches, upserts.
//
// Namespace create and schema/index DDL stay outside this transaction: they
// are catalog changes, and CREATE INDEX does not belong on the document path.
// Embedding and protocol parsing stay in the HTTP layer.
type Write struct {
	Namespace string

	Upserts         []Document
	UpsertCondition Filter

	Patches        []Document
	PatchCondition Filter

	PatchByFilter *PatchByFilterWrite

	Deletes         []DocumentID
	DeleteCondition Filter
	DeleteByFilter  Filter

	SkipValidation bool
}

// PatchByFilterWrite patches every document matching Filter.
type PatchByFilterWrite struct {
	Filter     Filter
	Attributes map[string]interface{}
	Vector     []float32
}

// WriteResult is the applied-row accounting for a Write.
type WriteResult struct {
	Upserted    int
	Patched     int
	Deleted     int
	UpsertedIDs []DocumentID
	PatchedIDs  []DocumentID
	DeletedIDs  []DocumentID
	// RowsRemaining is set when a filter write hit the operator cap.
	RowsRemaining int
}

func (r WriteResult) Affected() int {
	return r.Upserted + r.Patched + r.Deleted
}

func (w Write) empty() bool {
	return len(w.Upserts) == 0 &&
		len(w.Patches) == 0 &&
		len(w.Deletes) == 0 &&
		w.PatchByFilter == nil &&
		w.DeleteByFilter == nil
}

// Write applies a mixed document mutation atomically.
func (s *Store) Write(ctx context.Context, w Write) (WriteResult, error) {
	if err := ValidateNamespace(w.Namespace); err != nil {
		return WriteResult{}, fmt.Errorf("invalid namespace name: %w", err)
	}
	ns, err := s.GetNamespace(ctx, w.Namespace)
	if err != nil {
		return WriteResult{}, err
	}
	if err := duplicateWriteIDs(w); err != nil {
		return WriteResult{}, err
	}
	if w.PatchByFilter != nil && w.PatchByFilter.Filter == nil {
		return WriteResult{}, InvalidInput("patch_by_filter requires filters")
	}
	if !w.SkipValidation {
		if err := validateWriteDocuments(w.Upserts); err != nil {
			return WriteResult{}, err
		}
		if err := validateWriteDocuments(w.Patches); err != nil {
			return WriteResult{}, err
		}
		for _, id := range w.Deletes {
			if err := ValidateDocumentID(id); err != nil {
				return WriteResult{}, err
			}
		}
		for i := range w.Upserts {
			if err := validateDocument(&w.Upserts[i], ns.Dimensions); err != nil {
				return WriteResult{}, err
			}
		}
		for i := range w.Patches {
			if w.Patches[i].ID == "" {
				return WriteResult{}, ErrEmptyDocumentID
			}
		}
	}
	if err := coerceTypedAttributes(ns.Schema, w.Upserts); err != nil {
		return WriteResult{}, err
	}
	if err := coerceTypedAttributes(ns.Schema, w.Patches); err != nil {
		return WriteResult{}, err
	}
	if w.empty() {
		return WriteResult{}, nil
	}

	table := SQLIdent(GetNamespaceTableName(s.prefix, w.Namespace))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WriteResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			s.logger.Error("failed to rollback transaction", Field{Key: "error", Value: err.Error()})
		}
	}()

	var result WriteResult
	extraCols := extraVectorDests(ns.Schema)
	if w.DeleteByFilter != nil {
		ids, remaining, err := deleteByFilterTx(ctx, tx, table, w.DeleteByFilter, s.deleteByFilterMax)
		if err != nil {
			return WriteResult{}, err
		}
		result.DeletedIDs = append(result.DeletedIDs, ids...)
		result.RowsRemaining += remaining
	}
	if w.PatchByFilter != nil {
		ids, remaining, err := patchByFilterTx(ctx, tx, table, *w.PatchByFilter, s.patchByFilterMax)
		if err != nil {
			return WriteResult{}, err
		}
		result.PatchedIDs = append(result.PatchedIDs, ids...)
		result.RowsRemaining += remaining
	}
	if len(w.Deletes) > 0 {
		ids, err := deleteIDsTx(ctx, tx, table, w.Deletes, w.DeleteCondition)
		if err != nil {
			return WriteResult{}, err
		}
		result.DeletedIDs = append(result.DeletedIDs, ids...)
	}
	if len(w.Patches) > 0 {
		ids, err := patchTx(ctx, tx, table, w.Patches, w.PatchCondition, extraCols)
		if err != nil {
			return WriteResult{}, err
		}
		result.PatchedIDs = append(result.PatchedIDs, ids...)
	}
	if len(w.Upserts) > 0 {
		ids, err := upsertTx(ctx, tx, table, w.Upserts, w.UpsertCondition, extraCols)
		if err != nil {
			return WriteResult{}, err
		}
		result.UpsertedIDs = append(result.UpsertedIDs, ids...)
	}

	if err := tx.Commit(); err != nil {
		return WriteResult{}, fmt.Errorf("commit transaction: %w", err)
	}
	if err := s.ensureVectorIndexes(ctx, w.Namespace); err != nil {
		s.logger.Error("ensure vector indexes after write",
			Field{Key: "namespace", Value: w.Namespace},
			Field{Key: "error", Value: err.Error()},
		)
	}
	result.Upserted = len(result.UpsertedIDs)
	result.Patched = len(result.PatchedIDs)
	result.Deleted = len(result.DeletedIDs)
	s.logger.Info("wrote documents",
		Field{Key: "namespace", Value: w.Namespace},
		Field{Key: "upserted", Value: result.Upserted},
		Field{Key: "patched", Value: result.Patched},
		Field{Key: "deleted", Value: result.Deleted},
	)
	return result, nil
}

func duplicateWriteIDs(w Write) error {
	seen := map[DocumentID]struct{}{}
	add := func(id DocumentID) error {
		if id == "" {
			return nil
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateWriteID, id)
		}
		seen[id] = struct{}{}
		return nil
	}
	for _, doc := range w.Upserts {
		if err := add(doc.ID); err != nil {
			return err
		}
	}
	for _, doc := range w.Patches {
		if err := add(doc.ID); err != nil {
			return err
		}
	}
	for _, id := range w.Deletes {
		if err := add(id); err != nil {
			return err
		}
	}
	return nil
}

func upsertTx(ctx context.Context, tx *sql.Tx, table string, docs []Document, condition Filter, extraDests []string) ([]DocumentID, error) {
	var ids []DocumentID
	colSQL, placeholders, extraN := extraInsertSQL(extraDests)
	stmt := fmt.Sprintf(`
		INSERT INTO %s (id, vector, attributes%s)
		VALUES ($1, $2, $3%s)
		ON CONFLICT (id) DO UPDATE SET
			vector = EXCLUDED.vector,
			attributes = EXCLUDED.attributes%s
		RETURNING id`, table, colSQL, placeholders, extraUpdateSQL(extraDests))
	for _, doc := range docs {
		if doc.Attributes == nil {
			doc.Attributes = map[string]interface{}{}
		}
		attrsJSON, err := json.Marshal(doc.Attributes)
		if err != nil {
			return nil, fmt.Errorf("marshal attributes: %w", err)
		}
		query := stmt
		args := []interface{}{doc.ID, vectorArg(doc.Vector), attrsJSON}
		args = append(args, extraVectorArgs(doc, extraDests)...)
		if condition != nil {
			cond := bindRefNew(condition, doc.Attributes)
			where, condArgs, err := buildFilterSQL(cond)
			if err != nil {
				return nil, fmt.Errorf("upsert condition: %w", err)
			}
			if where != "" {
				query = fmt.Sprintf(`
		INSERT INTO %s (id, vector, attributes%s)
		VALUES ($1, $2, $3%s)
		ON CONFLICT (id) DO UPDATE SET
			vector = EXCLUDED.vector,
			attributes = EXCLUDED.attributes%s
		WHERE %s
		RETURNING id`, table, colSQL, placeholders, extraUpdateSQL(extraDests), shiftPlaceholders(qualifyExistingRow(where, table), 3+extraN))
				args = append(args, condArgs...)
			}
		}
		got, err := queryIDs(ctx, tx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("upsert document: %w", err)
		}
		ids = append(ids, got...)
	}
	return ids, nil
}

func extraInsertSQL(dests []string) (cols, placeholders string, n int) {
	if len(dests) == 0 {
		return "", "", 0
	}
	var colParts, phParts []string
	for i, dest := range dests {
		colParts = append(colParts, SQLIdent(GetVectorColumnName(dest)))
		phParts = append(phParts, fmt.Sprintf("$%d", 4+i))
	}
	return ", " + strings.Join(colParts, ", "), ", " + strings.Join(phParts, ", "), len(dests)
}

func extraUpdateSQL(dests []string) string {
	if len(dests) == 0 {
		return ""
	}
	var parts []string
	for _, dest := range dests {
		col := SQLIdent(GetVectorColumnName(dest))
		parts = append(parts, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
	}
	return ", " + strings.Join(parts, ", ")
}

func extraVectorArgs(doc Document, dests []string) []interface{} {
	out := make([]interface{}, len(dests))
	for i, dest := range dests {
		if doc.ExtraVectors != nil {
			out[i] = vectorArg(doc.ExtraVectors[dest])
			continue
		}
		out[i] = vectorArg(nil)
	}
	return out
}

func patchTx(ctx context.Context, tx *sql.Tx, table string, docs []Document, condition Filter, extraDests []string) ([]DocumentID, error) {
	var ids []DocumentID
	for _, doc := range docs {
		if doc.Attributes == nil {
			doc.Attributes = map[string]interface{}{}
		}
		attrsJSON, err := json.Marshal(doc.Attributes)
		if err != nil {
			return nil, fmt.Errorf("marshal attributes: %w", err)
		}
		query := fmt.Sprintf(`
				UPDATE %s
				SET attributes = attributes || $2::jsonb
				WHERE id = $1`, table)
		args := []interface{}{doc.ID, attrsJSON}
		shift := 2
		if len(doc.Vector) > 0 {
			query = fmt.Sprintf(`
				UPDATE %s
				SET attributes = attributes || $2::jsonb,
				    vector = $3
				WHERE id = $1`, table)
			args = []interface{}{doc.ID, attrsJSON, vectorArg(doc.Vector)}
			shift = 3
		}
		if extra := extraPatchSQL(doc, extraDests, &args); extra != "" {
			query = strings.Replace(query, "WHERE id = $1", extra+"\n				WHERE id = $1", 1)
			shift = len(args)
		}
		if condition != nil {
			cond := bindRefNew(condition, doc.Attributes)
			where, condArgs, err := buildFilterSQL(cond)
			if err != nil {
				return nil, fmt.Errorf("patch condition: %w", err)
			}
			if where != "" {
				query += " AND " + shiftPlaceholders(where, shift)
				args = append(args, condArgs...)
			}
		}
		query += " RETURNING id"
		got, err := queryIDs(ctx, tx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("patch document: %w", err)
		}
		ids = append(ids, got...)
	}
	return ids, nil
}

func patchByFilterTx(ctx context.Context, tx *sql.Tx, table string, spec PatchByFilterWrite, cap int) ([]DocumentID, int, error) {
	attrs := spec.Attributes
	if attrs == nil {
		attrs = map[string]interface{}{}
	}
	whereClause, args, err := buildFilterSQL(spec.Filter)
	if err != nil {
		return nil, 0, fmt.Errorf("build filter: %w", err)
	}
	if whereClause == "" {
		return nil, 0, nil
	}
	remaining, limitedWhere, limitedArgs, err := applyFilterCap(ctx, tx, table, whereClause, args, cap)
	if err != nil {
		return nil, 0, err
	}
	attrsJSON, err := json.Marshal(attrs)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal attributes: %w", err)
	}
	var query string
	var execArgs []interface{}
	if len(spec.Vector) > 0 {
		query = fmt.Sprintf(`
			UPDATE %s
			SET attributes = COALESCE(attributes, '{}'::jsonb) || $1::jsonb,
			    vector = $2
			WHERE %s
			RETURNING id`, table, shiftPlaceholders(limitedWhere, 2))
		execArgs = append([]interface{}{attrsJSON, vectorArg(spec.Vector)}, limitedArgs...)
	} else {
		query = fmt.Sprintf(`
			UPDATE %s
			SET attributes = COALESCE(attributes, '{}'::jsonb) || $1::jsonb
			WHERE %s
			RETURNING id`, table, shiftPlaceholders(limitedWhere, 1))
		execArgs = append([]interface{}{attrsJSON}, limitedArgs...)
	}
	ids, err := queryIDs(ctx, tx, query, execArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("patch by filter: %w", err)
	}
	return ids, remaining, nil
}

func deleteIDsTx(ctx context.Context, tx *sql.Tx, table string, ids []DocumentID, condition Filter) ([]DocumentID, error) {
	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE id = ANY($1)
		RETURNING id`, table)
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = string(id)
	}
	args := []interface{}{pq.Array(strIDs)}
	if condition != nil {
		where, condArgs, err := buildFilterSQL(condition)
		if err != nil {
			return nil, fmt.Errorf("delete condition: %w", err)
		}
		if where != "" {
			query = fmt.Sprintf(`
		DELETE FROM %s
		WHERE id = ANY($1) AND %s
		RETURNING id`, table, shiftPlaceholders(where, 1))
			args = append(args, condArgs...)
		}
	}
	got, err := queryIDs(ctx, tx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("delete documents: %w", err)
	}
	return got, nil
}

func extraPatchSQL(doc Document, dests []string, args *[]interface{}) string {
	if len(dests) == 0 || len(doc.ExtraVectors) == 0 {
		return ""
	}
	var parts []string
	for _, dest := range dests {
		vec, ok := doc.ExtraVectors[dest]
		if !ok || len(vec) == 0 {
			continue
		}
		*args = append(*args, vectorArg(vec))
		parts = append(parts, fmt.Sprintf(", %s = $%d", SQLIdent(GetVectorColumnName(dest)), len(*args)))
	}
	return strings.Join(parts, "")
}

func applyFilterCap(ctx context.Context, tx *sql.Tx, table, where string, args []interface{}, cap int) (remaining int, limitedWhere string, limitedArgs []interface{}, err error) {
	if cap <= 0 {
		return 0, where, args, nil
	}
	var matched int
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s`, table, where)
	if err := tx.QueryRowContext(ctx, countSQL, args...).Scan(&matched); err != nil {
		return 0, "", nil, fmt.Errorf("count matching rows: %w", err)
	}
	if matched <= cap {
		return 0, where, args, nil
	}
	idSQL := fmt.Sprintf(`id IN (SELECT id FROM %s WHERE %s LIMIT %d)`, table, where, cap)
	return matched - cap, idSQL, args, nil
}

func deleteByFilterTx(ctx context.Context, tx *sql.Tx, table string, filter Filter, cap int) ([]DocumentID, int, error) {
	whereClause, args, err := buildFilterSQL(filter)
	if err != nil {
		return nil, 0, fmt.Errorf("build filter: %w", err)
	}
	if whereClause == "" {
		return nil, 0, nil
	}
	remaining, limitedWhere, limitedArgs, err := applyFilterCap(ctx, tx, table, whereClause, args, cap)
	if err != nil {
		return nil, 0, err
	}
	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE %s
		RETURNING id`, table, limitedWhere)
	ids, err := queryIDs(ctx, tx, query, limitedArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("delete documents by filter: %w", err)
	}
	return ids, remaining, nil
}

func queryIDs(ctx context.Context, tx *sql.Tx, query string, args ...interface{}) ([]DocumentID, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []DocumentID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, DocumentID(id))
	}
	return ids, rows.Err()
}
