package types

import (
	"testing"
	"time"
)

func TestIsArrayType(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		want     bool
	}{
		{"int array", "int[]", true},
		{"string array", "string[]", true},
		{"float array", "float[]", true},
		{"bool array", "bool[]", true},
		{"simple string", "string", false},
		{"simple int", "int", false},
		{"json type", "json", false},
		{"empty string", "", false},
		{"just brackets", "[]", false},
		{"single bracket", "[", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsArrayType(tt.typeName); got != tt.want {
				t.Errorf("IsArrayType(%q) = %v, want %v", tt.typeName, got, tt.want)
			}
		})
	}
}

func TestArrayBaseType(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		want     string
	}{
		{"int array", "int[]", "int"},
		{"string array", "string[]", "string"},
		{"float array", "float[]", "float"},
		{"bool array", "bool[]", "bool"},
		{"not an array", "string", "string"},
		{"not an array int", "int", "int"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ArrayBaseType(tt.typeName); got != tt.want {
				t.Errorf("ArrayBaseType(%q) = %v, want %v", tt.typeName, got, tt.want)
			}
		})
	}
}

func TestConvertValue(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		typeName  string
		wantErr   bool
		checkFunc func(t *testing.T, got any)
	}{
		{
			name:     "string type",
			value:    "hello",
			typeName: "string",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != "hello" {
					t.Errorf("expected 'hello', got %v", got)
				}
			},
		},
		{
			name:     "int type",
			value:    "42",
			typeName: "int",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != 42 {
					t.Errorf("expected 42, got %v", got)
				}
			},
		},
		{
			name:     "integer type alias",
			value:    "42",
			typeName: "integer",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != 42 {
					t.Errorf("expected 42, got %v", got)
				}
			},
		},
		{
			name:     "invalid int",
			value:    "notanumber",
			typeName: "int",
			wantErr:  true,
		},
		{
			name:     "bool true",
			value:    "true",
			typeName: "bool",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != true {
					t.Errorf("expected true, got %v", got)
				}
			},
		},
		{
			name:     "bool false",
			value:    "false",
			typeName: "boolean",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != false {
					t.Errorf("expected false, got %v", got)
				}
			},
		},
		{
			name:     "float type",
			value:    "3.14",
			typeName: "float",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != 3.14 {
					t.Errorf("expected 3.14, got %v", got)
				}
			},
		},
		{
			name:     "double type alias",
			value:    "3.14",
			typeName: "double",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != 3.14 {
					t.Errorf("expected 3.14, got %v", got)
				}
			},
		},
		{
			name:     "datetime RFC3339",
			value:    "2024-01-15T10:30:00Z",
			typeName: "datetime",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				tm, ok := got.(time.Time)
				if !ok {
					t.Errorf("expected time.Time, got %T", got)
					return
				}
				if tm.Year() != 2024 || tm.Month() != 1 || tm.Day() != 15 {
					t.Errorf("unexpected date: %v", tm)
				}
			},
		},
		{
			name:     "date simple format",
			value:    "2024-01-15",
			typeName: "date",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				tm, ok := got.(time.Time)
				if !ok {
					t.Errorf("expected time.Time, got %T", got)
					return
				}
				if tm.Year() != 2024 || tm.Month() != 1 || tm.Day() != 15 {
					t.Errorf("unexpected date: %v", tm)
				}
			},
		},
		{
			name:     "invalid datetime",
			value:    "not-a-date",
			typeName: "datetime",
			wantErr:  true,
		},
		{
			name:     "json object",
			value:    `{"key": "value"}`,
			typeName: "json",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				s, ok := got.(string)
				if !ok {
					t.Errorf("expected string, got %T", got)
					return
				}
				if s != `{"key":"value"}` {
					t.Errorf("expected compact JSON, got %v", s)
				}
			},
		},
		{
			name:     "invalid json",
			value:    `{invalid`,
			typeName: "json",
			wantErr:  true,
		},
		{
			name:     "int array",
			value:    `[1, 2, 3]`,
			typeName: "int[]",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				s, ok := got.(string)
				if !ok {
					t.Errorf("expected string, got %T", got)
					return
				}
				if s != `[1,2,3]` {
					t.Errorf("expected [1,2,3], got %v", s)
				}
			},
		},
		{
			name:     "string array",
			value:    `["a", "b", "c"]`,
			typeName: "string[]",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				s, ok := got.(string)
				if !ok {
					t.Errorf("expected string, got %T", got)
					return
				}
				if s != `["a","b","c"]` {
					t.Errorf("expected [\"a\",\"b\",\"c\"], got %v", s)
				}
			},
		},
		{
			name:     "invalid array json",
			value:    `not an array`,
			typeName: "int[]",
			wantErr:  true,
		},
		{
			name:     "array type mismatch",
			value:    `["a", "b"]`,
			typeName: "int[]",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertValue(tt.value, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(t, got)
			}
		})
	}
}

func TestConvertJSONValue(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		typeName  string
		wantErr   bool
		checkFunc func(t *testing.T, got any)
	}{
		{
			name:     "string from string",
			value:    "hello",
			typeName: "string",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != "hello" {
					t.Errorf("expected 'hello', got %v", got)
				}
			},
		},
		{
			name:     "int from float64",
			value:    float64(42),
			typeName: "int",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != 42 {
					t.Errorf("expected 42, got %v", got)
				}
			},
		},
		{
			name:     "int from invalid type",
			value:    "notanumber",
			typeName: "int",
			wantErr:  true,
		},
		{
			name:     "float from float64",
			value:    float64(3.14),
			typeName: "float",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != 3.14 {
					t.Errorf("expected 3.14, got %v", got)
				}
			},
		},
		{
			name:     "float from invalid type",
			value:    "notanumber",
			typeName: "float",
			wantErr:  true,
		},
		{
			name:     "bool from bool",
			value:    true,
			typeName: "bool",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != true {
					t.Errorf("expected true, got %v", got)
				}
			},
		},
		{
			name:     "bool from invalid type",
			value:    float64(1),
			typeName: "bool",
			wantErr:  true,
		},
		{
			name:     "datetime from string",
			value:    "2024-01-15T10:30:00Z",
			typeName: "datetime",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				tm, ok := got.(time.Time)
				if !ok {
					t.Errorf("expected time.Time, got %T", got)
					return
				}
				if tm.Year() != 2024 {
					t.Errorf("unexpected year: %v", tm.Year())
				}
			},
		},
		{
			name:     "datetime from non-string",
			value:    float64(123),
			typeName: "datetime",
			wantErr:  true,
		},
		{
			name:     "json object",
			value:    map[string]any{"key": "value"},
			typeName: "json",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				// JSON type should preserve native Go types for iteration support
				m, ok := got.(map[string]any)
				if !ok {
					t.Errorf("expected map[string]any, got %T", got)
					return
				}
				if m["key"] != "value" {
					t.Errorf("expected key=value, got %v", m["key"])
				}
			},
		},
		{
			name:     "int array",
			value:    []any{float64(1), float64(2), float64(3)},
			typeName: "int[]",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				s, ok := got.(string)
				if !ok {
					t.Errorf("expected string, got %T", got)
					return
				}
				if s != `[1,2,3]` {
					t.Errorf("expected [1,2,3], got %v", s)
				}
			},
		},
		{
			name:     "array type mismatch",
			value:    []any{"a", "b"},
			typeName: "int[]",
			wantErr:  true,
		},
		{
			name:     "not an array for array type",
			value:    "not an array",
			typeName: "int[]",
			wantErr:  true,
		},
		{
			name:     "string from float64",
			value:    float64(42.5),
			typeName: "string",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != "42.5" {
					t.Errorf("expected '42.5', got %v", got)
				}
			},
		},
		{
			name:     "string from bool",
			value:    true,
			typeName: "string",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != "true" {
					t.Errorf("expected 'true', got %v", got)
				}
			},
		},
		{
			name:     "string from nil",
			value:    nil,
			typeName: "string",
			wantErr:  false,
			checkFunc: func(t *testing.T, got any) {
				if got != "" {
					t.Errorf("expected empty string, got %v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertJSONValue(tt.value, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertJSONValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(t, got)
			}
		})
	}
}

func TestValidateArrayElements(t *testing.T) {
	tests := []struct {
		name     string
		arr      []any
		baseType string
		wantErr  bool
		wantLen  int
	}{
		{
			name:     "valid int array",
			arr:      []any{float64(1), float64(2), float64(3)},
			baseType: "int",
			wantErr:  false,
			wantLen:  3,
		},
		{
			name:     "valid integer array",
			arr:      []any{float64(1), float64(2)},
			baseType: "integer",
			wantErr:  false,
			wantLen:  2,
		},
		{
			name:     "valid float array",
			arr:      []any{float64(1.5), float64(2.5)},
			baseType: "float",
			wantErr:  false,
			wantLen:  2,
		},
		{
			name:     "valid double array",
			arr:      []any{float64(1.5), float64(2.5)},
			baseType: "double",
			wantErr:  false,
			wantLen:  2,
		},
		{
			name:     "valid bool array",
			arr:      []any{true, false},
			baseType: "bool",
			wantErr:  false,
			wantLen:  2,
		},
		{
			name:     "valid boolean array",
			arr:      []any{true, false},
			baseType: "boolean",
			wantErr:  false,
			wantLen:  2,
		},
		{
			name:     "valid string array",
			arr:      []any{"a", "b", "c"},
			baseType: "string",
			wantErr:  false,
			wantLen:  3,
		},
		{
			name:     "invalid int array - string element",
			arr:      []any{float64(1), "not a number"},
			baseType: "int",
			wantErr:  true,
		},
		{
			name:     "invalid float array - string element",
			arr:      []any{float64(1.5), "not a number"},
			baseType: "float",
			wantErr:  true,
		},
		{
			name:     "invalid bool array - int element",
			arr:      []any{true, float64(1)},
			baseType: "bool",
			wantErr:  true,
		},
		{
			name:     "invalid string array - int element",
			arr:      []any{"a", float64(1)},
			baseType: "string",
			wantErr:  true,
		},
		{
			name:     "unknown type - passes through",
			arr:      []any{"a", float64(1), true},
			baseType: "unknown",
			wantErr:  false,
			wantLen:  3,
		},
		{
			name:     "empty array",
			arr:      []any{},
			baseType: "int",
			wantErr:  false,
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateArrayElements(tt.arr, tt.baseType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateArrayElements() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("ValidateArrayElements() returned %d elements, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestValidParamTypes(t *testing.T) {
	expectedTypes := []string{
		"string", "int", "integer", "float", "double",
		"bool", "boolean", "datetime", "date", "json",
		"int[]", "string[]", "float[]", "bool[]",
	}

	for _, typ := range expectedTypes {
		if !ValidParamTypes[typ] {
			t.Errorf("ValidParamTypes should include %q", typ)
		}
	}

	invalidTypes := []string{"invalid", "array", "object", ""}
	for _, typ := range invalidTypes {
		if ValidParamTypes[typ] {
			t.Errorf("ValidParamTypes should not include %q", typ)
		}
	}
}
