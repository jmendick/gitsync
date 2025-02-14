package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SyncProgress tracks the progress of a sync operation
type SyncProgress struct {
	StartTime    time.Time
	LastUpdate   time.Time
	CurrentPhase string
	PeerCount    int
	RetryCount   int
	RecentErrors []string
	Progress     float64
	Stats        struct {
		FilesTotal   int64
		FilesCurrent string
		BytesTotal   int64
		BytesSynced  int64
		CurrentSpeed float64
	}
}

// SyncJournal tracks sync operations
type SyncJournal struct {
	baseDir string
	entries map[string]*SyncJournalEntry
	mu      sync.RWMutex
}

type SyncJournalEntry struct {
	ID             string
	RepositoryPath string
	PendingFiles   []string
	CurrentBatch   []string
	Checksum       string
	StartTime      time.Time
	LastUpdate     time.Time
	Status         string
}

func NewSyncJournal(baseDir string) *SyncJournal {
	return &SyncJournal{
		baseDir: baseDir,
		entries: make(map[string]*SyncJournalEntry),
	}
}

func (sj *SyncJournal) GetIncompleteEntries() []*SyncJournalEntry {
	sj.mu.RLock()
	defer sj.mu.RUnlock()

	var incomplete []*SyncJournalEntry
	for _, entry := range sj.entries {
		if entry.Status != "completed" {
			incomplete = append(incomplete, entry)
		}
	}
	return incomplete
}

func (sj *SyncJournal) CreateEntry(repoPath string, files []string) *SyncJournalEntry {
	sj.mu.Lock()
	defer sj.mu.Unlock()

	entry := &SyncJournalEntry{
		ID:             uuid.New().String(),
		RepositoryPath: repoPath,
		PendingFiles:   files,
		StartTime:      time.Now(),
		LastUpdate:     time.Now(),
		Status:         "pending",
	}
	sj.entries[entry.ID] = entry
	return entry
}

func (sj *SyncJournal) CalculateChecksum(repoPath string, files []string) (string, error) {
	hasher := sha256.New()
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(repoPath, file))
		if err != nil {
			return "", err
		}
		hasher.Write(data)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (sj *SyncJournal) UpdateProgress(entryID string, batch []string, failed bool) {
	sj.mu.Lock()
	defer sj.mu.Unlock()

	if entry, exists := sj.entries[entryID]; exists {
		entry.LastUpdate = time.Now()
		if failed {
			entry.Status = "failed"
		}
		entry.CurrentBatch = batch
	}
}

func (sj *SyncJournal) SetCurrentBatch(entryID string, batch []string, offset int) {
	sj.mu.Lock()
	defer sj.mu.Unlock()

	if entry, exists := sj.entries[entryID]; exists {
		entry.CurrentBatch = batch
		entry.LastUpdate = time.Now()
	}
}

func (sj *SyncJournal) RemoveCompletedEntry(entryID string) {
	sj.mu.Lock()
	defer sj.mu.Unlock()
	delete(sj.entries, entryID)
}

// Save persists the journal to disk
func (sj *SyncJournal) Save() error {
	sj.mu.RLock()
	defer sj.mu.RUnlock()

	data, err := json.MarshalIndent(sj.entries, "", "  ")
	if err != nil {
		return err
	}

	journalPath := filepath.Join(sj.baseDir, ".gitsync", "journal.json")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(journalPath, data, 0644)
}

// Load restores the journal from disk
func (sj *SyncJournal) Load() error {
	sj.mu.Lock()
	defer sj.mu.Unlock()

	journalPath := filepath.Join(sj.baseDir, ".gitsync", "journal.json")
	data, err := os.ReadFile(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &sj.entries)
}
