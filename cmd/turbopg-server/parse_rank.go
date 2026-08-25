package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arjunsriva/turbopg"
)

func parseRankBy(rankBy []interface{}) (turbopg.RankSpec, error) {
	if len(rankBy) == 0 {
		return turbopg.RankSpec{}, nil
	}
	first, _ := rankBy[0].(string)
	switch first {
	case "Sum", "Max":
		if len(rankBy) < 2 {
			return turbopg.RankSpec{}, fmt.Errorf("rank_by %s requires a list of clauses", first)
		}
		children, err := parseRankList(rankBy[1])
		if err != nil {
			return turbopg.RankSpec{}, err
		}
		kind := turbopg.RankSum
		if first == "Max" {
			kind = turbopg.RankMax
		}
		return turbopg.RankSpec{Kind: kind, Children: children}, nil
	case "Product":
		if len(rankBy) < 3 {
			return turbopg.RankSpec{}, fmt.Errorf("rank_by Product requires a weight and subquery")
		}
		weight, childRaw, err := parseProductArgs(rankBy[1], rankBy[2])
		if err != nil {
			return turbopg.RankSpec{}, err
		}
		child, err := parseRankBy(childRaw)
		if err != nil {
			return turbopg.RankSpec{}, err
		}
		return turbopg.RankSpec{Kind: turbopg.RankProduct, Weight: weight, Children: []turbopg.RankSpec{child}}, nil
	}
	if len(rankBy) < 2 {
		return turbopg.RankSpec{}, fmt.Errorf("rank_by must have at least 2 elements")
	}
	field := first
	op, _ := rankBy[1].(string)
	switch strings.ToUpper(op) {
	case "ANN", "KNN":
		if len(rankBy) < 3 {
			return turbopg.RankSpec{}, fmt.Errorf("rank_by %s requires a query vector", strings.ToUpper(op))
		}
		exact := strings.EqualFold(op, "KNN")
		if query, model, ok, err := parseEmbedOperand(rankBy[2]); ok {
			if err != nil {
				return turbopg.RankSpec{}, err
			}
			return turbopg.RankSpec{Kind: turbopg.RankVector, Field: field, EmbedQuery: query, EmbedModel: model, Exact: exact}, nil
		}
		if isMultiVector(rankBy[2]) {
			multi, err := parseMultiVector(rankBy[2])
			if err != nil {
				return turbopg.RankSpec{}, err
			}
			return turbopg.RankSpec{Kind: turbopg.RankLate, Field: field, MultiVector: multi, Exact: exact}, nil
		}
		vector, err := parseVector(rankBy[2])
		if err != nil {
			return turbopg.RankSpec{}, err
		}
		return turbopg.RankSpec{Kind: turbopg.RankVector, Field: field, Vector: vector, Exact: exact}, nil
	case "SPARSEKNN":
		if len(rankBy) < 3 {
			return turbopg.RankSpec{}, fmt.Errorf("rank_by SparseKNN requires a sparse vector")
		}
		sparse, err := parseSparse(rankBy[2])
		if err != nil {
			return turbopg.RankSpec{}, err
		}
		return turbopg.RankSpec{Kind: turbopg.RankSparse, Field: field, Sparse: sparse}, nil
	case "BM25":
		if len(rankBy) < 3 {
			return turbopg.RankSpec{}, fmt.Errorf("rank_by BM25 requires a query string")
		}
		query, err := parseBM25Query(rankBy[2])
		if err != nil {
			return turbopg.RankSpec{}, err
		}
		return turbopg.RankSpec{Kind: turbopg.RankBM25, Field: field, Query: query}, nil
	case "ASC", "DESC":
		return turbopg.RankSpec{Kind: turbopg.RankAttribute, Field: field, Desc: strings.EqualFold(op, "desc")}, nil
	default:
		return turbopg.RankSpec{}, fmt.Errorf("unsupported rank_by operator %q", op)
	}
}

func parseEmbedOperand(v interface{}) (query, model string, ok bool, err error) {
	arr, err := asSlice(v)
	if err != nil || len(arr) < 2 {
		return "", "", false, nil
	}
	op, _ := arr[0].(string)
	if !strings.EqualFold(op, "Embed") {
		return "", "", false, nil
	}
	text, ok := arr[1].(string)
	if !ok || text == "" {
		return "", "", true, fmt.Errorf("Embed requires a string")
	}
	if len(arr) >= 3 {
		if m, ok := arr[2].(map[string]interface{}); ok {
			model, _ = m["model"].(string)
		}
	}
	return text, model, true, nil
}

func parseProductArgs(a, b interface{}) (float64, []interface{}, error) {
	if weight, err := toFloat64(a); err == nil {
		childRaw, err := asSlice(b)
		if err != nil {
			return 0, nil, err
		}
		return weight, childRaw, nil
	}
	if weight, err := toFloat64(b); err == nil {
		childRaw, err := asSlice(a)
		if err != nil {
			return 0, nil, err
		}
		return weight, childRaw, nil
	}
	return 0, nil, fmt.Errorf("Product weight: not a number")
}

func isMultiVector(v interface{}) bool {
	switch t := v.(type) {
	case [][]float32, [][]float64:
		return true
	case []interface{}:
		if len(t) == 0 {
			return false
		}
		switch t[0].(type) {
		case []interface{}, []float32, []float64:
			return true
		}
	}
	return false
}

func parseMultiVector(v interface{}) ([][]float32, error) {
	switch t := v.(type) {
	case [][]float32:
		return t, nil
	case [][]float64:
		out := make([][]float32, len(t))
		for i, row := range t {
			vec := make([]float32, len(row))
			for j, x := range row {
				vec[j] = float32(x)
			}
			out[i] = vec
		}
		return out, nil
	case []interface{}:
		out := make([][]float32, 0, len(t))
		for _, item := range t {
			vec, err := parseVector(item)
			if err != nil {
				return nil, err
			}
			out = append(out, vec)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("late-interaction query must be an array of vectors, got %T", v)
	}
}

func parseSparse(v interface{}) (map[string]float64, error) {
	switch t := v.(type) {
	case map[string]float64:
		return t, nil
	case map[string]float32:
		out := make(map[string]float64, len(t))
		for k, val := range t {
			out[k] = float64(val)
		}
		return out, nil
	case map[string]interface{}:
		out := make(map[string]float64, len(t))
		for k, val := range t {
			f, err := toFloat64(val)
			if err != nil {
				return nil, fmt.Errorf("sparse vector %s: %w", k, err)
			}
			out[k] = f
		}
		return out, nil
	default:
		return nil, fmt.Errorf("SparseKNN query must be an object, got %T", v)
	}
}

func parseRankList(v interface{}) ([]turbopg.RankSpec, error) {
	arr, err := asSlice(v)
	if err != nil {
		return nil, err
	}
	out := make([]turbopg.RankSpec, 0, len(arr))
	for _, item := range arr {
		sl, err := asSlice(item)
		if err != nil {
			return nil, err
		}
		spec, err := parseRankBy(sl)
		if err != nil {
			return nil, err
		}
		out = append(out, spec)
	}
	return out, nil
}

func parseBM25Query(v interface{}) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case []string:
		return strings.Join(t, " "), nil
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, x := range t {
			parts = append(parts, fmt.Sprint(x))
		}
		return strings.Join(parts, " "), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func toFloat64(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case json.Number:
		return t.Float64()
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}
