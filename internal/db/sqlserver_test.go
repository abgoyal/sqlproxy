package db

import (
	"database/sql"
	"testing"
)

// TestIsolationToSQL tests conversion of config isolation strings to SQL Server syntax
func TestIsolationToSQL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"read_uncommitted", "READ UNCOMMITTED"},
		{"read_committed", "READ COMMITTED"},
		{"repeatable_read", "REPEATABLE READ"},
		{"serializable", "SERIALIZABLE"},
		{"snapshot", "SNAPSHOT"},
		{"", "READ COMMITTED"},      // default
		{"invalid", "READ COMMITTED"}, // fallback
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := isolationToSQL(tc.input)
			if result != tc.expected {
				t.Errorf("isolationToSQL(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// TestDeadlockPriorityToSQL tests conversion of config deadlock priority strings to SQL Server syntax
func TestDeadlockPriorityToSQL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"low", "LOW"},
		{"normal", "NORMAL"},
		{"high", "HIGH"},
		{"", "LOW"},         // default
		{"invalid", "LOW"},  // fallback
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := deadlockPriorityToSQL(tc.input)
			if result != tc.expected {
				t.Errorf("deadlockPriorityToSQL(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// TestSQLServerDriver_BuildArgs verifies parameter extraction from SQL
func TestSQLServerDriver_BuildArgs(t *testing.T) {
	d := &SQLServerDriver{}

	tests := []struct {
		name          string
		query         string
		params        map[string]any
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "no params",
			query:         "SELECT * FROM users",
			params:        map[string]any{},
			expectedCount: 0,
			expectedNames: nil,
		},
		{
			name:          "single param",
			query:         "SELECT * FROM users WHERE id = @id",
			params:        map[string]any{"id": 1},
			expectedCount: 1,
			expectedNames: []string{"id"},
		},
		{
			name:          "multiple params",
			query:         "SELECT * FROM users WHERE name = @name AND age > @age",
			params:        map[string]any{"name": "test", "age": 18},
			expectedCount: 2,
			expectedNames: []string{"name", "age"},
		},
		{
			name:          "duplicate param in query",
			query:         "SELECT * FROM users WHERE name = @name OR alias = @name",
			params:        map[string]any{"name": "test"},
			expectedCount: 1, // Should deduplicate
			expectedNames: []string{"name"},
		},
		{
			name:          "param not in map",
			query:         "SELECT * FROM users WHERE id = @id",
			params:        map[string]any{}, // Empty map
			expectedCount: 1,                 // Still extracts param, value will be nil
			expectedNames: []string{"id"},
		},
		{
			name:          "extra param in map",
			query:         "SELECT * FROM users WHERE id = @id",
			params:        map[string]any{"id": 1, "unused": "ignored"},
			expectedCount: 1, // Only params in query are used
			expectedNames: []string{"id"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := d.buildArgs(tc.query, tc.params)

			if len(args) != tc.expectedCount {
				t.Errorf("buildArgs() returned %d args, want %d", len(args), tc.expectedCount)
			}

			// Verify args are sql.Named values
			for i, arg := range args {
				named, ok := arg.(sql.NamedArg)
				if !ok {
					t.Errorf("arg[%d] is not sql.NamedArg", i)
					continue
				}
				if i < len(tc.expectedNames) {
					if named.Name != tc.expectedNames[i] {
						t.Errorf("arg[%d].Name = %q, want %q", i, named.Name, tc.expectedNames[i])
					}
				}
			}
		})
	}
}

// TestSQLServerDriver_BuildArgs_Values verifies parameter values are correctly assigned
func TestSQLServerDriver_BuildArgs_Values(t *testing.T) {
	d := &SQLServerDriver{}

	query := "SELECT * FROM users WHERE name = @name AND age = @age AND active = @active"
	params := map[string]any{
		"name":   "John",
		"age":    25,
		"active": true,
	}

	args := d.buildArgs(query, params)

	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}

	// Check each arg
	for _, arg := range args {
		named := arg.(sql.NamedArg)
		switch named.Name {
		case "name":
			if named.Value != "John" {
				t.Errorf("name param value = %v, want 'John'", named.Value)
			}
		case "age":
			if named.Value != 25 {
				t.Errorf("age param value = %v, want 25", named.Value)
			}
		case "active":
			if named.Value != true {
				t.Errorf("active param value = %v, want true", named.Value)
			}
		}
	}
}

// TestSQLServerDriver_BuildArgs_NilValue verifies nil values are handled correctly
func TestSQLServerDriver_BuildArgs_NilValue(t *testing.T) {
	d := &SQLServerDriver{}

	query := "SELECT * FROM users WHERE (@status IS NULL OR status = @status)"
	params := map[string]any{
		"status": nil,
	}

	args := d.buildArgs(query, params)

	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}

	named := args[0].(sql.NamedArg)
	if named.Name != "status" {
		t.Errorf("name = %q, want 'status'", named.Name)
	}
	if named.Value != nil {
		t.Errorf("value = %v, want nil", named.Value)
	}
}

