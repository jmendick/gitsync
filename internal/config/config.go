package config

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application's configuration.
type Config struct {
	ListenAddress  string   `yaml:"listen_address"`
	BootstrapPeers []string `yaml:"bootstrap_peers"`
	RepositoryDir  string   `yaml:"repository_dir"`
	// ... other configuration options ...
}

// LoadConfig loads the configuration from command-line flags and/or config file.
func LoadConfig() (*Config, error) {
	var configFile string
	cfg := &Config{
		ListenAddress:  ":8080",           // Default listen address
		RepositoryDir:  "./gitsync-repos", // Default repository directory
		BootstrapPeers: []string{},        // Default bootstrap peers (empty)
	}

	// Define command-line flags
	flag.StringVar(&configFile, "config", "", "Path to YAML config file")
	flag.StringVar(&cfg.ListenAddress, "listen", cfg.ListenAddress, "Listen address for P2P communication")
	flag.StringVar(&cfg.RepositoryDir, "repo-dir", cfg.RepositoryDir, "Directory to store Git repositories")

	// Parse command-line flags
	flag.Parse()

	// Load from config file if provided
	if configFile != "" {
		if err := loadYAMLConfig(configFile, cfg); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	}

	// Basic validation
	if cfg.RepositoryDir == "" {
		return nil, fmt.Errorf("repository directory cannot be empty")
	}

	return cfg, nil
}

// loadYAMLConfig loads configuration from a YAML file into the provided Config struct
func loadYAMLConfig(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}

	return nil
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
