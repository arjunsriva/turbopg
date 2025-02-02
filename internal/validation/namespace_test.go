package validation

import (
	"testing"
)

func TestValidateNamespace(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{
			name:    "valid namespace",
			input:   "test_namespace_123",
			wantErr: nil,
		},
		{
			name:    "empty namespace",
			input:   "",
			wantErr: ErrEmptyNamespace,
		},
		{
			name:    "too long namespace",
			input:   "this_namespace_is_way_too_long_and_exceeds_the_postgres_identifier_limit_by_a_lot",
			wantErr: ErrNamespaceTooLong,
		},
		{
			name:    "starts with number",
			input:   "1namespace",
			wantErr: ErrInvalidNamespaceStart,
		},
		{
			name:    "invalid characters",
			input:   "test-namespace",
			wantErr: ErrInvalidNamespaceChar,
		},
		{
			name:    "reserved pg_ prefix",
			input:   "pg_namespace",
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "reserved vector_ prefix",
			input:   "vector_namespace",
			wantErr: ErrReservedNamespace,
		},
		{
			name:    "starts with underscore",
			input:   "_namespace",
			wantErr: nil,
		},
		{
			name:    "mixed case and numbers",
			input:   "Test_Namespace_123",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.input)
			if err != tt.wantErr {
				t.Errorf("ValidateNamespace() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
