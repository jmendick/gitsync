package main

import (
	"flag"
	"log"
	"os"

	"github.com/jmendick/gitsync/internal/cli"
	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/p2p"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override config path if specified via flag
	if *configPath != "config.yaml" {
		cfg, err = config.LoadConfigFromFile(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config from %s: %v", *configPath, err)
		}
	}

	// Ensure repository directory exists
	if err := os.MkdirAll(cfg.RepositoryDir, 0755); err != nil {
		log.Fatalf("Failed to create repository directory: %v", err)
	}

	// Initialize data directory for users
	dataDir := cfg.GetRepositoryDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Initialize server
	server, err := p2p.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	// Start HTTP server in a goroutine
	go func() {
		addr := cfg.GetListenAddress()
		if addr == "" {
			addr = ":8080"
		}
		if err := server.ListenAndServe(addr); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Run CLI
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
