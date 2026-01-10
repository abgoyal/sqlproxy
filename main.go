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
	install      = flag.Bool("install", false, "Install as system service")
	uninstall    = flag.Bool("uninstall", false, "Uninstall system service")
	start        = flag.Bool("start", false, "Start the system service")
	stop         = flag.Bool("stop", false, "Stop the system service")
	restart      = flag.Bool("restart", false, "Restart the system service")
	status       = flag.Bool("status", false, "Show system service status")
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

	if *start {
		if err := service.Start(); err != nil {
			log.Fatalf("Failed to start service: %v", err)
		}
		return
	}

	if *stop {
		if err := service.Stop(); err != nil {
			log.Fatalf("Failed to stop service: %v", err)
		}
		return
	}

	if *restart {
		if err := service.Restart(); err != nil {
			log.Fatalf("Failed to restart service: %v", err)
		}
		return
	}

	if *status {
		st, err := service.Status()
		if err != nil {
			log.Fatalf("Failed to get service status: %v", err)
		}
		fmt.Printf("Service %s: %s\n", service.ServiceName(), st)
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
