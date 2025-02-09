package config

import (
	"flag"
	"fmt"
	"os"
)

// Config represents the application's configuration.
type Config struct {
	ListenAddress string `yaml:"listen_address"`
	BootstrapPeers []string `yaml:"bootstrap_peers"`
	RepositoryDir string `yaml:"repository_dir"`
	// ... other configuration options ...
}

// LoadConfig loads the configuration from command-line flags and/or config file.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		ListenAddress: ":8080", // Default listen address
		RepositoryDir: "./gitsync-repos", // Default repository directory
		BootstrapPeers: []string{}, // Default bootstrap peers (empty)
	}

	// Define command-line flags
	flag.StringVar(&cfg.ListenAddress, "listen", cfg.ListenAddress, "Listen address for P2P communication")
	flag.StringVar(&cfg.RepositoryDir, "repo-dir", cfg.RepositoryDir, "Directory to store Git repositories")
	// ... other flags ...

	// Parse command-line flags
	flag.Parse()

	// TODO: Load from config file (e.g., YAML) if provided via flag or environment variable

	// Basic validation
	if cfg.RepositoryDir == "" {
		return nil, fmt.Errorf("repository directory cannot be empty")
	}

	return cfg, nil
}

// GetRepositoryDir returns the configured repository directory.
func (c *Config) GetRepositoryDir() string {
	return c.RepositoryDir
}

// GetListenAddress returns the configured listen address.
func (c *Config) GetListenAddress() string {
	return c.ListenAddress
}

// GetBootstrapPeers returns the configured bootstrap peers.
func (c *Config) GetBootstrapPeers() []string {
	return c.BootstrapPeers
}