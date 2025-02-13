package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the application's configuration.
type Config struct {
	ListenAddress  string   `yaml:"listen_address"`
	BootstrapPeers []string `yaml:"bootstrap_peers"`
	RepositoryDir  string   `yaml:"repository_dir"`

	// Security settings
	Security SecurityConfig `yaml:"security"`

	// Network topology preferences
	Network NetworkConfig `yaml:"network"`

	// Sync strategy configuration
	Sync SyncConfig `yaml:"sync"`

	// Merge preferences
	Merge MergeConfig `yaml:"merge"`

	// Discovery persistence
	Discovery DiscoveryConfig `yaml:"discovery"`

	// Authentication configuration
	Auth AuthConfig `yaml:"auth"`

	// Helper methods
	configDir string
}

// SecurityConfig holds security-related settings
type SecurityConfig struct {
	EnableEncryption bool     `yaml:"enable_encryption"`
	EncryptionKey    string   `yaml:"encryption_key,omitempty"`
	TLSCertFile      string   `yaml:"tls_cert_file,omitempty"`
	TLSKeyFile       string   `yaml:"tls_key_file,omitempty"`
	TrustedPeers     []string `yaml:"trusted_peers,omitempty"`
	AuthMode         string   `yaml:"auth_mode"` // none, tls, shared_key
}

// NetworkConfig defines network topology preferences
type NetworkConfig struct {
	MaxPeers           int           `yaml:"max_peers"`
	MinPeers           int           `yaml:"min_peers"`
	PeerTimeout        time.Duration `yaml:"peer_timeout"`
	PreferredZones     []string      `yaml:"preferred_zones,omitempty"`
	NetworkMode        string        `yaml:"network_mode"`        // mesh, star, hierarchical
	ConnectionStrategy string        `yaml:"connection_strategy"` // aggressive, conservative
	BandwidthLimit     int64         `yaml:"bandwidth_limit"`     // bytes per second, 0 for unlimited
}

// SyncConfig defines synchronization strategies
type SyncConfig struct {
	SyncInterval     time.Duration `yaml:"sync_interval"`
	MaxSyncAttempts  int           `yaml:"max_sync_attempts"`
	SyncMode         string        `yaml:"sync_mode"`         // full, incremental, selective
	ConflictStrategy string        `yaml:"conflict_strategy"` // manual, auto-resolve, prefer-local, prefer-remote
	AutoSyncEnabled  bool          `yaml:"auto_sync_enabled"`
	ExcludePatterns  []string      `yaml:"exclude_patterns,omitempty"`
	IncludePatterns  []string      `yaml:"include_patterns,omitempty"`
	BatchSize        int           `yaml:"batch_size"` // number of files to sync in one batch
}

// MergeConfig defines merge preferences
type MergeConfig struct {
	DefaultStrategy  string   `yaml:"default_strategy"` // ours, theirs, union, manual
	AutoResolve      bool     `yaml:"auto_resolve"`
	IgnoreWhitespace bool     `yaml:"ignore_whitespace"`
	PreferUpstream   bool     `yaml:"prefer_upstream"`
	CustomMergeTools []string `yaml:"custom_merge_tools,omitempty"`
}

// DiscoveryConfig defines peer discovery persistence settings
type DiscoveryConfig struct {
	PersistenceEnabled bool          `yaml:"persistence_enabled"`
	StorageDir         string        `yaml:"storage_dir"`
	PeerCacheTime      time.Duration `yaml:"peer_cache_time"`
	MaxStoredPeers     int           `yaml:"max_stored_peers"`
}

// AuthConfig holds authentication-related settings
type AuthConfig struct {
	Enabled       bool              `yaml:"enabled"`
	TokenSecret   string            `yaml:"token_secret"`
	TokenExpiry   string            `yaml:"token_expiry"`
	AllowedRoles  []Role            `yaml:"allowed_roles"`
	MinPassLength int               `yaml:"min_password_length"`
	GitHubOAuth   GitHubOAuthConfig `yaml:"github_oauth"`
}

type GitHubOAuthConfig struct {
	Enabled      bool   `yaml:"enabled"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	CallbackURL  string `yaml:"callback_url"`
}

// GetAuthURL returns the GitHub OAuth authorization URL
func (c *GitHubOAuthConfig) GetAuthURL() string {
	return fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=repo,read:org",
		c.ClientID,
		url.QueryEscape(c.CallbackURL),
	)
}

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleGuest  Role = "guest"
)

// LoadConfig loads the configuration from command-line flags and/or config file.
func LoadConfig() (*Config, error) {
	var configFile string
	cfg := &Config{
		ListenAddress:  ":8080",           // Default listen address
		RepositoryDir:  "./gitsync-repos", // Default repository directory
		BootstrapPeers: []string{},        // Default bootstrap peers (empty)

		// Default security settings
		Security: SecurityConfig{
			EnableEncryption: false,
			AuthMode:         "none",
		},

		// Default network settings
		Network: NetworkConfig{
			MaxPeers:           50,
			MinPeers:           5,
			PeerTimeout:        5 * time.Minute,
			NetworkMode:        "mesh",
			ConnectionStrategy: "conservative",
		},

		// Default sync settings
		Sync: SyncConfig{
			SyncInterval:     5 * time.Minute,
			MaxSyncAttempts:  3,
			SyncMode:         "incremental",
			ConflictStrategy: "manual",
			AutoSyncEnabled:  true,
			BatchSize:        100,
		},

		// Default merge settings
		Merge: MergeConfig{
			DefaultStrategy:  "manual",
			AutoResolve:      false,
			IgnoreWhitespace: true,
			PreferUpstream:   false,
		},

		// Default discovery settings
		Discovery: DiscoveryConfig{
			PersistenceEnabled: true,
			StorageDir:         "./peer-cache",
			PeerCacheTime:      24 * time.Hour,
			MaxStoredPeers:     1000,
		},

		// Default authentication settings
		Auth: AuthConfig{
			Enabled:       false,
			TokenSecret:   "",
			TokenExpiry:   "24h",
			AllowedRoles:  []Role{RoleAdmin, RoleMember, RoleGuest},
			MinPassLength: 8,
		},
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
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Ensure configuration persistence
	if err := cfg.save(); err != nil {
		return nil, fmt.Errorf("failed to save configuration: %w", err)
	}

	return cfg, nil
}

// LoadConfigFromFile loads configuration from a specific file path
func LoadConfigFromFile(configPath string) (*Config, error) {
	cfg := &Config{
		ListenAddress:  ":8080",           // Default listen address
		RepositoryDir:  "./gitsync-repos", // Default repository directory
		BootstrapPeers: []string{},        // Default bootstrap peers (empty)

		// Default security settings
		Security: SecurityConfig{
			EnableEncryption: false,
			AuthMode:         "none",
		},

		// Default network settings
		Network: NetworkConfig{
			MaxPeers:           50,
			MinPeers:           5,
			PeerTimeout:        5 * time.Minute,
			NetworkMode:        "mesh",
			ConnectionStrategy: "conservative",
		},

		// Default sync settings
		Sync: SyncConfig{
			SyncInterval:     5 * time.Minute,
			MaxSyncAttempts:  3,
			SyncMode:         "incremental",
			ConflictStrategy: "manual",
			AutoSyncEnabled:  true,
			BatchSize:        100,
		},

		// Default merge settings
		Merge: MergeConfig{
			DefaultStrategy:  "manual",
			AutoResolve:      false,
			IgnoreWhitespace: true,
			PreferUpstream:   false,
		},

		// Default discovery settings
		Discovery: DiscoveryConfig{
			PersistenceEnabled: true,
			StorageDir:         "./peer-cache",
			PeerCacheTime:      24 * time.Hour,
			MaxStoredPeers:     1000,
		},

		// Default authentication settings
		Auth: AuthConfig{
			Enabled:       false,
			TokenSecret:   "",
			TokenExpiry:   "24h",
			AllowedRoles:  []Role{RoleAdmin, RoleMember, RoleGuest},
			MinPassLength: 8,
		},
	}

	if err := loadYAMLConfig(configPath, cfg); err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", configPath, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	if err := cfg.save(); err != nil {
		return nil, fmt.Errorf("failed to save configuration: %w", err)
	}

	return cfg, nil
}

// validate performs configuration validation
func (c *Config) validate() error {
	if c.RepositoryDir == "" {
		return fmt.Errorf("repository directory cannot be empty")
	}

	if c.Network.MaxPeers < c.Network.MinPeers {
		return fmt.Errorf("max_peers cannot be less than min_peers")
	}

	if c.Sync.BatchSize < 1 {
		return fmt.Errorf("batch_size must be greater than 0")
	}

	return nil
}

// save persists the configuration to disk
func (c *Config) save() error {
	configDir := filepath.Join(c.RepositoryDir, ".gitsync")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
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

// Export configuration getters
func (c *Config) GetRepositoryDir() string {
	return c.RepositoryDir
}

func (c *Config) GetListenAddress() string {
	return c.ListenAddress
}

func (c *Config) GetBootstrapPeers() []string {
	return c.BootstrapPeers
}

// GetSecurity returns the security configuration
func (c *Config) GetSecurity() SecurityConfig {
	return c.Security
}

// GetNetwork returns the network configuration
func (c *Config) GetNetwork() NetworkConfig {
	return c.Network
}

// GetSync returns the sync configuration
func (c *Config) GetSync() SyncConfig {
	return c.Sync
}

// GetMerge returns the merge configuration
func (c *Config) GetMerge() MergeConfig {
	return c.Merge
}

// GetDiscovery returns the discovery configuration
func (c *Config) GetDiscovery() DiscoveryConfig {
	return c.Discovery
}

// GetAuth returns the authentication configuration
func (c *Config) GetAuth() AuthConfig {
	return c.Auth
}

// SetSecurityConfig updates security configuration settings
func (c *Config) SetSecurityConfig(key, value string) error {
	switch key {
	case "enable_encryption":
		c.Security.EnableEncryption = value == "true"
	case "auth_mode":
		c.Security.AuthMode = value
	default:
		return fmt.Errorf("unknown security setting: %s", key)
	}
	return c.save()
}

// SetNetworkConfig updates network configuration settings
func (c *Config) SetNetworkConfig(key, value string) error {
	switch key {
	case "max_peers":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid value for max_peers: %s", value)
		}
		c.Network.MaxPeers = v
	case "network_mode":
		c.Network.NetworkMode = value
	default:
		return fmt.Errorf("unknown network setting: %s", key)
	}
	return c.save()
}

// SetSyncConfig updates sync configuration settings
func (c *Config) SetSyncConfig(key, value string) error {
	switch key {
	case "auto_sync_enabled":
		c.Sync.AutoSyncEnabled = value == "true"
	case "sync_mode":
		c.Sync.SyncMode = value
	case "conflict_strategy":
		c.Sync.ConflictStrategy = value
	default:
		return fmt.Errorf("unknown sync setting: %s", key)
	}
	return c.save()
}

// SetMergeConfig updates merge configuration settings
func (c *Config) SetMergeConfig(key, value string) error {
	switch key {
	case "default_strategy":
		c.Merge.DefaultStrategy = value
	case "auto_resolve":
		c.Merge.AutoResolve = value == "true"
	default:
		return fmt.Errorf("unknown merge setting: %s", key)
	}
	return c.save()
}

// SetDiscoveryConfig updates discovery configuration settings
func (c *Config) SetDiscoveryConfig(key, value string) error {
	switch key {
	case "persistence_enabled":
		c.Discovery.PersistenceEnabled = value == "true"
	case "max_stored_peers":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid value for max_stored_peers: %s", value)
		}
		c.Discovery.MaxStoredPeers = v
	default:
		return fmt.Errorf("unknown discovery setting: %s", key)
	}
	return c.save()
}

// SetAuthConfig updates authentication configuration settings
func (c *Config) SetAuthConfig(key, value string) error {
	switch key {
	case "enabled":
		c.Auth.Enabled = value == "true"
	case "token_secret":
		c.Auth.TokenSecret = value
	case "token_expiry":
		c.Auth.TokenExpiry = value
	case "min_password_length":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid value for min_password_length: %s", value)
		}
		c.Auth.MinPassLength = v
	default:
		return fmt.Errorf("unknown auth setting: %s", key)
	}
	return c.save()
}

// GetConfigDir returns the configuration directory
func (c *Config) GetConfigDir() string {
	if c.configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".", ".gitsync")
		}
		c.configDir = filepath.Join(homeDir, ".gitsync")
	}
	return c.configDir
}

// GetConfigDir returns the directory where configuration files are stored
func GetConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".gitsync")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	return configDir, nil
}
