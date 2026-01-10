//go:build !windows

package service

import (
	"testing"
)

// TestServiceName verifies ServiceName returns the expected constant
func TestServiceName(t *testing.T) {
	name := ServiceName()
	if name != "sql-proxy" {
		t.Errorf("expected ServiceName()='sql-proxy', got %q", name)
	}
}

// TestIsWindowsService verifies IsWindowsService returns false on non-Windows
func TestIsWindowsService(t *testing.T) {
	result := IsWindowsService()
	if result {
		t.Error("expected IsWindowsService()=false on non-Windows platform")
	}
}
