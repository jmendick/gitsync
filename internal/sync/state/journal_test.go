package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestJournal(t *testing.T) (*SyncJournal, string, func()) {
	tempDir, err := os.MkdirTemp("", "gitsync-journal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	journal := NewSyncJournal(tempDir)
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return journal, tempDir, cleanup
}

func createTestFiles(t *testing.T, dir string, files []string) {
	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}
}

func TestSyncJournal(t *testing.T) {
	t.Run("CreateAndGetEntry", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		entry := journal.CreateEntry(tempDir, "peer1")
		if entry.ID == "" {
			t.Error("Entry ID should not be empty")
		}
		if entry.RepoPath != tempDir {
			t.Errorf("Expected repo path %s, got %s", tempDir, entry.RepoPath)
		}
		if entry.PeerID != "peer1" {
			t.Errorf("Expected peer ID peer1, got %s", entry.PeerID)
		}
		if entry.Status != "started" {
			t.Errorf("Expected status started, got %s", entry.Status)
		}

		// Test GetEntry
		retrieved, exists := journal.GetEntry(entry.ID)
		if !exists {
			t.Error("Entry should exist")
		}
		if retrieved.ID != entry.ID {
			t.Errorf("Retrieved entry ID mismatch: expected %s, got %s", entry.ID, retrieved.ID)
		}
	})

	t.Run("UpdateEntry", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		entry := journal.CreateEntry(tempDir, "peer1")
		entry.Status = "syncing"
		entry.Progress = 50.0

		err := journal.UpdateEntry(entry)
		if err != nil {
			t.Fatalf("Failed to update entry: %v", err)
		}

		updated, exists := journal.GetEntry(entry.ID)
		if !exists {
			t.Fatal("Entry should exist after update")
		}
		if updated.Status != "syncing" {
			t.Errorf("Expected status syncing, got %s", updated.Status)
		}
		if updated.Progress != 50.0 {
			t.Errorf("Expected progress 50.0, got %f", updated.Progress)
		}
	})

	t.Run("BatchProcessing", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		entry := journal.CreateEntry(tempDir, "peer1")

		// Test setting current batch
		batch := []string{"file1.txt", "file2.txt"}
		journal.SetCurrentBatch(entry.ID, batch, 50)

		updated, _ := journal.GetEntry(entry.ID)
		if len(updated.CurrentBatch) != 2 {
			t.Errorf("Expected 2 files in current batch, got %d", len(updated.CurrentBatch))
		}
		if updated.BatchProgress != 50 {
			t.Errorf("Expected batch progress 50, got %d", updated.BatchProgress)
		}
	})

	t.Run("ProgressTracking", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		files := []string{"progress1.txt", "progress2.txt", "progress3.txt"}
		createTestFiles(t, tempDir, files)

		entry := journal.CreateEntry(tempDir, "peer1")
		entry.PendingFiles = files
		journal.UpdateEntry(entry)

		// Update progress for some files
		journal.UpdateProgress(entry.ID, []string{"progress1.txt", "progress2.txt"}, false)

		updated, _ := journal.GetEntry(entry.ID)
		if len(updated.PendingFiles) != 1 {
			t.Errorf("Expected 1 pending file, got %d", len(updated.PendingFiles))
		}
		if updated.PendingFiles[0] != "progress3.txt" {
			t.Errorf("Expected progress3.txt to be pending, got %s", updated.PendingFiles[0])
		}
	})

	t.Run("PersistenceAndRecovery", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		// Create and update entry
		entry := journal.CreateEntry(tempDir, "peer1")
		entry.Status = "syncing"
		entry.Progress = 75.0
		journal.UpdateEntry(entry)

		// Create new journal instance
		journal2 := NewSyncJournal(tempDir)
		if err := journal2.load(); err != nil {
			t.Fatalf("Failed to load journal: %v", err)
		}

		loaded, exists := journal2.GetEntry(entry.ID)
		if !exists {
			t.Fatal("Entry should exist after loading")
		}
		if loaded.Status != "syncing" {
			t.Errorf("Expected status syncing, got %s", loaded.Status)
		}
		if loaded.Progress != 75.0 {
			t.Errorf("Expected progress 75.0, got %f", loaded.Progress)
		}
	})

	t.Run("Checksumming", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		files := []string{"check1.txt", "check2.txt"}
		createTestFiles(t, tempDir, files)

		checksum1, err := journal.CalculateChecksum(tempDir, files)
		if err != nil {
			t.Fatalf("Failed to calculate checksum: %v", err)
		}

		// Modify a file
		err = os.WriteFile(filepath.Join(tempDir, "check1.txt"), []byte("modified content"), 0644)
		if err != nil {
			t.Fatalf("Failed to modify file: %v", err)
		}

		checksum2, err := journal.CalculateChecksum(tempDir, files)
		if err != nil {
			t.Fatalf("Failed to calculate second checksum: %v", err)
		}

		if checksum1 == checksum2 {
			t.Error("Checksums should be different after file modification")
		}
	})

	t.Run("EntryExpiration", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		// Create an old entry
		entry := journal.CreateEntry(tempDir, "peer1")
		entry.LastUpdate = time.Now().Add(-25 * time.Hour)
		journal.UpdateEntry(entry)

		// Load journal again
		journal2 := NewSyncJournal(tempDir)
		if err := journal2.load(); err != nil {
			t.Fatalf("Failed to load journal: %v", err)
		}

		// Old entry should be cleaned up
		if _, exists := journal2.GetEntry(entry.ID); exists {
			t.Error("Expired entry should have been cleaned up")
		}
	})

	t.Run("ByteCalculation", func(t *testing.T) {
		journal, tempDir, cleanup := setupTestJournal(t)
		defer cleanup()

		files := []string{"bytes1.txt", "bytes2.txt"}
		createTestFiles(t, tempDir, files)

		totalBytes := journal.calculateTotalBytes(tempDir, files)
		expectedBytes := int64(len("test content") * len(files))
		if totalBytes != expectedBytes {
			t.Errorf("Expected %d total bytes, got %d", expectedBytes, totalBytes)
		}
	})
}
