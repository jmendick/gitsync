package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SyncState represents the synchronization state with a peer
type SyncState struct {
	LastSyncHash string    `json:"last_sync_hash"`
	LastSyncTime time.Time `json:"last_sync_time"`
	SyncCount    int       `json:"sync_count"`
	Success      bool      `json:"success"`
}

// PeerSyncState tracks sync state per repository per peer
type PeerSyncState struct {
	RepoPath  string                `json:"repo_path"`
	PeerStats map[string]*SyncState `json:"peer_stats"`
	mu        sync.RWMutex          `json:"-"`
}

// StateManager manages sync state persistence
type StateManager struct {
	baseDir     string
	stateFiles  map[string]*PeerSyncState
	mu          sync.RWMutex
	storageFile string
}

// NewStateManager creates a new sync state manager
func NewStateManager(baseDir string) (*StateManager, error) {
	sm := &StateManager{
		baseDir:     baseDir,
		stateFiles:  make(map[string]*PeerSyncState),
		storageFile: filepath.Join(baseDir, ".gitsync", "sync_state.json"),
	}

	if err := os.MkdirAll(filepath.Dir(sm.storageFile), 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	if err := sm.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load sync state: %w", err)
	}

	return sm, nil
}

// UpdateSyncPoint updates the sync state for a repository and peer
func (sm *StateManager) UpdateSyncPoint(repoPath, peerID, commitHash string, success bool) error {
	sm.mu.Lock()
	state, exists := sm.stateFiles[repoPath]
	if !exists {
		state = &PeerSyncState{
			RepoPath:  repoPath,
			PeerStats: make(map[string]*SyncState),
		}
		sm.stateFiles[repoPath] = state
	}
	sm.mu.Unlock()

	state.mu.Lock()
	state.PeerStats[peerID] = &SyncState{
		LastSyncHash: commitHash,
		LastSyncTime: time.Now(),
		SyncCount:    state.PeerStats[peerID].SyncCount + 1,
		Success:      success,
	}
	state.mu.Unlock()

	return sm.save()
}

// GetLastSyncPoint returns the last successful sync point for a repository and peer
func (sm *StateManager) GetLastSyncPoint(repoPath, peerID string) (string, error) {
	sm.mu.RLock()
	state, exists := sm.stateFiles[repoPath]
	sm.mu.RUnlock()

	if !exists {
		return "", nil
	}

	state.mu.RLock()
	syncState, exists := state.PeerStats[peerID]
	state.mu.RUnlock()

	if !exists || !syncState.Success {
		return "", nil
	}

	return syncState.LastSyncHash, nil
}

// GetPeerSyncStats returns sync statistics for a peer
func (sm *StateManager) GetPeerSyncStats(peerID string) map[string]*SyncState {
	stats := make(map[string]*SyncState)

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for repoPath, state := range sm.stateFiles {
		state.mu.RLock()
		if syncState, exists := state.PeerStats[peerID]; exists {
			stats[repoPath] = syncState
		}
		state.mu.RUnlock()
	}

	return stats
}

// save persists the sync state to disk
func (sm *StateManager) save() error {
	data, err := json.MarshalIndent(sm.stateFiles, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sync state: %w", err)
	}

	if err := os.WriteFile(sm.storageFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write sync state: %w", err)
	}

	return nil
}

// load reads the sync state from disk
func (sm *StateManager) load() error {
	data, err := os.ReadFile(sm.storageFile)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &sm.stateFiles)
}
