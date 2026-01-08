//go:build !windows

package service

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"sql-proxy/internal/config"
	"sql-proxy/internal/server"
)

// Run starts the server (non-Windows version)
func Run(cfg *config.Config) error {
	srv, err := server.New(cfg, true) // Always interactive on non-Windows
	if err != nil {
		return err
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start()
	}()

	select {
	case err := <-errChan:
		return err
	case <-sigChan:
		return srv.Shutdown(context.Background())
	}
}

// Install is a no-op on non-Windows
func Install(exePath, configPath string) error {
	return nil
}

// Uninstall is a no-op on non-Windows
func Uninstall() error {
	return nil
}

// IsWindowsService always returns false on non-Windows
func IsWindowsService() bool {
	return false
}
