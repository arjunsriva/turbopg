package main

import (
	"encoding/json"
	"fmt"

	"github.com/arjunsriva/turbopg"
)

func parseFilter(raw interface{}) (turbopg.Filter, error) {
	if raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("filter must be an array")
	}
	if len(arr) == 0 {
		return nil, nil
	}
	first, ok := arr[0].(string)
	if !ok {
		return nil, fmt.Errorf("filter[0] must be a string")
	}
	switch first {
	case "And", "Or":
		if len(arr) != 2 {
			return nil, fmt.Errorf("%s filter requires a list of conditions", first)
		}
		childrenRaw, err := asSlice(arr[1])
		if err != nil {
			return nil, err
		}
		children := make([]turbopg.Filter, 0, len(childrenRaw))
		for _, child := range childrenRaw {
			parsed, err := parseFilter(child)
			if err != nil {
				return nil, err
			}
			if parsed != nil {
				children = append(children, parsed)
			}
		}
		op := turbopg.LogicalOpAnd
		if first == "Or" {
			op = turbopg.LogicalOpOr
		}
		return turbopg.LogicalFilter{Op: op, Filters: children}, nil
	case "Not":
		if len(arr) != 2 {
			return nil, fmt.Errorf("Not filter requires an inner condition")
		}
		inner, err := parseFilter(arr[1])
		if err != nil {
			return nil, err
		}
		return turbopg.NotFilter{Filter: inner}, nil
	default:
		if len(arr) != 3 {
			return nil, fmt.Errorf("condition filter must be [field, op, value]")
		}
		opStr, ok := arr[1].(string)
		if !ok {
			return nil, fmt.Errorf("filter operator must be a string")
		}
		return turbopg.FilterCondition{
			Field: first,
			Op:    turbopg.FilterOp(opStr),
			Value: normalizeFilterValue(arr[2]),
		}, nil
	}
}

func normalizeFilterValue(v interface{}) interface{} {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return f
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, x := range t {
			out[i] = normalizeFilterValue(x)
		}
		return out
	default:
		return v
	}
}
