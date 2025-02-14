package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Manager handles the synchronization state persistence
type Manager struct {
	baseDir string
	mu      sync.RWMutex
	state   map[string]map[string]string // map[repoPath]map[peerID]lastSyncPoint
}

// NewManager creates a new state manager
func NewManager(baseDir string) (*Manager, error) {
	m := &Manager{
		baseDir: baseDir,
		state:   make(map[string]map[string]string),
	}

	if err := m.load(); err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	return m, nil
}

// GetLastSyncPoint returns the last successful sync point for a repository and peer
func (m *Manager) GetLastSyncPoint(repoPath, peerID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if peerState, ok := m.state[repoPath]; ok {
		if syncPoint, ok := peerState[peerID]; ok {
			return syncPoint, nil
		}
	}
	return "", nil
}

// UpdateSyncPoint updates the sync point for a repository and peer
func (m *Manager) UpdateSyncPoint(repoPath, peerID, commitHash string, success bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.state[repoPath]; !ok {
		m.state[repoPath] = make(map[string]string)
	}

	if success {
		m.state[repoPath][peerID] = commitHash
	}

	return m.save()
}

func (m *Manager) load() error {
	statePath := filepath.Join(m.baseDir, ".gitsync", "sync_state.json")
	data, err := os.ReadFile(statePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	return json.Unmarshal(data, &m.state)
}

func (m *Manager) save() error {
	statePath := filepath.Join(m.baseDir, ".gitsync", "sync_state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return os.WriteFile(statePath, data, 0644)
}
