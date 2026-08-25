package turbopg

import (
	"testing"
)

func TestBuildFilterSQL(t *testing.T) {
	tests := []struct {
		name      string
		filter    interface{} // Changed from Filter to interface{} to allow non-Filter values
		wantSQL   string
		wantArgs  []interface{}
		wantError bool
	}{
		{
			name: "simple equality",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "electronics",
			},
			wantSQL:   "attributes->>'category' = $1",
			wantArgs:  []interface{}{"electronics"},
			wantError: false,
		},
		{
			name: "numeric less than",
			filter: FilterCondition{
				Field: "price",
				Op:    FilterOpLt,
				Value: 100,
			},
			wantSQL:   "((attributes->>'price')::numeric IS NULL OR (attributes->>'price')::numeric < $1)",
			wantArgs:  []interface{}{100},
			wantError: false,
		},
		{
			name: "complex AND filter",
			filter: LogicalFilter{
				Op: LogicalOpAnd,
				Filters: []Filter{
					FilterCondition{
						Field: "category",
						Op:    FilterOpEq,
						Value: "electronics",
					},
					FilterCondition{
						Field: "price",
						Op:    FilterOpGte,
						Value: 150,
					},
					FilterCondition{
						Field: "inStock",
						Op:    FilterOpEq,
						Value: false,
					},
				},
			},
			wantSQL:   "(attributes->>'category' = $1) AND ((attributes->>'price')::numeric >= $2) AND (attributes->>'inStock' = $3)",
			wantArgs:  []interface{}{"electronics", 150, false},
			wantError: false,
		},
		{
			name: "complex OR filter",
			filter: LogicalFilter{
				Op: LogicalOpOr,
				Filters: []Filter{
					FilterCondition{
						Field: "category",
						Op:    FilterOpEq,
						Value: "books",
					},
					FilterCondition{
						Field: "price",
						Op:    FilterOpGt,
						Value: 150,
					},
				},
			},
			wantSQL:   "(attributes->>'category' = $1) OR ((attributes->>'price')::numeric > $2)",
			wantArgs:  []interface{}{"books", 150},
			wantError: false,
		},
		{
			name: "nested logical filters",
			filter: LogicalFilter{
				Op: LogicalOpOr,
				Filters: []Filter{
					LogicalFilter{
						Op: LogicalOpAnd,
						Filters: []Filter{
							FilterCondition{
								Field: "category",
								Op:    FilterOpEq,
								Value: "electronics",
							},
							FilterCondition{
								Field: "price",
								Op:    FilterOpLt,
								Value: 150,
							},
						},
					},
					LogicalFilter{
						Op: LogicalOpAnd,
						Filters: []Filter{
							FilterCondition{
								Field: "category",
								Op:    FilterOpEq,
								Value: "books",
							},
							FilterCondition{
								Field: "price",
								Op:    FilterOpLt,
								Value: 20,
							},
						},
					},
				},
			},
			wantSQL:   "((attributes->>'category' = $1) AND (((attributes->>'price')::numeric IS NULL OR (attributes->>'price')::numeric < $2))) OR ((attributes->>'category' = $3) AND (((attributes->>'price')::numeric IS NULL OR (attributes->>'price')::numeric < $4)))",
			wantArgs:  []interface{}{"electronics", 150, "books", 20},
			wantError: false,
		},
		{
			name: "IN condition",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpIn,
				Value: []interface{}{"books", "electronics"},
			},
			wantSQL:   "((attributes->'category' @> $1::jsonb) OR (attributes->'category' @> $2::jsonb))",
			wantArgs:  []interface{}{`"books"`, `"electronics"`},
			wantError: false,
		},
		{
			name: "IN scalar on array attribute",
			filter: FilterCondition{
				Field: "users",
				Op:    FilterOpIn,
				Value: "bojan",
			},
			wantSQL:   "(attributes->'users' @> $1::jsonb)",
			wantArgs:  []interface{}{`"bojan"`},
			wantError: false,
		},
		{
			name: "NOT IN condition",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpNotIn,
				Value: []interface{}{"clothing", "food"},
			},
			wantSQL:   "NOT (((attributes->'category' @> $1::jsonb) OR (attributes->'category' @> $2::jsonb)))",
			wantArgs:  []interface{}{`"clothing"`, `"food"`},
			wantError: false,
		},
		{
			name: "id equality",
			filter: FilterCondition{
				Field: "id",
				Op:    FilterOpEq,
				Value: 1,
			},
			wantSQL:   "id = $1",
			wantArgs:  []interface{}{"1"},
			wantError: false,
		},
		{
			name: "id numeric comparison",
			filter: FilterCondition{
				Field: "id",
				Op:    FilterOpGte,
				Value: 0,
			},
			wantSQL:   "id::numeric >= $1",
			wantArgs:  []interface{}{0},
			wantError: false,
		},
		{
			name: "glob like",
			filter: FilterCondition{
				Field: "name",
				Op:    FilterOpGlob,
				Value: "foo%",
			},
			wantSQL:   "attributes->>'name' LIKE $1",
			wantArgs:  []interface{}{"foo%"},
			wantError: false,
		},
		{
			name: "not glob",
			filter: FilterCondition{
				Field: "name",
				Op:    FilterOpNotGlob,
				Value: "foo%",
			},
			wantSQL:   "attributes->>'name' NOT LIKE $1",
			wantArgs:  []interface{}{"foo%"},
			wantError: false,
		},
		{
			name: "iglob",
			filter: FilterCondition{
				Field: "name",
				Op:    FilterOpIGlob,
				Value: "FOO%",
			},
			wantSQL:   "attributes->>'name' ILIKE $1",
			wantArgs:  []interface{}{"FOO%"},
			wantError: false,
		},
		{
			name: "not iglob",
			filter: FilterCondition{
				Field: "name",
				Op:    FilterOpNotIGlob,
				Value: "FOO%",
			},
			wantSQL:   "attributes->>'name' NOT ILIKE $1",
			wantArgs:  []interface{}{"FOO%"},
			wantError: false,
		},
		{
			name: "not equal",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpNotEq,
				Value: "books",
			},
			wantSQL:   "attributes->>'category' != $1",
			wantArgs:  []interface{}{"books"},
			wantError: false,
		},
		{
			name: "lte",
			filter: FilterCondition{
				Field: "price",
				Op:    FilterOpLte,
				Value: 10,
			},
			wantSQL:   "((attributes->>'price')::numeric IS NULL OR (attributes->>'price')::numeric <= $1)",
			wantArgs:  []interface{}{10},
			wantError: false,
		},
		{
			name: "pointer condition",
			filter: &FilterCondition{
				Field: "category",
				Op:    FilterOpEq,
				Value: "x",
			},
			wantSQL:   "attributes->>'category' = $1",
			wantArgs:  []interface{}{"x"},
			wantError: false,
		},
		{
			name: "pointer logical",
			filter: &LogicalFilter{
				Op: LogicalOpAnd,
				Filters: []Filter{
					FilterCondition{Field: "a", Op: FilterOpEq, Value: "1"},
				},
			},
			wantSQL:   "(attributes->>'a' = $1)",
			wantArgs:  []interface{}{"1"},
			wantError: false,
		},
		{
			name: "contains all tokens",
			filter: FilterCondition{
				Field: "text",
				Op:    FilterOpContainsAllTokens,
				Value: "lazy walrus",
			},
			wantSQL:   "to_tsvector('simple', COALESCE((attributes->>'text'), '')) @@ to_tsquery('simple', $1)",
			wantArgs:  []interface{}{"lazy & walrus"},
			wantError: false,
		},
		{
			name: "not contains",
			filter: FilterCondition{
				Field: "name",
				Op:    FilterOpNotContains,
				Value: "wal",
			},
			wantSQL:   "NOT (attributes->'name' @> $1::jsonb)",
			wantArgs:  []interface{}{`"wal"`},
			wantError: false,
		},
		{
			name: "contains array membership",
			filter: FilterCondition{
				Field: "users",
				Op:    FilterOpContains,
				Value: "simon",
			},
			wantSQL:   "attributes->'users' @> $1::jsonb",
			wantArgs:  []interface{}{`"simon"`},
			wantError: false,
		},
		{
			name: "unsupported op",
			filter: FilterCondition{
				Field: "x",
				Op:    FilterOp("Nope"),
				Value: 1,
			},
			wantError: true,
		},
		{
			name: "empty logical filter",
			filter: LogicalFilter{
				Op:      LogicalOpAnd,
				Filters: []Filter{},
			},
			wantError: true,
		},
		{
			name:      "invalid filter type",
			filter:    struct{}{}, // Now valid since filter is interface{}
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			var sql string
			var args []interface{}

			// Handle type assertion error for invalid filter type
			if filter, ok := tt.filter.(Filter); ok {
				sql, args, err = buildFilterSQL(filter)
			} else {
				err = ErrInvalidFilterType
			}

			if (err != nil) != tt.wantError {
				t.Errorf("buildFilterCondition() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if err != nil {
				return
			}

			if sql != tt.wantSQL {
				t.Errorf("buildFilterCondition() sql = %v, want %v", sql, tt.wantSQL)
			}

			if len(args) != len(tt.wantArgs) {
				t.Errorf("buildFilterCondition() got %d args, want %d args", len(args), len(tt.wantArgs))
				return
			}

			for i, arg := range args {
				if arg != tt.wantArgs[i] {
					t.Errorf("buildFilterCondition() arg[%d] = %v, want %v", i, arg, tt.wantArgs[i])
				}
			}
		})
	}
}
