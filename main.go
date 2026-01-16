package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"sql-proxy/internal/config"
	"sql-proxy/internal/service"
	"sql-proxy/internal/validate"
)

// Version is set at build time via ldflags
// Example: go build -ldflags "-X main.Version=1.0.0 -X main.BuildTime=2024-01-15T10:30:00Z"
var (
	Version   = "dev"
	BuildTime = "unknown"
)

var (
	configPath   = flag.String("config", "config.yaml", "Path to configuration file")
	serviceName  = flag.String("service-name", "sql-proxy", "Service name (for multi-instance support)")
	daemon       = flag.Bool("daemon", false, "Run as background daemon/service (disables interactive output)")
	install      = flag.Bool("install", false, "Install as system service")
	uninstall    = flag.Bool("uninstall", false, "Uninstall system service")
	start        = flag.Bool("start", false, "Start the system service")
	stop         = flag.Bool("stop", false, "Stop the system service")
	restart      = flag.Bool("restart", false, "Restart the system service")
	status       = flag.Bool("status", false, "Show system service status")
	validateOnly = flag.Bool("validate", false, "Validate configuration and exit")
	showVersion  = flag.Bool("version", false, "Print version and exit")
)

func main() {
	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("sql-proxy version %s (built %s)\n", Version, BuildTime)
		return
	}

	// Handle service install/uninstall
	if *install {
		fmt.Printf("SQL Proxy Service %s\n", Version)
		exePath, err := os.Executable()
		if err != nil {
			log.Fatalf("Failed to get executable path: %v", err)
		}

		absConfigPath, err := filepath.Abs(*configPath)
		if err != nil {
			log.Fatalf("Failed to get absolute config path: %v", err)
		}

		if err := service.Install(*serviceName, exePath, absConfigPath); err != nil {
			log.Fatalf("Failed to install service: %v", err)
		}
		return
	}

	if *uninstall {
		fmt.Printf("SQL Proxy Service %s\n", Version)
		if err := service.Uninstall(*serviceName); err != nil {
			log.Fatalf("Failed to uninstall service: %v", err)
		}
		return
	}

	if *start {
		if err := service.Start(*serviceName); err != nil {
			log.Fatalf("Failed to start service: %v", err)
		}
		return
	}

	if *stop {
		if err := service.Stop(*serviceName); err != nil {
			log.Fatalf("Failed to stop service: %v", err)
		}
		return
	}

	if *restart {
		if err := service.Restart(*serviceName); err != nil {
			log.Fatalf("Failed to restart service: %v", err)
		}
		return
	}

	if *status {
		st, err := service.Status(*serviceName)
		if err != nil {
			log.Fatalf("Failed to get service status: %v", err)
		}
		fmt.Printf("Service %s: %s\n", *serviceName, st)
		return
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set runtime info (not from config file)
	cfg.Server.Version = Version
	cfg.Server.BuildTime = BuildTime

	// Handle validation mode
	if *validateOnly {
		result := validate.Run(cfg)
		printValidationResult(cfg, result)
		if result.Valid {
			os.Exit(0)
		}
		os.Exit(1)
	}

	// Interactive mode shows startup info
	interactive := !*daemon
	if interactive {
		fmt.Printf("SQL Proxy Service %s\n", Version)
		fmt.Printf("Loaded %d workflows\n", len(cfg.Workflows))
	}

	// Set service name before running (needed for Windows service mode)
	service.SetServiceName(*serviceName)

	// Run the service
	if err := service.Run(cfg, interactive); err != nil {
		log.Fatalf("Service error: %v", err)
	}
}

func printValidationResult(cfg *config.Config, result *validate.Result) {
	fmt.Println("SQL Proxy Configuration Validator")
	fmt.Println("==================================")
	fmt.Printf("Config file: %s\n\n", *configPath)

	fmt.Printf("Server: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
	fmt.Printf("Databases: %d configured\n", len(cfg.Databases))
	for _, db := range cfg.Databases {
		mode := "read-only"
		if !db.IsReadOnly() {
			mode = "read-write"
		}
		dbType := db.Type
		if dbType == "" {
			dbType = "sqlserver"
		}
		if dbType == "sqlite" {
			fmt.Printf("  - %s: sqlite:%s (%s)\n", db.Name, db.Path, mode)
		} else {
			fmt.Printf("  - %s: %s@%s/%s (%s)\n", db.Name, db.User, db.Host, db.Database, mode)
		}
	}
	fmt.Printf("Workflows: %d configured\n", len(cfg.Workflows))

	if len(cfg.Workflows) > 0 {
		fmt.Println("\nWorkflows:")
		for _, wf := range cfg.Workflows {
			for _, trigger := range wf.Triggers {
				if trigger.Type == "http" && trigger.Path != "" {
					fmt.Printf("  %s %s - %s (%d params)\n", trigger.Method, trigger.Path, wf.Name, len(trigger.Parameters))
				} else if trigger.Type == "cron" {
					fmt.Printf("  [cron] %s - %s\n", trigger.Schedule, wf.Name)
				}
			}
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  [WARN] %s\n", w)
		}
	}

	if len(result.Errors) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range result.Errors {
			fmt.Printf("  [ERROR] %s\n", e)
		}
	}

	fmt.Println()
	if result.Valid {
		fmt.Println("Configuration valid")
	} else {
		fmt.Println("Configuration invalid")
	}
}
