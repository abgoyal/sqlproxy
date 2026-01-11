//go:build !windows

package service

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/template"
	"time"

	"sql-proxy/internal/config"
	"sql-proxy/internal/server"
	"sql-proxy/internal/validate"
)

//go:embed templates/systemd.unit.tmpl
var systemdUnitTemplate string

//go:embed templates/launchd.plist.tmpl
var launchdPlistTemplate string

// serviceTemplateData holds data for service file templates
type serviceTemplateData struct {
	Name       string
	ExePath    string
	ConfigPath string
	Label      string // For launchd
}

const shutdownTimeout = 30 * time.Second
const defaultServiceName = "sql-proxy"

// Run starts the server.
// If interactive is true, runs in foreground with signal handling and output.
// If interactive is false (daemon mode), runs quietly for systemd/launchd.
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
	case sig := <-sigChan:
		if interactive {
			log.Printf("Received %v, shutting down...", sig)
		}
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// Install outputs the service file and instructions for the current platform
func Install(name, exePath, configPath string) error {
	if name == "" {
		name = defaultServiceName
	}

	// Get absolute paths
	absExePath, err := filepath.Abs(exePath)
	if err != nil {
		absExePath = exePath
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		absConfigPath = configPath
	}

	switch runtime.GOOS {
	case "linux":
		return installLinux(name, absExePath, absConfigPath)
	case "darwin":
		return installDarwin(name, absExePath, absConfigPath)
	default:
		fmt.Printf("Service installation is not supported on %s.\n", runtime.GOOS)
		fmt.Println("Run the binary directly with --daemon flag for background operation.")
		return nil
	}
}

func installLinux(name, exePath, configPath string) error {
	// Generate systemd unit file from template
	tmpl, err := template.New("systemd").Parse(systemdUnitTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse systemd template: %w", err)
	}

	data := serviceTemplateData{
		Name:       name,
		ExePath:    exePath,
		ConfigPath: configPath,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute systemd template: %w", err)
	}
	unitFile := buf.String()

	unitPath := fmt.Sprintf("/etc/systemd/system/%s.service", name)

	fmt.Println("=== Systemd Service Installation ===")
	fmt.Println()
	fmt.Println("1. Create the service file:")
	fmt.Printf("   sudo tee %s << 'EOF'\n", unitPath)
	fmt.Println(unitFile + "EOF")
	fmt.Println()
	fmt.Println("2. Reload systemd and enable the service:")
	fmt.Printf("   sudo systemctl daemon-reload\n")
	fmt.Printf("   sudo systemctl enable %s\n", name)
	fmt.Printf("   sudo systemctl start %s\n", name)
	fmt.Println()
	fmt.Println("3. Check status:")
	fmt.Printf("   sudo systemctl status %s\n", name)
	fmt.Printf("   sudo journalctl -u %s -f\n", name)
	fmt.Println()

	return nil
}

func installDarwin(name, exePath, configPath string) error {
	// Generate launchd plist file from template
	tmpl, err := template.New("launchd").Parse(launchdPlistTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse launchd template: %w", err)
	}

	plistLabel := fmt.Sprintf("com.sqlproxy.%s", name)
	data := serviceTemplateData{
		Name:       name,
		ExePath:    exePath,
		ConfigPath: configPath,
		Label:      plistLabel,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute launchd template: %w", err)
	}
	plistFile := buf.String()

	plistPath := fmt.Sprintf("/Library/LaunchDaemons/%s.plist", plistLabel)

	fmt.Println("=== launchd Service Installation ===")
	fmt.Println()
	fmt.Println("1. Create the plist file:")
	fmt.Printf("   sudo tee %s << 'EOF'\n", plistPath)
	fmt.Println(plistFile + "EOF")
	fmt.Println()
	fmt.Println("2. Set permissions and load the service:")
	fmt.Printf("   sudo chown root:wheel %s\n", plistPath)
	fmt.Printf("   sudo chmod 644 %s\n", plistPath)
	fmt.Printf("   sudo launchctl load %s\n", plistPath)
	fmt.Println()
	fmt.Println("3. Check status:")
	fmt.Printf("   sudo launchctl list | grep %s\n", name)
	fmt.Printf("   tail -f /var/log/%s.log\n", name)
	fmt.Println()

	return nil
}

// Uninstall prints instructions for the current platform
func Uninstall(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	switch runtime.GOOS {
	case "linux":
		fmt.Println("To uninstall the systemd service:")
		fmt.Println()
		fmt.Printf("   sudo systemctl stop %s\n", name)
		fmt.Printf("   sudo systemctl disable %s\n", name)
		fmt.Printf("   sudo rm /etc/systemd/system/%s.service\n", name)
		fmt.Println("   sudo systemctl daemon-reload")
	case "darwin":
		plistLabel := fmt.Sprintf("com.sqlproxy.%s", name)
		plistPath := fmt.Sprintf("/Library/LaunchDaemons/%s.plist", plistLabel)
		fmt.Println("To uninstall the launchd service:")
		fmt.Println()
		fmt.Printf("   sudo launchctl unload %s\n", plistPath)
		fmt.Printf("   sudo rm %s\n", plistPath)
	default:
		fmt.Printf("Service uninstallation is not supported on %s.\n", runtime.GOOS)
	}
	return nil
}

// Start prints instructions for the current platform
func Start(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	switch runtime.GOOS {
	case "linux":
		fmt.Println("To start the service:")
		fmt.Printf("   sudo systemctl start %s\n", name)
	case "darwin":
		plistLabel := fmt.Sprintf("com.sqlproxy.%s", name)
		fmt.Println("To start the service:")
		fmt.Printf("   sudo launchctl start %s\n", plistLabel)
	default:
		fmt.Println("Run the binary directly with --daemon flag to start.")
	}
	return nil
}

// Stop prints instructions for the current platform
func Stop(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	switch runtime.GOOS {
	case "linux":
		fmt.Println("To stop the service:")
		fmt.Printf("   sudo systemctl stop %s\n", name)
	case "darwin":
		plistLabel := fmt.Sprintf("com.sqlproxy.%s", name)
		fmt.Println("To stop the service:")
		fmt.Printf("   sudo launchctl stop %s\n", plistLabel)
	default:
		fmt.Println("Send SIGTERM to the process to stop it gracefully.")
	}
	return nil
}

// Restart prints instructions for the current platform
func Restart(name string) error {
	if name == "" {
		name = defaultServiceName
	}

	switch runtime.GOOS {
	case "linux":
		fmt.Println("To restart the service:")
		fmt.Printf("   sudo systemctl restart %s\n", name)
	case "darwin":
		plistLabel := fmt.Sprintf("com.sqlproxy.%s", name)
		fmt.Println("To restart the service:")
		fmt.Printf("   sudo launchctl stop %s\n", plistLabel)
		fmt.Printf("   sudo launchctl start %s\n", plistLabel)
	default:
		fmt.Println("Stop and start the process manually.")
	}
	return nil
}

// Status prints instructions for the current platform
func Status(name string) (string, error) {
	if name == "" {
		name = defaultServiceName
	}

	switch runtime.GOOS {
	case "linux":
		fmt.Println("To check service status:")
		fmt.Printf("   sudo systemctl status %s\n", name)
	case "darwin":
		plistLabel := fmt.Sprintf("com.sqlproxy.%s", name)
		fmt.Println("To check service status:")
		fmt.Printf("   sudo launchctl list | grep %s\n", plistLabel)
	default:
		fmt.Println("Check if the process is running using ps or your platform's tools.")
	}
	return "use platform tools", nil
}

// joinErrors joins error messages with newline and indentation
func joinErrors(errors []string) string {
	return strings.Join(errors, "\n  ")
}
