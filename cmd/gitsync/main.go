package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/p2p"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
	"github.com/jmendick/gitsync/internal/sync"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Create a context that will be canceled on interrupt
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Println("\nShutdown signal received...")
		cancel()
	}()

	// Initialize components
	components, err := initializeComponents()
	if err != nil {
		return fmt.Errorf("failed to initialize components: %w", err)
	}
	defer cleanup(components)

	fmt.Println("Go-GitSync is running. Press Ctrl+C to stop.")
	<-ctx.Done()
	fmt.Println("Go-GitSync stopped.")
	return nil
}

type components struct {
	node        *p2p.Node
	syncManager *sync.SyncManager
}

func initializeComponents() (*components, error) {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("error loading configuration: %w", err)
	}
	fmt.Printf("Configuration loaded: %+v\n", cfg)

	// Initialize Git manager
	gitManager, err := git.NewGitRepositoryManager(cfg.GetRepositoryDir())
	if err != nil {
		return nil, fmt.Errorf("error initializing git manager: %w", err)
	}

	// Initialize protocol handler
	protocolHandler := protocol.NewProtocolHandler(gitManager)

	// Initialize P2P Node
	node, err := p2p.NewNode(cfg, protocolHandler)
	if err != nil {
		return nil, fmt.Errorf("error initializing P2P node: %w", err)
	}

	// Initialize sync manager
	syncManager := sync.NewSyncManager(cfg, node)
	if err := syncManager.Start(); err != nil {
		node.Close() // Clean up the node if sync manager fails to start
		return nil, fmt.Errorf("error starting sync manager: %w", err)
	}

	return &components{
		node:        node,
		syncManager: syncManager,
	}, nil
}

func cleanup(c *components) {
	if c == nil {
		return
	}

	// Order matters: stop sync manager first, then close p2p node
	if c.syncManager != nil {
		if err := c.syncManager.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error stopping sync manager: %v\n", err)
		}
	}

	if c.node != nil {
		if err := c.node.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing P2P node: %v\n", err)
		}
	}
}
