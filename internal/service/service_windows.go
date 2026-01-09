//go:build windows

package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"sql-proxy/internal/config"
	"sql-proxy/internal/server"
)

const serviceName = "SQLProxy"
const serviceDesc = "SQL Proxy Service - HTTP endpoints for SQL Server and SQLite databases"

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

// Run starts the service. If running interactively, it starts the server directly.
func Run(cfg *config.Config) error {
	isWindowsService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("failed to determine if running as service: %w", err)
	}

	interactive := !isWindowsService
	srv, err := server.New(cfg, interactive)
	if err != nil {
		return err
	}

	if isWindowsService {
		// Running as a Windows service
		ws := &windowsService{server: srv}
		elog, err := eventlog.Open(serviceName)
		if err == nil {
			defer elog.Close()
		}
		return svc.Run(serviceName, ws)
	}

	// Running interactively - handle Ctrl+C for graceful shutdown
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
		log.Println("Received interrupt, shutting down...")
		return srv.Shutdown(context.Background())
	}
}

// Install installs the service
func Install(exePath, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", serviceName)
	}

	s, err = m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName: serviceName,
		Description: serviceDesc,
		StartType:   mgr.StartAutomatic,
	}, "-config", configPath)
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
	err = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return fmt.Errorf("failed to setup event log: %w", err)
	}

	log.Printf("Service %s installed successfully", serviceName)
	return nil
}

// Uninstall removes the service
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", serviceName, err)
	}
	defer s.Close()

	err = s.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	err = eventlog.Remove(serviceName)
	if err != nil {
		log.Printf("Warning: failed to remove event log: %v", err)
	}

	log.Printf("Service %s uninstalled successfully", serviceName)
	return nil
}

// Start starts the Windows service
func Start() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", serviceName, err)
	}
	defer s.Close()

	err = s.Start()
	if err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	log.Printf("Service %s started successfully", serviceName)
	return nil
}

// Stop stops the Windows service
func Stop() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", serviceName, err)
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

	log.Printf("Service %s stopped successfully", serviceName)
	return nil
}

// Restart restarts the Windows service
func Restart() error {
	if err := Stop(); err != nil {
		// If stop fails because service isn't running, that's OK
		log.Printf("Note: %v", err)
	}
	return Start()
}

// Status returns the current service status
func Status() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
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

// IsWindowsService returns true if running as a Windows service
func IsWindowsService() bool {
	isService, _ := svc.IsWindowsService()
	return isService
}

// ServiceName returns the service name
func ServiceName() string {
	return serviceName
}
