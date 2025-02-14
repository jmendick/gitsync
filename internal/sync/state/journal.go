package state

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SyncJournal manages persistent sync state
type SyncJournal struct {
	storageDir string
	mu         sync.RWMutex
	entries    map[string]*SyncJournalEntry
}

// SyncJournalEntry represents a sync operation entry
type SyncJournalEntry struct {
	ID             string    `json:"id"`
	RepositoryPath string    `json:"repository_path"`
	StartTime      time.Time `json:"start_time"`
	LastUpdate     time.Time `json:"last_update"`
	PendingFiles   []string  `json:"pending_files"`
	CurrentBatch   []string  `json:"current_batch"`
	BatchProgress  int64     `json:"batch_progress"`
	Completed      bool      `json:"completed"`
	RepoPath       string
	PeerID         string
	Status         string
	Progress       float64
	BytesTotal     int64
	BytesSynced    int64
}

func NewSyncJournal(storageDir string) *SyncJournal {
	return &SyncJournal{
		storageDir: storageDir,
		entries:    make(map[string]*SyncJournalEntry),
	}
}

func (j *SyncJournal) CreateEntry(repoPath, peerID string) *SyncJournalEntry {
	entry := &SyncJournalEntry{
		ID:             fmt.Sprintf("%s-%s-%d", repoPath, peerID, time.Now().Unix()),
		RepositoryPath: repoPath,
		RepoPath:       repoPath,
		PeerID:         peerID,
		StartTime:      time.Now(),
		LastUpdate:     time.Now(),
		Status:         "started",
		Completed:      false,
	}

	j.mu.Lock()
	j.entries[entry.ID] = entry
	j.mu.Unlock()

	j.save()
	return entry
}

func (j *SyncJournal) UpdateEntry(entry *SyncJournalEntry) error {
	j.mu.Lock()
	j.entries[entry.ID] = entry
	j.mu.Unlock()

	return j.save()
}

func (j *SyncJournal) GetEntry(id string) (*SyncJournalEntry, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	entry, exists := j.entries[id]
	return entry, exists
}

func (j *SyncJournal) SetCurrentBatch(id string, batch []string, progress int64) {
	j.mu.Lock()
	if entry, exists := j.entries[id]; exists {
		entry.CurrentBatch = batch
		entry.BatchProgress = progress
		entry.LastUpdate = time.Now()
	}
	j.mu.Unlock()

	j.save()
}

func (j *SyncJournal) UpdateProgress(id string, completedFiles []string, failed bool) {
	j.mu.Lock()
	entry, exists := j.entries[id]
	if !exists {
		j.mu.Unlock()
		return
	}

	// Remove completed files from pending
	if !failed {
		remaining := make([]string, 0, len(entry.PendingFiles))
		for _, f := range entry.PendingFiles {
			isCompleted := false
			for _, cf := range completedFiles {
				if f == cf {
					isCompleted = true
					break
				}
			}
			if !isCompleted {
				remaining = append(remaining, f)
			}
		}
		entry.PendingFiles = remaining
	}

	entry.CurrentBatch = nil
	entry.BatchProgress = 0
	entry.LastUpdate = time.Now()
	j.mu.Unlock()

	j.save()
}

func (j *SyncJournal) GetIncompleteEntries() []*SyncJournalEntry {
	j.mu.RLock()
	defer j.mu.RUnlock()

	incomplete := make([]*SyncJournalEntry, 0)
	for _, entry := range j.entries {
		if !entry.Completed && time.Since(entry.LastUpdate) < 24*time.Hour {
			incomplete = append(incomplete, entry)
		}
	}
	return incomplete
}

func (j *SyncJournal) RemoveCompletedEntry(id string) {
	j.mu.Lock()
	if entry, exists := j.entries[id]; exists {
		entry.Completed = true
		entry.LastUpdate = time.Now()
	}
	j.mu.Unlock()

	j.save()
}

func (j *SyncJournal) save() error {
	if err := os.MkdirAll(j.storageDir, 0755); err != nil {
		return fmt.Errorf("failed to create journal directory: %w", err)
	}

	j.mu.RLock()
	data, err := json.MarshalIndent(j.entries, "", "  ")
	j.mu.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to marshal journal: %w", err)
	}

	journalPath := filepath.Join(j.storageDir, "sync_journal.json")
	tempPath := journalPath + ".tmp"

	// Write to temp file first
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write journal: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, journalPath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("failed to finalize journal: %w", err)
	}

	return nil
}

func (j *SyncJournal) load() error {
	journalPath := filepath.Join(j.storageDir, "sync_journal.json")
	data, err := os.ReadFile(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No existing journal is ok
		}
		return fmt.Errorf("failed to read journal: %w", err)
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if err := json.Unmarshal(data, &j.entries); err != nil {
		return fmt.Errorf("failed to parse journal: %w", err)
	}

	// Clean up old entries
	for id, entry := range j.entries {
		if entry.Completed || time.Since(entry.LastUpdate) > 24*time.Hour {
			delete(j.entries, id)
		}
	}

	return nil
}

func (j *SyncJournal) calculateTotalBytes(repoPath string, files []string) int64 {
	var total int64
	for _, file := range files {
		fullPath := filepath.Join(repoPath, file)
		if info, err := os.Stat(fullPath); err == nil {
			total += info.Size()
		}
	}
	return total
}

func (j *SyncJournal) CalculateChecksum(repoPath string, files []string) (string, error) {
	h := sha256.New()
	for _, file := range files {
		fullPath := filepath.Join(repoPath, file)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", err
		}
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
