package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmendick/gitsync/internal/model"
)

// StoreManager manages data persistence for gitsync.
type StoreManager struct {
	dataDir string
}

// NewStoreManager creates a new StoreManager.
func NewStoreManager(dataDir string) (*StoreManager, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("data directory cannot be empty")
	}
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create data directory: %w", err)
		}
	}
	return &StoreManager{
		dataDir: dataDir,
	}, nil
}

// SavePeerInfo saves peer information to the store.
func (sm *StoreManager) SavePeerInfo(peerInfo *model.PeerInfo) error {
	if peerInfo == nil || peerInfo.ID == "" {
		return fmt.Errorf("invalid peer info")
	}

	data, err := json.Marshal(peerInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal peer info: %w", err)
	}

	filePath := sm.getDataFilePath(fmt.Sprintf("peer_%s.json", peerInfo.ID))
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write peer info file: %w", err)
	}

	return nil
}

// GetPeerInfo retrieves peer information from the store.
func (sm *StoreManager) GetPeerInfo(peerID string) (*model.PeerInfo, error) {
	if peerID == "" {
		return nil, fmt.Errorf("peer ID cannot be empty")
	}

	filePath := sm.getDataFilePath(fmt.Sprintf("peer_%s.json", peerID))
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("peer info not found for ID: %s", peerID)
		}
		return nil, fmt.Errorf("failed to read peer info file: %w", err)
	}

	var peerInfo model.PeerInfo
	if err := json.Unmarshal(data, &peerInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal peer info: %w", err)
	}

	return &peerInfo, nil
}

// SaveRepositoryMetadata saves repository metadata to the store.
func (sm *StoreManager) SaveRepositoryMetadata(metadata *model.RepositoryMetadata) error {
	if metadata == nil || metadata.Name == "" {
		return fmt.Errorf("invalid repository metadata")
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal repository metadata: %w", err)
	}

	filePath := sm.getDataFilePath(fmt.Sprintf("repo_%s.json", metadata.Name))
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write repository metadata file: %w", err)
	}

	return nil
}

// GetRepositoryMetadata retrieves repository metadata from the store.
func (sm *StoreManager) GetRepositoryMetadata(repoName string) (*model.RepositoryMetadata, error) {
	if repoName == "" {
		return nil, fmt.Errorf("repository name cannot be empty")
	}

	filePath := sm.getDataFilePath(fmt.Sprintf("repo_%s.json", repoName))
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("repository metadata not found for: %s", repoName)
		}
		return nil, fmt.Errorf("failed to read repository metadata file: %w", err)
	}

	var metadata model.RepositoryMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal repository metadata: %w", err)
	}

	return &metadata, nil
}

// getDataFilePath constructs the full path to a data file within the data directory.
func (sm *StoreManager) getDataFilePath(filename string) string {
	return filepath.Join(sm.dataDir, filename)
}
