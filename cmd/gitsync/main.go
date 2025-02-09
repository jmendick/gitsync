package main

import (
	"fmt"
	"os"

	"github.com/your-username/gitsync/internal/config" // Replace with your project path
	"github.com/your-username/gitsync/internal/p2p"     // Replace with your project path
	"github.com/your-username/gitsync/internal/sync"    // Replace with your project path
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Starting Go-GitSync...")
	fmt.Printf("Configuration loaded: %+v\n", cfg)

	// Initialize P2P Node
	node, err := p2p.NewNode(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing P2P node: %v\n", err)
		os.Exit(1)
	}
	defer node.Close() // Ensure node resources are released

	// Start synchronization logic (placeholder)
	syncManager := sync.NewSyncManager(cfg, node)
	if err := syncManager.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting sync manager: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Go-GitSync is running. Press Ctrl+C to stop.")
	// Keep the application running (e.g., using a channel to wait for shutdown signal)
	<-make(chan os.Signal, 1) // Placeholder - replace with proper signal handling later
	fmt.Println("Go-GitSync stopped.")
}