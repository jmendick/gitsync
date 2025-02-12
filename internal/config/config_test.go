package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &Config{
				RepositoryDir: "./test-repos",
				Network: NetworkConfig{
					MaxPeers: 50,
					MinPeers: 5,
				},
				Sync: SyncConfig{
					BatchSize: 100,
				},
			},
			wantErr: false,
		},
		{
			name: "empty repository dir",
			cfg: &Config{
				RepositoryDir: "",
				Network: NetworkConfig{
					MaxPeers: 50,
					MinPeers: 5,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid peer counts",
			cfg: &Config{
				RepositoryDir: "./test-repos",
				Network: NetworkConfig{
					MaxPeers: 5,
					MinPeers: 10,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid batch size",
			cfg: &Config{
				RepositoryDir: "./test-repos",
				Network: NetworkConfig{
					MaxPeers: 50,
					MinPeers: 5,
				},
				Sync: SyncConfig{
					BatchSize: 0,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigLoadSave(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "gitsync-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test configuration
	testConfig := &Config{
		ListenAddress:  ":9090",
		RepositoryDir:  tempDir,
		BootstrapPeers: []string{"peer1:8080", "peer2:8080"},
		Security: SecurityConfig{
			EnableEncryption: true,
			AuthMode:         "tls",
		},
		Network: NetworkConfig{
			MaxPeers:    100,
			MinPeers:    10,
			PeerTimeout: 5 * time.Minute,
		},
		Sync: SyncConfig{
			SyncInterval:    10 * time.Minute,
			AutoSyncEnabled: true,
		},
		Discovery: DiscoveryConfig{
			PersistenceEnabled: true,
			MaxStoredPeers:     500,
		},
	}

	// Save configuration
	if err := testConfig.save(); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load configuration and compare
	configPath := filepath.Join(tempDir, ".gitsync", "config.yaml")
	loadedConfig := &Config{}
	if err := loadYAMLConfig(configPath, loadedConfig); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded configuration matches saved configuration
	if loadedConfig.ListenAddress != testConfig.ListenAddress {
		t.Errorf("Got listen address %s, want %s", loadedConfig.ListenAddress, testConfig.ListenAddress)
	}
	if loadedConfig.Security.EnableEncryption != testConfig.Security.EnableEncryption {
		t.Errorf("Got encryption enabled %v, want %v", loadedConfig.Security.EnableEncryption, testConfig.Security.EnableEncryption)
	}
	if loadedConfig.Network.MaxPeers != testConfig.Network.MaxPeers {
		t.Errorf("Got max peers %d, want %d", loadedConfig.Network.MaxPeers, testConfig.Network.MaxPeers)
	}
}

func TestConfigSetters(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gitsync-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &Config{
		RepositoryDir: tempDir,
		Network: NetworkConfig{
			MaxPeers: 50,
			MinPeers: 5,
		},
		Sync: SyncConfig{
			BatchSize: 100,
		},
	}

	// Test security settings
	if err := cfg.SetSecurityConfig("enable_encryption", "true"); err != nil {
		t.Errorf("Failed to set security config: %v", err)
	}
	if !cfg.Security.EnableEncryption {
		t.Error("Security.EnableEncryption not set correctly")
	}

	// Test network settings
	if err := cfg.SetNetworkConfig("max_peers", "200"); err != nil {
		t.Errorf("Failed to set network config: %v", err)
	}
	if cfg.Network.MaxPeers != 200 {
		t.Errorf("Network.MaxPeers not set correctly, got %d want 200", cfg.Network.MaxPeers)
	}

	// Test sync settings
	if err := cfg.SetSyncConfig("auto_sync_enabled", "true"); err != nil {
		t.Errorf("Failed to set sync config: %v", err)
	}
	if !cfg.Sync.AutoSyncEnabled {
		t.Error("Sync.AutoSyncEnabled not set correctly")
	}

	// Test invalid settings
	if err := cfg.SetNetworkConfig("invalid_key", "value"); err == nil {
		t.Error("Expected error for invalid network setting key")
	}
}
