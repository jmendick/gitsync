package conflict

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/git"
)

// ConflictStore manages storage of unresolved conflicts
type ConflictStore struct {
	baseDir string
	mu      sync.RWMutex
}

// StoredConflict represents a saved conflict state
type StoredConflict struct {
	ID        string                `json:"id"`
	RepoPath  string                `json:"repo_path"`
	FilePath  string                `json:"file_path"`
	Versions  []git.ConflictVersion `json:"versions"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
	Status    string                `json:"status"`
}

func NewConflictStore(baseDir string) (*ConflictStore, error) {
	storageDir := filepath.Join(baseDir, ".gitsync", "conflicts")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create conflict store directory: %w", err)
	}

	return &ConflictStore{
		baseDir: storageDir,
	}, nil
}

func (cs *ConflictStore) StoreConflict(repoPath string, conflict *git.Conflict) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	stored := &StoredConflict{
		ID:        conflict.ID,
		RepoPath:  repoPath,
		FilePath:  conflict.FilePath,
		Versions:  conflict.Versions,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    "unresolved",
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal conflict: %w", err)
	}

	filename := filepath.Join(cs.baseDir, fmt.Sprintf("%s.json", conflict.ID))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write conflict file: %w", err)
	}

	return nil
}

func (cs *ConflictStore) GetConflict(id string) (*StoredConflict, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	filename := filepath.Join(cs.baseDir, fmt.Sprintf("%s.json", id))
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read conflict file: %w", err)
	}

	var conflict StoredConflict
	if err := json.Unmarshal(data, &conflict); err != nil {
		return nil, fmt.Errorf("failed to unmarshal conflict: %w", err)
	}

	return &conflict, nil
}

func (cs *ConflictStore) UpdateConflictStatus(id string, status string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	conflict, err := cs.GetConflict(id)
	if err != nil {
		return err
	}
	if conflict == nil {
		return fmt.Errorf("conflict not found: %s", id)
	}

	conflict.Status = status
	conflict.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(conflict, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal conflict: %w", err)
	}

	filename := filepath.Join(cs.baseDir, fmt.Sprintf("%s.json", id))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write conflict file: %w", err)
	}

	return nil
}

func (cs *ConflictStore) ListUnresolvedConflicts(repoPath string) ([]*StoredConflict, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	files, err := os.ReadDir(cs.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read conflict directory: %w", err)
	}

	conflicts := make([]*StoredConflict, 0)
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".json" {
			data, err := os.ReadFile(filepath.Join(cs.baseDir, file.Name()))
			if err != nil {
				continue
			}

			var conflict StoredConflict
			if err := json.Unmarshal(data, &conflict); err != nil {
				continue
			}

			if conflict.RepoPath == repoPath && conflict.Status == "unresolved" {
				conflicts = append(conflicts, &conflict)
			}
		}
	}

	return conflicts, nil
}
