package turbopg

import "testing"

func TestGetSystemTableName(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		table    string
		expected string
	}{
		{
			name:     "basic prefix and table",
			prefix:   "myapp_",
			table:    "migration_requests",
			expected: "myapp_sys_migration_requests",
		},
		{
			name:     "empty prefix",
			prefix:   "",
			table:    "migration_requests",
			expected: "sys_migration_requests",
		},
		{
			name:     "prefix without underscore",
			prefix:   "myapp",
			table:    "migration_requests",
			expected: "myappsys_migration_requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSystemTableName(tt.prefix, tt.table)
			if result != tt.expected {
				t.Errorf("GetSystemTableName(%q, %q) = %q, want %q",
					tt.prefix, tt.table, result, tt.expected)
			}
		})
	}
}

func TestGetNamespaceTableName(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		namespace string
		expected  string
	}{
		{
			name:      "basic prefix and namespace",
			prefix:    "myapp_",
			namespace: "documents",
			expected:  "myapp_ns_documents",
		},
		{
			name:      "empty prefix",
			prefix:    "",
			namespace: "documents",
			expected:  "ns_documents",
		},
		{
			name:      "prefix without underscore",
			prefix:    "myapp",
			namespace: "documents",
			expected:  "myappns_documents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetNamespaceTableName(tt.prefix, tt.namespace)
			if result != tt.expected {
				t.Errorf("GetNamespaceTableName(%q, %q) = %q, want %q",
					tt.prefix, tt.namespace, result, tt.expected)
			}
		})
	}
}

func TestGetNamespaceFromTableName(t *testing.T) {
	tests := []struct {
		name          string
		prefix        string
		tableName     string
		wantNamespace string
		wantIsValid   bool
	}{
		{
			name:          "valid namespace table",
			prefix:        "myapp_",
			tableName:     "myapp_ns_documents",
			wantNamespace: "documents",
			wantIsValid:   true,
		},
		{
			name:          "system table",
			prefix:        "myapp_",
			tableName:     "myapp_sys_migrations",
			wantNamespace: "",
			wantIsValid:   false,
		},
		{
			name:          "invalid prefix",
			prefix:        "myapp_",
			tableName:     "otherapp_ns_documents",
			wantNamespace: "",
			wantIsValid:   false,
		},
		{
			name:          "too short",
			prefix:        "myapp_",
			tableName:     "myapp_ns_",
			wantNamespace: "",
			wantIsValid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNamespace, gotIsValid := GetNamespaceFromTableName(tt.prefix, tt.tableName)
			if gotNamespace != tt.wantNamespace || gotIsValid != tt.wantIsValid {
				t.Errorf("GetNamespaceFromTableName(%q, %q) = (%q, %v), want (%q, %v)",
					tt.prefix, tt.tableName, gotNamespace, gotIsValid,
					tt.wantNamespace, tt.wantIsValid)
			}
		})
	}
}

func TestIsSystemTable(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		tableName string
		want      bool
	}{
		{
			name:      "system table",
			prefix:    "myapp_",
			tableName: "myapp_sys_migrations",
			want:      true,
		},
		{
			name:      "namespace table",
			prefix:    "myapp_",
			tableName: "myapp_ns_documents",
			want:      false,
		},
		{
			name:      "other table",
			prefix:    "myapp_",
			tableName: "some_other_table",
			want:      false,
		},
		{
			name:      "wrong prefix",
			prefix:    "myapp_",
			tableName: "otherapp_sys_migrations",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSystemTable(tt.prefix, tt.tableName)
			if got != tt.want {
				t.Errorf("IsSystemTable(%q, %q) = %v, want %v",
					tt.prefix, tt.tableName, got, tt.want)
			}
		})
	}
}


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
