//go:build !windows

package service

import (
	"testing"
)

// TestDefaultServiceName verifies the default service name constant
func TestDefaultServiceName(t *testing.T) {
	// The default service name should be "sql-proxy"
	if defaultServiceName != "sql-proxy" {
		t.Errorf("expected defaultServiceName='sql-proxy', got %q", defaultServiceName)
	}
}

// TestJoinErrors verifies error joining function
func TestJoinErrors(t *testing.T) {
	errors := []string{"error1", "error2", "error3"}
	result := joinErrors(errors)
	expected := "error1\n  error2\n  error3"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
