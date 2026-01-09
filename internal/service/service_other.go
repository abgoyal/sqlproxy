//go:build !windows

package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"sql-proxy/internal/config"
	"sql-proxy/internal/server"
)

const serviceName = "sql-proxy"

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

// Install prints instructions for the current platform
func Install(exePath, configPath string) error {
	switch runtime.GOOS {
	case "linux":
		fmt.Println("To install as a systemd service on Linux:")
		fmt.Println()
		fmt.Println("1. Copy the example unit file:")
		fmt.Println("   sudo cp sql-proxy.service /etc/systemd/system/")
		fmt.Println()
		fmt.Println("2. Edit the unit file to set correct paths:")
		fmt.Println("   sudo systemctl edit --full sql-proxy")
		fmt.Println()
		fmt.Println("3. Reload systemd and enable the service:")
		fmt.Println("   sudo systemctl daemon-reload")
		fmt.Println("   sudo systemctl enable sql-proxy")
		fmt.Println("   sudo systemctl start sql-proxy")
	case "darwin":
		fmt.Println("To install as a launchd service on macOS:")
		fmt.Println()
		fmt.Println("1. Copy the example plist file:")
		fmt.Println("   sudo cp com.sqlproxy.plist /Library/LaunchDaemons/")
		fmt.Println()
		fmt.Println("2. Edit the plist file to set correct paths:")
		fmt.Println("   sudo nano /Library/LaunchDaemons/com.sqlproxy.plist")
		fmt.Println()
		fmt.Println("3. Load and start the service:")
		fmt.Println("   sudo launchctl load /Library/LaunchDaemons/com.sqlproxy.plist")
	default:
		fmt.Printf("Service installation is not supported on %s.\n", runtime.GOOS)
		fmt.Println("Run the binary directly or use your platform's init system.")
	}
	return nil
}

// Uninstall prints instructions for the current platform
func Uninstall() error {
	switch runtime.GOOS {
	case "linux":
		fmt.Println("To uninstall the systemd service:")
		fmt.Println()
		fmt.Println("   sudo systemctl stop sql-proxy")
		fmt.Println("   sudo systemctl disable sql-proxy")
		fmt.Println("   sudo rm /etc/systemd/system/sql-proxy.service")
		fmt.Println("   sudo systemctl daemon-reload")
	case "darwin":
		fmt.Println("To uninstall the launchd service:")
		fmt.Println()
		fmt.Println("   sudo launchctl unload /Library/LaunchDaemons/com.sqlproxy.plist")
		fmt.Println("   sudo rm /Library/LaunchDaemons/com.sqlproxy.plist")
	default:
		fmt.Printf("Service uninstallation is not supported on %s.\n", runtime.GOOS)
	}
	return nil
}

// Start prints instructions for the current platform
func Start() error {
	switch runtime.GOOS {
	case "linux":
		fmt.Println("To start the service:")
		fmt.Println("   sudo systemctl start sql-proxy")
	case "darwin":
		fmt.Println("To start the service:")
		fmt.Println("   sudo launchctl start com.sqlproxy")
	default:
		fmt.Println("Run the binary directly to start the service.")
	}
	return nil
}

// Stop prints instructions for the current platform
func Stop() error {
	switch runtime.GOOS {
	case "linux":
		fmt.Println("To stop the service:")
		fmt.Println("   sudo systemctl stop sql-proxy")
	case "darwin":
		fmt.Println("To stop the service:")
		fmt.Println("   sudo launchctl stop com.sqlproxy")
	default:
		fmt.Println("Send SIGTERM to the process to stop it gracefully.")
	}
	return nil
}

// Restart prints instructions for the current platform
func Restart() error {
	switch runtime.GOOS {
	case "linux":
		fmt.Println("To restart the service:")
		fmt.Println("   sudo systemctl restart sql-proxy")
	case "darwin":
		fmt.Println("To restart the service:")
		fmt.Println("   sudo launchctl stop com.sqlproxy")
		fmt.Println("   sudo launchctl start com.sqlproxy")
	default:
		fmt.Println("Stop and start the process manually.")
	}
	return nil
}

// Status prints instructions for the current platform
func Status() (string, error) {
	switch runtime.GOOS {
	case "linux":
		fmt.Println("To check service status:")
		fmt.Println("   sudo systemctl status sql-proxy")
	case "darwin":
		fmt.Println("To check service status:")
		fmt.Println("   sudo launchctl list | grep sqlproxy")
	default:
		fmt.Println("Check if the process is running using ps or your platform's tools.")
	}
	return "use platform tools", nil
}

// IsWindowsService always returns false on non-Windows
func IsWindowsService() bool {
	return false
}

// ServiceName returns the service name
func ServiceName() string {
	return serviceName
}
