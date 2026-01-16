package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInit_Stdout verifies logger initialization to stdout sets correct level
func TestInit_Stdout(t *testing.T) {
	err := Init("info", "", 100, 5, 30)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	// Verify level is set
	if GetLevel() != "info" {
		t.Errorf("expected level info, got %s", GetLevel())
	}
}

// TestInit_FileOutput tests logger initialization creates log directory and file
func TestInit_FileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "logs", "test.log")

	err := Init("debug", logPath, 10, 3, 7)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	// Verify log directory was created
	if _, err := os.Stat(filepath.Dir(logPath)); os.IsNotExist(err) {
		t.Error("log directory was not created")
	}
}

// TestSetLevel tests dynamic log level changes including case handling
func TestSetLevel(t *testing.T) {
	err := Init("info", "", 100, 5, 30)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	tests := []struct {
		set  string
		want string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"error", "error"},
		{"DEBUG", "debug"},
		{"WARN", "warn"},
		{"WARNING", "warn"},
		{"ERROR", "error"},
		{"invalid", "info"}, // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.set, func(t *testing.T) {
			SetLevel(tt.set)
			if got := GetLevel(); got != tt.want {
				t.Errorf("SetLevel(%s): GetLevel() = %s, want %s", tt.set, got, tt.want)
			}
		})
	}
}

// TestGetLevel verifies GetLevel returns correct string for each slog level
func TestGetLevel(t *testing.T) {
	err := Init("info", "", 100, 5, 30)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Close()

	// Set each level via levelVar directly and test GetLevel
	levelVar.Set(slog.LevelDebug)
	if GetLevel() != "debug" {
		t.Errorf("expected debug, got %s", GetLevel())
	}

	levelVar.Set(slog.LevelInfo)
	if GetLevel() != "info" {
		t.Errorf("expected info, got %s", GetLevel())
	}

	levelVar.Set(slog.LevelWarn)
	if GetLevel() != "warn" {
		t.Errorf("expected warn, got %s", GetLevel())
	}

	levelVar.Set(slog.LevelError)
	if GetLevel() != "error" {
		t.Errorf("expected error, got %s", GetLevel())
	}
}

// TestParseLevel tests string to slog.Level parsing with case insensitivity
func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},        // default
		{"unknown", slog.LevelInfo}, // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseLevel(tt.input); got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestMapToAttrs tests map to slog attribute slice conversion
func TestMapToAttrs(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		want int // expected length of result
	}{
		{"nil map", nil, 0},
		{"empty map", map[string]any{}, 0},
		{"one item", map[string]any{"key": "value"}, 2},
		{"two items", map[string]any{"a": 1, "b": 2}, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := mapToAttrs(tt.m)
			if len(attrs) != tt.want {
				t.Errorf("mapToAttrs() returned %d items, want %d", len(attrs), tt.want)
			}
		})
	}
}

// TestLogFunctions tests Debug, Info, Warn, Error output to buffer
func TestLogFunctions(t *testing.T) {
	// Initialize with a buffer to capture output
	var buf bytes.Buffer
	levelVar.Set(slog.LevelDebug)
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: levelVar})
	slog.SetDefault(slog.New(handler))

	// Test each logging function
	Debug("debug message", map[string]any{"key": "debug_value"})
	if !strings.Contains(buf.String(), "debug message") {
		t.Error("Debug message not logged")
	}

	buf.Reset()
	Info("info message", map[string]any{"key": "info_value"})
	if !strings.Contains(buf.String(), "info message") {
		t.Error("Info message not logged")
	}

	buf.Reset()
	Warn("warn message", map[string]any{"key": "warn_value"})
	if !strings.Contains(buf.String(), "warn message") {
		t.Error("Warn message not logged")
	}

	buf.Reset()
	Error("error message", map[string]any{"key": "error_value"})
	if !strings.Contains(buf.String(), "error message") {
		t.Error("Error message not logged")
	}
}

// TestLogFunctions_NilFields verifies log functions handle nil field maps without panic
func TestLogFunctions_NilFields(t *testing.T) {
	var buf bytes.Buffer
	levelVar.Set(slog.LevelDebug)
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: levelVar})
	slog.SetDefault(slog.New(handler))

	// Should not panic with nil fields
	Debug("test", nil)
	Info("test", nil)
	Warn("test", nil)
	Error("test", nil)
}

// TestClose_NoFile tests Close handles nil file closer gracefully
func TestClose_NoFile(t *testing.T) {
	// Reset closer
	closer = nil

	err := Close()
	if err != nil {
		t.Errorf("Close() returned error when no file: %v", err)
	}
}

// TestClose_WithFile tests Close properly closes log file handle
func TestClose_WithFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	err := Init("info", logPath, 10, 3, 7)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Write something to ensure file is created
	Info("test message", nil)

	err = Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

// TestInit_InvalidDirectory tests Init handles permission-denied paths without panic
func TestInit_InvalidDirectory(t *testing.T) {
	// Try to create log in non-existent parent with no permission
	// This test might not fail on all systems, so we just check it doesn't panic
	_ = Init("info", "/root/cannot/create/test.log", 10, 3, 7)
	Close()
}
