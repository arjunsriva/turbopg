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
			wantSQL:   "(attributes->>'price')::numeric < $1",
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
			wantSQL:   "((attributes->>'category' = $1) AND ((attributes->>'price')::numeric < $2)) OR ((attributes->>'category' = $3) AND ((attributes->>'price')::numeric < $4))",
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
			wantSQL:   "attributes->>'category' IN ($1,$2)",
			wantArgs:  []interface{}{"books", "electronics"},
			wantError: false,
		},
		{
			name: "NOT IN condition",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpNotIn,
				Value: []interface{}{"clothing", "food"},
			},
			wantSQL:   "attributes->>'category' NOT IN ($1,$2)",
			wantArgs:  []interface{}{"clothing", "food"},
			wantError: false,
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
		{
			name: "invalid IN value type",
			filter: FilterCondition{
				Field: "category",
				Op:    FilterOpIn,
				Value: "not an array",
			},
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
