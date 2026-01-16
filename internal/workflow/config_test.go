package workflow

import (
	"testing"
)

func TestStepConfig_StepType(t *testing.T) {
	tests := []struct {
		name     string
		step     StepConfig
		expected string
	}{
		{
			name:     "explicit query type",
			step:     StepConfig{Type: "query"},
			expected: "query",
		},
		{
			name:     "implicit query from SQL",
			step:     StepConfig{SQL: "SELECT 1"},
			expected: "query",
		},
		{
			name:     "explicit httpcall type",
			step:     StepConfig{Type: "httpcall"},
			expected: "httpcall",
		},
		{
			name:     "implicit httpcall from URL",
			step:     StepConfig{URL: "https://example.com"},
			expected: "httpcall",
		},
		{
			name:     "explicit response type",
			step:     StepConfig{Type: "response"},
			expected: "response",
		},
		{
			name:     "implicit response from Template",
			step:     StepConfig{Template: `{"status": "ok"}`},
			expected: "response",
		},
		{
			name:     "block from nested Steps",
			step:     StepConfig{Steps: []StepConfig{{SQL: "SELECT 1"}}},
			expected: "block",
		},
		{
			name:     "unknown with empty config",
			step:     StepConfig{},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.StepType()
			if got != tt.expected {
				t.Errorf("StepType() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStepConfig_IsBlock(t *testing.T) {
	tests := []struct {
		name     string
		step     StepConfig
		expected bool
	}{
		{
			name:     "block with nested Steps",
			step:     StepConfig{Steps: []StepConfig{{SQL: "SELECT 1"}}},
			expected: true,
		},
		{
			name:     "not block - query",
			step:     StepConfig{Type: "query", SQL: "SELECT 1"},
			expected: false,
		},
		{
			name:     "not block - empty",
			step:     StepConfig{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.IsBlock()
			if got != tt.expected {
				t.Errorf("IsBlock() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStepConfig_IsQuery(t *testing.T) {
	tests := []struct {
		name     string
		step     StepConfig
		expected bool
	}{
		{
			name:     "explicit type",
			step:     StepConfig{Type: "query"},
			expected: true,
		},
		{
			name:     "implicit from SQL",
			step:     StepConfig{SQL: "SELECT 1"},
			expected: true,
		},
		{
			name:     "not query - httpcall",
			step:     StepConfig{Type: "httpcall"},
			expected: false,
		},
		{
			name:     "not query - empty",
			step:     StepConfig{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.IsQuery()
			if got != tt.expected {
				t.Errorf("IsQuery() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStepConfig_IsHTTPCall(t *testing.T) {
	tests := []struct {
		name     string
		step     StepConfig
		expected bool
	}{
		{
			name:     "explicit type",
			step:     StepConfig{Type: "httpcall"},
			expected: true,
		},
		{
			name:     "implicit from URL",
			step:     StepConfig{URL: "https://api.example.com"},
			expected: true,
		},
		{
			name:     "not httpcall - query",
			step:     StepConfig{Type: "query"},
			expected: false,
		},
		{
			name:     "not httpcall - empty",
			step:     StepConfig{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.IsHTTPCall()
			if got != tt.expected {
				t.Errorf("IsHTTPCall() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStepConfig_IsResponse(t *testing.T) {
	tests := []struct {
		name     string
		step     StepConfig
		expected bool
	}{
		{
			name:     "explicit type",
			step:     StepConfig{Type: "response"},
			expected: true,
		},
		{
			name:     "not response - query",
			step:     StepConfig{Type: "query"},
			expected: false,
		},
		{
			name:     "not response - implicit response (has Template but not type)",
			step:     StepConfig{Template: `{"ok": true}`},
			expected: false, // IsResponse only checks Type field
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.step.IsResponse()
			if got != tt.expected {
				t.Errorf("IsResponse() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRateLimitRefConfig(t *testing.T) {
	t.Run("pool reference", func(t *testing.T) {
		cfg := RateLimitRefConfig{Pool: "default"}
		if cfg.Pool == "" {
			t.Error("expected pool reference")
		}
	})

	t.Run("inline config", func(t *testing.T) {
		cfg := RateLimitRefConfig{
			RequestsPerSecond: 10,
			Burst:             20,
			Key:               "{{.ClientIP}}",
		}
		if cfg.Pool != "" {
			t.Error("expected inline config, not pool reference")
		}
	})
}
