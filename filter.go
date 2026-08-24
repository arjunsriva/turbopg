package turbopg

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// Common errors
var (
	ErrInvalidFilterType = errors.New("invalid filter type")
)

// FilterOp represents the type of filter operation
type FilterOp string

const (
	FilterOpEq                    FilterOp = "Eq"
	FilterOpNotEq                 FilterOp = "NotEq"
	FilterOpIn                    FilterOp = "In"
	FilterOpNotIn                 FilterOp = "NotIn"
	FilterOpLt                    FilterOp = "Lt"
	FilterOpLte                   FilterOp = "Lte"
	FilterOpGt                    FilterOp = "Gt"
	FilterOpGte                   FilterOp = "Gte"
	FilterOpGlob                  FilterOp = "Glob"
	FilterOpNotGlob               FilterOp = "NotGlob"
	FilterOpIGlob                 FilterOp = "IGlob"
	FilterOpNotIGlob              FilterOp = "NotIGlob"
	FilterOpContains              FilterOp = "Contains"
	FilterOpNotContains           FilterOp = "NotContains"
	FilterOpContainsAny           FilterOp = "ContainsAny"
	FilterOpNotContainsAny        FilterOp = "NotContainsAny"
	FilterOpContainsAllTokens     FilterOp = "ContainsAllTokens"
	FilterOpContainsAnyToken      FilterOp = "ContainsAnyToken"
	FilterOpContainsTokenSequence FilterOp = "ContainsTokenSequence"
	FilterOpRegex                 FilterOp = "Regex"
	FilterOpFuzzy                 FilterOp = "Fuzzy"
	FilterOpAnyGt                 FilterOp = "AnyGt"
	FilterOpAnyGte                FilterOp = "AnyGte"
	FilterOpAnyLt                 FilterOp = "AnyLt"
	FilterOpAnyLte                FilterOp = "AnyLte"
	FilterOpAnyEq                 FilterOp = "AnyEq"
)

// Filter represents a single filter condition or a logical operation
type Filter interface {
	isFilter()
}

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Field string
	Op    FilterOp
	Value interface{}
}

func (f FilterCondition) isFilter() {}

// LogicalOp represents the type of logical operation
type LogicalOp string

const (
	LogicalOpAnd LogicalOp = "And"
	LogicalOpOr  LogicalOp = "Or"
)

// LogicalFilter represents a logical operation on multiple filters
type LogicalFilter struct {
	Op      LogicalOp
	Filters []Filter
}

func (f LogicalFilter) isFilter() {}

// NotFilter negates an inner filter.
type NotFilter struct {
	Filter Filter
}

func (f NotFilter) isFilter() {}

var placeholderRE = regexp.MustCompile(`\$(\d+)`)

func shiftPlaceholders(sql string, offset int) string {
	if offset == 0 {
		return sql
	}
	return placeholderRE.ReplaceAllStringFunc(sql, func(m string) string {
		n, _ := strconv.Atoi(m[1:])
		return fmt.Sprintf("$%d", n+offset)
	})
}

func escapeJSONKey(field string) string {
	return strings.ReplaceAll(field, "'", "''")
}

func filterColumn(field string, numeric bool) string {
	if field == "id" {
		if numeric {
			return "id::numeric"
		}
		return "id"
	}
	key := escapeJSONKey(field)
	if numeric {
		return fmt.Sprintf("(attributes->>'%s')::numeric", key)
	}
	return fmt.Sprintf("attributes->>'%s'", key)
}

// buildFilterSQL converts a Filter to SQL WHERE clause and args
func buildFilterSQL(filter Filter) (string, []interface{}, error) {
	switch f := filter.(type) {
	case FilterCondition:
		return buildSimpleFilterSQL(f)
	case *FilterCondition:
		return buildSimpleFilterSQL(*f)
	case LogicalFilter:
		return buildLogicalFilterSQL(f)
	case *LogicalFilter:
		return buildLogicalFilterSQL(*f)
	case NotFilter:
		return buildNotFilterSQL(f)
	case *NotFilter:
		return buildNotFilterSQL(*f)
	default:
		return "", nil, fmt.Errorf("unsupported filter type: %T", filter)
	}
}

// buildSimpleFilterSQL converts a FilterCondition to SQL
func buildSimpleFilterSQL(f FilterCondition) (string, []interface{}, error) {
	var op string
	needsCast := false

	switch f.Op {
	case FilterOpEq:
		if isFilterNull(f.Value) {
			return fmt.Sprintf("%s IS NULL", filterColumn(f.Field, false)), nil, nil
		}
		op = "="
	case FilterOpNotEq:
		if isFilterNull(f.Value) {
			return fmt.Sprintf("%s IS NOT NULL", filterColumn(f.Field, false)), nil, nil
		}
		op = "!="
	case FilterOpLt:
		op = "<"
		needsCast = true
	case FilterOpLte:
		op = "<="
		needsCast = true
	case FilterOpGt:
		op = ">"
		needsCast = true
	case FilterOpGte:
		op = ">="
		needsCast = true
	case FilterOpGlob:
		op = "LIKE"
		f.Value = globToLike(fmt.Sprint(f.Value))
	case FilterOpNotGlob:
		op = "NOT LIKE"
		f.Value = globToLike(fmt.Sprint(f.Value))
	case FilterOpIGlob:
		op = "ILIKE"
		f.Value = globToLike(fmt.Sprint(f.Value))
	case FilterOpNotIGlob:
		op = "NOT ILIKE"
		f.Value = globToLike(fmt.Sprint(f.Value))
	case FilterOpRegex:
		return fmt.Sprintf("%s ~ $1", jsonTextExpr(f.Field)), []interface{}{fmt.Sprint(f.Value)}, nil
	case FilterOpFuzzy:
		return fmt.Sprintf("%s ILIKE '%%' || $1 || '%%'", jsonTextExpr(f.Field)), []interface{}{fmt.Sprint(f.Value)}, nil
	case FilterOpIn:
		return buildInConditionSQL(f, false)
	case FilterOpNotIn:
		return buildInConditionSQL(f, true)
	case FilterOpContains:
		return buildContainsSQL(f, false)
	case FilterOpNotContains:
		sql, args, err := buildContainsSQL(f, false)
		if err != nil {
			return "", nil, err
		}
		return "NOT (" + sql + ")", args, nil
	case FilterOpContainsAny:
		return buildContainsAnySQL(f)
	case FilterOpNotContainsAny:
		sql, args, err := buildContainsAnySQL(f)
		if err != nil {
			return "", nil, err
		}
		return "NOT (" + sql + ")", args, nil
	case FilterOpContainsAllTokens, FilterOpContainsAnyToken, FilterOpContainsTokenSequence:
		return buildTokenFilterSQL(f)
	case FilterOpAnyGt, FilterOpAnyGte, FilterOpAnyLt, FilterOpAnyLte, FilterOpAnyEq:
		return buildAnyCompareSQL(f)
	default:
		return "", nil, fmt.Errorf("unsupported filter operation: %s", f.Op)
	}

	col := filterColumn(f.Field, needsCast)
	value := coerceFilterValue(f.Value)
	if f.Field == "id" && !needsCast {
		value = fmt.Sprint(value)
	}
	sql := fmt.Sprintf("%s %s $1", col, op)
	if (f.Op == FilterOpLt || f.Op == FilterOpLte) && !isFilterNull(f.Value) {
		sql = fmt.Sprintf("(%s IS NULL OR %s %s $1)", col, col, op)
	}
	return sql, []interface{}{value}, nil
}

// buildInConditionSQL handles IN and NOT IN conditions
func buildInConditionSQL(f FilterCondition, not bool) (string, []interface{}, error) {
	values, err := inValues(f.Value)
	if err != nil {
		return "", nil, err
	}
	if len(values) == 0 {
		if not {
			return "TRUE", nil, nil
		}
		return "FALSE", nil, nil
	}

	if f.Field == "id" {
		placeholder := make([]string, len(values))
		args := make([]interface{}, len(values))
		for i, v := range values {
			placeholder[i] = fmt.Sprintf("$%d", i+1)
			args[i] = fmt.Sprint(v)
		}
		op := "IN"
		if not {
			op = "NOT IN"
		}
		return fmt.Sprintf("%s %s (%s)", filterColumn(f.Field, false), op, strings.Join(placeholder, ",")), args, nil
	}

	parts := make([]string, len(values))
	args := make([]interface{}, len(values))
	for i, v := range values {
		raw, err := json.Marshal(coerceFilterValue(v))
		if err != nil {
			return "", nil, err
		}
		parts[i] = fmt.Sprintf("(%s @> $%d::jsonb)", jsonContainer(f.Field), i+1)
		args[i] = string(raw)
	}
	sql := strings.Join(parts, " OR ")
	if len(parts) > 1 {
		sql = "(" + sql + ")"
	}
	if not {
		sql = "NOT (" + sql + ")"
	}
	return sql, args, nil
}

func inValues(v interface{}) ([]interface{}, error) {
	switch t := v.(type) {
	case []interface{}:
		return t, nil
	case []string:
		out := make([]interface{}, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out, nil
	case nil:
		return nil, fmt.Errorf("IN/NOT IN requires a value")
	default:
		return []interface{}{v}, nil
	}
}

// buildLogicalFilterSQL converts a LogicalFilter to SQL
func buildLogicalFilterSQL(f LogicalFilter) (string, []interface{}, error) {
	if len(f.Filters) == 0 {
		return "", nil, fmt.Errorf("logical filter must have at least one condition")
	}

	var conditions []string
	var args []interface{}
	argOffset := 0

	for _, subFilter := range f.Filters {
		cond, subArgs, err := buildFilterSQL(subFilter)
		if err != nil {
			return "", nil, err
		}

		cond = shiftPlaceholders(cond, argOffset)
		conditions = append(conditions, "("+cond+")")
		args = append(args, subArgs...)
		argOffset += len(subArgs)
	}

	op := " AND "
	if f.Op == LogicalOpOr {
		op = " OR "
	}

	return strings.Join(conditions, op), args, nil
}

func buildNotFilterSQL(f NotFilter) (string, []interface{}, error) {
	if f.Filter == nil {
		return "", nil, fmt.Errorf("Not filter requires an inner condition")
	}
	cond, args, err := buildFilterSQL(f.Filter)
	if err != nil {
		return "", nil, err
	}
	return "NOT (" + cond + ")", args, nil
}

func asStringSlice(v interface{}) ([]string, error) {
	switch t := v.(type) {
	case []string:
		return t, nil
	case []interface{}:
		out := make([]string, len(t))
		for i, x := range t {
			out[i] = fmt.Sprint(x)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected array, got %T", v)
	}
}

func buildContainsSQL(f FilterCondition, _ bool) (string, []interface{}, error) {
	switch f.Value.(type) {
	case []interface{}, []string:
		raw, err := json.Marshal(f.Value)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%s @> $1::jsonb", jsonContainer(f.Field)), []interface{}{string(raw)}, nil
	default:
		raw, err := json.Marshal(coerceFilterValue(f.Value))
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%s @> $1::jsonb", jsonContainer(f.Field)), []interface{}{string(raw)}, nil
	}
}

func buildAnyCompareSQL(f FilterCondition) (string, []interface{}, error) {
	op := ""
	switch f.Op {
	case FilterOpAnyGt:
		op = ">"
	case FilterOpAnyGte:
		op = ">="
	case FilterOpAnyLt:
		op = "<"
	case FilterOpAnyLte:
		op = "<="
	case FilterOpAnyEq:
		op = "="
	}
	return fmt.Sprintf(
		`EXISTS (SELECT 1 FROM jsonb_array_elements_text(COALESCE(%s, '[]'::jsonb)) e WHERE e::numeric %s $1)`,
		jsonContainer(f.Field), op,
	), []interface{}{coerceFilterValue(f.Value)}, nil
}

func jsonContainer(field string) string {
	if field == "id" {
		return "to_jsonb(id)"
	}
	return fmt.Sprintf("attributes->'%s'", escapeJSONKey(field))
}

func buildContainsAnySQL(f FilterCondition) (string, []interface{}, error) {
	vals, err := asStringSlice(f.Value)
	if err != nil {
		return "", nil, fmt.Errorf("ContainsAny requires array value: %w", err)
	}
	return fmt.Sprintf("(%s = ANY($1) OR %s ?| $1)", jsonTextExpr(f.Field), jsonContainer(f.Field)),
		[]interface{}{pq.Array(vals)}, nil
}

func buildTokenFilterSQL(f FilterCondition) (string, []interface{}, error) {
	query, ok := f.Value.(string)
	if !ok {
		query = fmt.Sprint(f.Value)
	}
	ts := tsQueryFromTokens(query, f.Op == FilterOpContainsAnyToken, f.Op == FilterOpContainsTokenSequence)
	if ts == "" {
		return "TRUE", nil, nil
	}
	return fmt.Sprintf("to_tsvector('simple', COALESCE(%s, '')) @@ to_tsquery('simple', $1)", jsonTextExpr(f.Field)),
		[]interface{}{ts}, nil
}

func tsQueryFromTokens(s string, any, sequence bool) string {
	parts := strings.Fields(s)
	var toks []string
	for _, p := range parts {
		cleaned := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, p)
		if cleaned != "" {
			toks = append(toks, cleaned)
		}
	}
	if len(toks) == 0 {
		return ""
	}
	sep := " & "
	if sequence {
		sep = " <-> "
	} else if any {
		sep = " | "
	}
	return strings.Join(toks, sep)
}

func qualifyExistingRow(sql, table string) string {
	sql = regexp.MustCompile(`\battributes\b`).ReplaceAllString(sql, table+".attributes")
	return regexp.MustCompile(`\bid\b`).ReplaceAllString(sql, table+".id")
}

func coerceFilterValue(v interface{}) interface{} {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return f
	default:
		return v
	}
}

func isFilterNull(v interface{}) bool {
	return v == nil
}

func globToLike(pattern string) string {
	if strings.ContainsAny(pattern, "%_") && !strings.ContainsAny(pattern, "*?") {
		return pattern
	}
	var b strings.Builder
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteByte('%')
		case '?':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func bindRefNew(filter Filter, attrs map[string]interface{}) Filter {
	if filter == nil {
		return nil
	}
	switch f := filter.(type) {
	case FilterCondition:
		f.Value = resolveRefNew(f.Value, attrs)
		return f
	case *FilterCondition:
		cp := *f
		cp.Value = resolveRefNew(cp.Value, attrs)
		return cp
	case LogicalFilter:
		out := make([]Filter, len(f.Filters))
		for i, inner := range f.Filters {
			out[i] = bindRefNew(inner, attrs)
		}
		f.Filters = out
		return f
	case *LogicalFilter:
		return bindRefNew(*f, attrs)
	case NotFilter:
		f.Filter = bindRefNew(f.Filter, attrs)
		return f
	case *NotFilter:
		return bindRefNew(*f, attrs)
	default:
		return filter
	}
}

func resolveRefNew(v interface{}, attrs map[string]interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok || attrs == nil {
		return v
	}
	ref, ok := m["$ref_new"]
	if !ok {
		return v
	}
	return attrs[fmt.Sprint(ref)]
}
