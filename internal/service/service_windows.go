//go:build windows

package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"sql-proxy/internal/config"
	"sql-proxy/internal/server"
	"sql-proxy/internal/validate"
)

const defaultServiceName = "sql-proxy"
const shutdownTimeout = 30 * time.Second

// runningServiceName is set when running as a Windows service
var runningServiceName string

type windowsService struct {
	server *server.Server
}

func (ws *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	// Start the HTTP server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- ws.server.Start()
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case err := <-errChan:
			if err != nil {
				log.Printf("Server error: %v", err)
				return true, 1
			}
			return false, 0

		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus

			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				ws.server.Shutdown(ctx)
				cancel()
				return false, 0

			default:
				log.Printf("Unexpected control request: %v", c.Cmd)
			}
		}
	}
}

// Run starts the service.
// If interactive is true, runs in foreground with Ctrl+C handling.
// If interactive is false (daemon mode), runs as Windows service or background process.
func Run(cfg *config.Config, interactive bool) error {
	// Validate configuration before starting
	result := validate.Run(cfg)
	if !result.Valid {
		return fmt.Errorf("configuration validation failed:\n  %s", joinErrors(result.Errors))
	}

	srv, err := server.New(cfg, interactive)
	if err != nil {
		return err
	}

	if !interactive {
		// Daemon mode - check if we're actually running as a Windows service
		isWindowsService, _ := svc.IsWindowsService()
		if isWindowsService && runningServiceName != "" {
			// Running as a Windows service via SCM
			ws := &windowsService{server: srv}
			elog, err := eventlog.Open(runningServiceName)
			if err == nil {
				defer elog.Close()
			}
			return svc.Run(runningServiceName, ws)
		}
		// Daemon mode but not via SCM - just run without interactive output
	}

	// Running interactively or as daemon without SCM - handle Ctrl+C for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start()
	}()

	select {
	case err := <-errChan:
		return err
	case <-sigChan:
		if interactive {
			log.Println("Received interrupt, shutting down...")
		}
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// Install installs the service with the given name
func Install(name, exePath, configPath string) error {
	if name == "" {
		name = defaultServiceName
	}

	// Get absolute path and verify config file exists
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to resolve config path: %w", err)
	}
	if _, err := os.Stat(absConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", absConfigPath)
	}
	configPath = absConfigPath

	// Get absolute path for executable
	absExePath, err := filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}
	if _, err := os.Stat(absExePath); os.IsNotExist(err) {
		return fmt.Errorf("executable not found: %s", absExePath)
	}
	exePath = absExePath

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", name)
	}

	displayName := "SQL Proxy"
	if name != defaultServiceName {
		displayName = fmt.Sprintf("SQL Proxy (%s)", name)
	}
	desc := "SQL Proxy Service - HTTP endpoints for SQL Server and SQLite databases"

	// Include --daemon and --service-name flags for proper daemon mode
	s, err = m.CreateService(name, exePath, mgr.Config{
		DisplayName: displayName,
		Description: desc,
		StartType:   mgr.StartAutomatic,
	}, "--daemon", "--service-name", name, "--config", configPath)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}
	defer s.Close()

	// Configure recovery actions - restart on failure
	// First failure: restart after 5 seconds
	// Second failure: restart after 10 seconds
	// Subsequent failures: restart after 30 seconds
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	err = s.SetRecoveryActions(recoveryActions, 86400) // Reset failure count after 24 hours
	if err != nil {
		log.Printf("Warning: failed to set recovery actions: %v", err)
		// Non-fatal - continue without recovery configuration
	}

	// Setup event logging
	err = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return fmt.Errorf("failed to setup event log: %w", err)
	}

	fmt.Printf("Service '%s' installed successfully\n", name)
	fmt.Printf("Start with: sc start %s\n", name)
	return nil
}

// Uninstall removes the service with the given name
func Uninstall(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", name, err)
	}
	defer s.Close()

	err = s.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	err = eventlog.Remove(name)
	if err != nil {
		log.Printf("Warning: failed to remove event log: %v", err)
	}

	fmt.Printf("Service '%s' uninstalled successfully\n", name)
	return nil
}

// Start starts the Windows service with the given name
func Start(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", name, err)
	}
	defer s.Close()

	err = s.Start()
	if err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	fmt.Printf("Service '%s' started successfully\n", name)
	return nil
}

// Stop stops the Windows service with the given name
func Stop(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", name, err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	// Wait for the service to stop
	timeout := time.Now().Add(30 * time.Second)
	for status.State != svc.Stopped {
		if time.Now().After(timeout) {
			return fmt.Errorf("timeout waiting for service to stop")
		}
		time.Sleep(500 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return fmt.Errorf("failed to query service status: %w", err)
		}
	}

	fmt.Printf("Service '%s' stopped successfully\n", name)
	return nil
}

// Restart restarts the Windows service with the given name
func Restart(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	if err := Stop(name); err != nil {
		// If stop fails because service isn't running, that's OK
		log.Printf("Note: %v", err)
	}
	return Start(name)
}

// Status returns the current service status for the given name
func Status(name string) (string, error) {
	if name == "" {
		name = defaultServiceName
	}

	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return "not installed", nil
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return "", fmt.Errorf("failed to query service status: %w", err)
	}

	switch status.State {
	case svc.Stopped:
		return "stopped", nil
	case svc.StartPending:
		return "starting", nil
	case svc.StopPending:
		return "stopping", nil
	case svc.Running:
		return "running", nil
	case svc.ContinuePending:
		return "resuming", nil
	case svc.PausePending:
		return "pausing", nil
	case svc.Paused:
		return "paused", nil
	default:
		return "unknown", nil
	}
}

// joinErrors joins error messages with newline and indentation
func joinErrors(errors []string) string {
	return strings.Join(errors, "\n  ")
}
