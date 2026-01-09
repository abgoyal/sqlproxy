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

	// Handle validation mode
	if *validateOnly {
		result := validate.Run(cfg)
		printValidationResult(cfg, result)
		if result.Valid {
			os.Exit(0)
		}
		os.Exit(1)
	}

	fmt.Printf("SQL Proxy Service\n")
	fmt.Printf("Loaded %d query endpoints\n", len(cfg.Queries))

	// Run the service
	if err := service.Run(cfg); err != nil {
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
		fmt.Printf("  - %s: %s@%s/%s (%s)\n", db.Name, db.User, db.Host, db.Database, mode)
	}
	fmt.Printf("Queries: %d configured\n", len(cfg.Queries))

	if len(cfg.Queries) > 0 {
		fmt.Println("\nEndpoints:")
		for _, q := range cfg.Queries {
			if q.Path != "" {
				fmt.Printf("  %s %s - %s [%s] (%d params)\n", q.Method, q.Path, q.Name, q.Database, len(q.Parameters))
			}
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
	}

	if len(result.Errors) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range result.Errors {
			fmt.Printf("  ✗ %s\n", e)
		}
	}

	fmt.Println()
	if result.Valid {
		fmt.Println("✓ Configuration valid")
	} else {
		fmt.Println("✗ Configuration invalid")
	}
}
