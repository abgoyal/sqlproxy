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

var (
	configPath   = flag.String("config", "config.yaml", "Path to configuration file")
	install      = flag.Bool("install", false, "Install as Windows service")
	uninstall    = flag.Bool("uninstall", false, "Uninstall Windows service")
	validateOnly = flag.Bool("validate", false, "Validate configuration and exit")
	validateDB   = flag.Bool("validate-db", false, "Validate configuration including database connectivity")
)

func main() {
	flag.Parse()

	// Handle service install/uninstall
	if *install {
		exePath, err := os.Executable()
		if err != nil {
			log.Fatalf("Failed to get executable path: %v", err)
		}

		absConfigPath, err := filepath.Abs(*configPath)
		if err != nil {
			log.Fatalf("Failed to get absolute config path: %v", err)
		}

		if err := service.Install(exePath, absConfigPath); err != nil {
			log.Fatalf("Failed to install service: %v", err)
		}
		return
	}

	if *uninstall {
		if err := service.Uninstall(); err != nil {
			log.Fatalf("Failed to uninstall service: %v", err)
		}
		return
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Handle validation modes
	if *validateOnly || *validateDB {
		runValidation(cfg, *validateDB)
		return
	}

	// Configure logging for non-service mode
	if service.IsWindowsService() {
		// When running as a service, log to a file
		logDir := filepath.Dir(*configPath)
		logFile := filepath.Join(logDir, "sql-proxy.log")
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			log.SetOutput(f)
			defer f.Close()
		}
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	fmt.Printf("SQL Proxy Service\n")
	fmt.Printf("Loaded %d query endpoints\n", len(cfg.Queries))

	// Run the service
	if err := service.Run(cfg); err != nil {
		log.Fatalf("Service error: %v", err)
	}
}

func runValidation(cfg *config.Config, testDB bool) {
	fmt.Println("SQL Proxy Configuration Validator")
	fmt.Println("==================================")
	fmt.Printf("Config file: %s\n\n", *configPath)

	var result *validate.Result
	if testDB {
		fmt.Println("Validating configuration and testing database connection...")
		result = validate.ConfigWithDB(cfg)
	} else {
		fmt.Println("Validating configuration (use -validate-db to also test database)...")
		result = validate.Config(cfg)
	}

	// Print summary
	fmt.Printf("\nServer: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
	fmt.Printf("Database: %s@%s/%s\n", cfg.Database.User, cfg.Database.Host, cfg.Database.Database)
	fmt.Printf("Queries: %d endpoints configured\n", len(cfg.Queries))
	fmt.Printf("Logging: level=%s, file=%s\n", cfg.Logging.Level, cfg.Logging.FilePath)
	fmt.Printf("Metrics: enabled=%v, interval=%ds\n", cfg.Metrics.Enabled, cfg.Metrics.IntervalSec)

	// Print queries
	if len(cfg.Queries) > 0 {
		fmt.Println("\nConfigured endpoints:")
		for _, q := range cfg.Queries {
			paramCount := len(q.Parameters)
			fmt.Printf("  %s %s - %s (%d params)\n", q.Method, q.Path, q.Name, paramCount)
		}
	}

	// Print warnings
	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
	}

	// Print errors
	if len(result.Errors) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range result.Errors {
			fmt.Printf("  ✗ %s\n", e)
		}
	}

	// Final result
	fmt.Println()
	if result.Valid {
		fmt.Println("✓ Configuration is valid")
		os.Exit(0)
	} else {
		fmt.Println("✗ Configuration has errors")
		os.Exit(1)
	}
}
