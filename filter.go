package turbopg

import (
	"errors"
	"fmt"
	"strings"
)

// Common errors
var (
	ErrInvalidFilterType = errors.New("invalid filter type")
)

// FilterOp represents the type of filter operation
type FilterOp string

const (
	FilterOpEq       FilterOp = "Eq"
	FilterOpNotEq    FilterOp = "NotEq"
	FilterOpIn       FilterOp = "In"
	FilterOpNotIn    FilterOp = "NotIn"
	FilterOpLt       FilterOp = "Lt"
	FilterOpLte      FilterOp = "Lte"
	FilterOpGt       FilterOp = "Gt"
	FilterOpGte      FilterOp = "Gte"
	FilterOpGlob     FilterOp = "Glob"
	FilterOpNotGlob  FilterOp = "NotGlob"
	FilterOpIGlob    FilterOp = "IGlob"
	FilterOpNotIGlob FilterOp = "NotIGlob"
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

// buildFilterSQL converts a Filter to SQL WHERE clause and args
func buildFilterSQL(filter Filter) (string, []interface{}, error) {
	switch f := filter.(type) {
	case FilterCondition:
		return buildSimpleFilterSQL(f)
	case LogicalFilter:
		return buildLogicalFilterSQL(f)
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
		op = "="
	case FilterOpNotEq:
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
	case FilterOpNotGlob:
		op = "NOT LIKE"
	case FilterOpIGlob:
		op = "ILIKE"
	case FilterOpNotIGlob:
		op = "NOT ILIKE"
	case FilterOpIn:
		return buildInConditionSQL(f, false)
	case FilterOpNotIn:
		return buildInConditionSQL(f, true)
	default:
		return "", nil, fmt.Errorf("unsupported filter operation: %s", f.Op)
	}

	if needsCast {
		return fmt.Sprintf("(attributes->>'%s')::numeric %s $1", f.Field, op), []interface{}{f.Value}, nil
	}
	return fmt.Sprintf("attributes->>'%s' %s $1", f.Field, op), []interface{}{f.Value}, nil
}

// buildInConditionSQL handles IN and NOT IN conditions
func buildInConditionSQL(f FilterCondition, not bool) (string, []interface{}, error) {
	values, ok := f.Value.([]interface{})
	if !ok {
		return "", nil, fmt.Errorf("IN/NOT IN requires array value, got %T", f.Value)
	}

	placeholder := make([]string, len(values))
	for i := range values {
		placeholder[i] = fmt.Sprintf("$%d", i+1)
	}

	op := "IN"
	if not {
		op = "NOT IN"
	}

	return fmt.Sprintf("attributes->>'%s' %s (%s)",
			f.Field,
			op,
			strings.Join(placeholder, ",")),
		values,
		nil
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

		// Adjust placeholders for accumulated args
		for i := 1; i <= strings.Count(cond, "$"); i++ {
			old := fmt.Sprintf("$%d", i)
			new := fmt.Sprintf("$%d", i+argOffset)
			cond = strings.Replace(cond, old, new, -1)
		}

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

// buildFilterCondition converts a Filter to SQL WHERE clause and args
func (s *Store) buildFilterCondition(filter Filter) (string, []interface{}, error) {
	return buildFilterSQL(filter)
}

// buildSimpleFilterCondition converts a FilterCondition to SQL
func (s *Store) buildSimpleFilterCondition(f FilterCondition) (string, []interface{}, error) {
	return buildSimpleFilterSQL(f)
}

// buildInCondition handles IN and NOT IN conditions
func (s *Store) buildInCondition(f FilterCondition, not bool) (string, []interface{}, error) {
	return buildInConditionSQL(f, not)
}

// buildLogicalFilterCondition converts a LogicalFilter to SQL
func (s *Store) buildLogicalFilterCondition(f LogicalFilter) (string, []interface{}, error) {
	return buildLogicalFilterSQL(f)
}