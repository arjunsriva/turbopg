package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/arjunsriva/turbopg"
)

func documentsFromRows(rows []map[string]interface{}) ([]turbopg.Document, error) {
	docs := make([]turbopg.Document, 0, len(rows))
	for i, row := range rows {
		doc, err := documentFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func documentsFromColumns(cols map[string]interface{}) ([]turbopg.Document, error) {
	idsRaw, ok := cols["id"]
	if !ok {
		return nil, fmt.Errorf("columnar write requires an id column")
	}
	ids, err := asSlice(idsRaw)
	if err != nil {
		return nil, fmt.Errorf("id column: %w", err)
	}
	n := len(ids)
	rows := make([]map[string]interface{}, n)
	for i := 0; i < n; i++ {
		rows[i] = map[string]interface{}{"id": ids[i]}
	}
	for key, raw := range cols {
		if key == "id" {
			continue
		}
		vals, err := asSlice(raw)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", key, err)
		}
		if len(vals) != n {
			return nil, fmt.Errorf("column %s length %d does not match id length %d", key, len(vals), n)
		}
		for i := 0; i < n; i++ {
			rows[i][key] = vals[i]
		}
	}
	return documentsFromRows(rows)
}

func documentFromRow(row map[string]interface{}) (turbopg.Document, error) {
	idRaw, ok := row["id"]
	if !ok || idRaw == nil {
		return turbopg.Document{}, turbopg.ErrEmptyDocumentID
	}
	id, err := stringifyID(idRaw)
	if err != nil {
		return turbopg.Document{}, err
	}
	if err := turbopg.ValidateDocumentID(turbopg.DocumentID(id)); err != nil {
		return turbopg.Document{}, err
	}

	attrs := make(map[string]interface{})
	var vector []float32
	for k, v := range row {
		if k == "id" {
			continue
		}
		if k == "vector" {
			if v == nil {
				continue
			}
			vector, err = parseVector(v)
			if err != nil {
				return turbopg.Document{}, err
			}
			continue
		}
		if err := turbopg.ValidateAttributeName(k); err != nil {
			return turbopg.Document{}, err
		}
		attrs[k] = v
	}
	return turbopg.Document{
		ID:         turbopg.DocumentID(id),
		Vector:     vector,
		Attributes: attrs,
	}, nil
}

func stringifyID(v interface{}) (string, error) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return "", turbopg.ErrEmptyDocumentID
		}
		return t, nil
	case json.Number:
		return t.String(), nil
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), nil
		}
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case uint64:
		return strconv.FormatUint(t, 10), nil
	default:
		return fmt.Sprint(t), nil
	}
}

func exportID(id turbopg.DocumentID) interface{} {
	s := string(id)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && strconv.FormatInt(n, 10) == s {
		return n
	}
	return s
}

func parseVector(v interface{}) ([]float32, error) {
	switch t := v.(type) {
	case []float32:
		return t, nil
	case []float64:
		out := make([]float32, len(t))
		for i, x := range t {
			out[i] = float32(x)
		}
		return out, nil
	case []interface{}:
		out := make([]float32, len(t))
		for i, x := range t {
			f, err := toFloat32(x)
			if err != nil {
				return nil, err
			}
			out[i] = f
		}
		return out, nil
	case string:
		return decodeBase64Vector(t)
	default:
		return nil, fmt.Errorf("vector must be an array, got %T", v)
	}
}

// decodeBase64Vector accepts official-client encoding: little-endian f32
// packed as standard base64 (see turbopuffer-python lib.vector).
func decodeBase64Vector(s string) ([]float32, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("vector must be an array or base64-encoded f32")
		}
	}
	if len(raw) == 0 || len(raw)%4 != 0 {
		return nil, fmt.Errorf("vector must be an array or base64-encoded f32")
	}
	out := make([]float32, len(raw)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out, nil
}

func toFloat32(v interface{}) (float32, error) {
	switch t := v.(type) {
	case float64:
		return float32(t), nil
	case float32:
		return t, nil
	case json.Number:
		f, err := t.Float64()
		return float32(f), err
	case int:
		return float32(t), nil
	case int64:
		return float32(t), nil
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}

func asSlice(v interface{}) ([]interface{}, error) {
	switch t := v.(type) {
	case []interface{}:
		return t, nil
	default:
		return nil, fmt.Errorf("expected array, got %T", v)
	}
}

func idsFromJSON(raw []interface{}) []turbopg.DocumentID {
	ids := make([]turbopg.DocumentID, 0, len(raw))
	for _, v := range raw {
		s, err := stringifyID(v)
		if err != nil {
			continue
		}
		ids = append(ids, turbopg.DocumentID(s))
	}
	return ids
}

func exportIDs(ids []turbopg.DocumentID) []interface{} {
	if len(ids) == 0 {
		return nil
	}
	out := make([]interface{}, len(ids))
	for i, id := range ids {
		out[i] = exportID(id)
	}
	return out
}

func firstVectorFromDocs(docs []turbopg.Document) []float32 {
	for _, d := range docs {
		if len(d.Vector) > 0 {
			return d.Vector
		}
	}
	return nil
}

func resultRow(res turbopg.QueryResult, include includeAttributes, exclude []string) map[string]interface{} {
	row := map[string]interface{}{
		"id": exportID(res.Document.ID),
	}
	if res.HasScore {
		row["$dist"] = res.Score
	}
	for k, v := range res.Computed {
		row[k] = v
	}
	skip := map[string]struct{}{}
	for _, f := range exclude {
		skip[f] = struct{}{}
	}
	copyAttrs := func(all bool, wanted map[string]struct{}) {
		for k, v := range res.Document.Attributes {
			if _, blocked := skip[k]; blocked {
				continue
			}
			if all {
				row[k] = v
				continue
			}
			if _, ok := wanted[k]; ok {
				row[k] = v
			}
		}
		if _, blocked := skip["vector"]; !blocked && len(res.Document.Vector) > 0 {
			if all {
				row["vector"] = res.Document.Vector
			} else if _, ok := wanted["vector"]; ok {
				row["vector"] = res.Document.Vector
			}
		}
		for k, v := range res.Document.ExtraVectors {
			if _, blocked := skip[k]; blocked {
				continue
			}
			if _, exists := row[k]; exists {
				continue
			}
			if all {
				row[k] = v
				continue
			}
			if _, ok := wanted[k]; ok {
				row[k] = v
			}
		}
	}
	if !include.set {
		if len(exclude) > 0 {
			copyAttrs(true, nil)
		}
		return row
	}
	if include.all {
		copyAttrs(true, nil)
		return row
	}
	wanted := map[string]struct{}{}
	for _, f := range include.fields {
		wanted[f] = struct{}{}
	}
	copyAttrs(false, wanted)
	return row
}

func queryPerformance(count int64, started time.Time) QueryPerformance {
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return QueryPerformance{
		ApproxNamespaceSize:   count,
		CacheHitRatio:         1,
		CacheTemperature:      "hot",
		ExhaustiveSearchCount: 0,
		QueryExecutionMs:      elapsed,
		ServerTotalMs:         elapsed,
	}
}

func writePerformance(started time.Time) WritePerformance {
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return WritePerformance{ServerTotalMs: elapsed}
}
