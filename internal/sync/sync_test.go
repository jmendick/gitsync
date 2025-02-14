package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/jmendick/gitsync/internal/auth"
	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/p2p"
	"github.com/jmendick/gitsync/internal/testutil"
)

// MockP2PNode implements minimal P2P functionality for testing
type MockP2PNode struct {
	p2p.Node // Embed p2p.Node to satisfy interface
	t        *testing.T
}

func NewMockP2PNode(t *testing.T) *MockP2PNode {
	return &MockP2PNode{t: t}
}

func (m *MockP2PNode) GetPeerDiscovery() interface{} {
	return &MockDiscovery{}
}

type MockDiscovery struct{}

func (m *MockDiscovery) UpdatePeerStatistics(peerID string, success bool, latency time.Duration) {}

// MockAuthStore implements minimal auth functionality for testing
type MockAuthStore struct{}

func (m *MockAuthStore) GetUser(string) (*auth.User, error)                         { return &auth.User{}, nil }
func (m *MockAuthStore) GetUserByEmail(email string) (*auth.User, error)            { return &auth.User{}, nil }
func (m *MockAuthStore) ListUsers() ([]*auth.User, error)                           { return []*auth.User{}, nil }
func (m *MockAuthStore) UpdateUser(user *auth.User) error                           { return nil }
func (m *MockAuthStore) Authenticate(string, string) (*auth.User, error)            { return &auth.User{}, nil }
func (m *MockAuthStore) CreateUser(username, password string, role auth.Role) error { return nil }
func (m *MockAuthStore) DeleteUser(username string) error                           { return nil }

func setupTestSync(t *testing.T) (*SyncManager, string, func()) {
	tempDir, err := os.MkdirTemp("", "gitsync-sync-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cfg := testutil.NewMockConfig()
	cfg.Config.RepositoryDir = tempDir
	cfg.Config.Sync = config.SyncConfig{
		SyncInterval:     5 * time.Minute,
		MaxSyncAttempts:  3,
		SyncMode:         "incremental",
		ConflictStrategy: "manual",
		AutoSyncEnabled:  true,
		BatchSize:        100,
	}

	node := NewMockP2PNode(t)
	authStore := &MockAuthStore{}

	syncManager, err := NewSyncManager(cfg.Config, &node.Node, authStore)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create sync manager: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return syncManager, tempDir, cleanup
}

func TestSyncManager_IncrementalSync(t *testing.T) {
	sm, tmpDir, cleanup := setupTestSync(t)
	defer cleanup()

	repoPath := filepath.Join(tmpDir, "test-owner/test-repo")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("Failed to create test repo dir: %v", err)
	}

	repo, err := git.PlainInit(repoPath, false)
	if err != nil {
		t.Fatalf("Failed to initialize test repo: %v", err)
	}

	peer := &model.PeerInfo{
		ID:        "test-peer",
		Addresses: []string{"127.0.0.1:9999"},
	}

	// Test incremental sync
	ctx := context.Background()
	if err := sm.syncWithPeers(ctx, repo, []string{peer.ID}, "test-owner/test-repo"); err != nil {
		t.Errorf("Failed to sync with peer: %v", err)
	}

	// Verify sync state was updated
	hash, err := sm.stateManager.GetLastSyncPoint("test-owner/test-repo", peer.ID)
	if err != nil {
		t.Errorf("Failed to get sync point: %v", err)
	}
	if hash == "" {
		t.Error("Expected non-empty sync hash after sync")
	}
}

func TestBatchProcessing(t *testing.T) {
	sm, tmpDir, cleanup := setupTestSync(t)
	defer cleanup()

	t.Run("BatchCreation", func(t *testing.T) {
		// Create test files
		files := []string{"small.txt", "medium.txt", "large.txt"}
		repoPath := filepath.Join(tmpDir, "test-owner/test-repo")
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create test repo dir: %v", err)
		}

		for _, f := range files {
			path := filepath.Join(repoPath, f)
			if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}
		}

		repo, err := git.PlainInit(repoPath, false)
		if err != nil {
			t.Fatalf("Failed to initialize test repo: %v", err)
		}

		// Test batch syncing
		ctx := context.Background()
		err = sm.syncFiles(ctx, repo, "test-peer", files)
		if err != nil {
			t.Errorf("Failed to sync files: %v", err)
		}
	})

	t.Run("BatchRetry", func(t *testing.T) {
		repoPath := filepath.Join(tmpDir, "test-owner/test-repo-retry")
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create test repo dir: %v", err)
		}

		repo, err := git.PlainInit(repoPath, false)
		if err != nil {
			t.Fatalf("Failed to initialize test repo: %v", err)
		}

		// Try syncing with a failing peer
		ctx := context.Background()
		files := []string{"test.txt"}
		err = sm.syncFiles(ctx, repo, "failing-peer", files)
		if err == nil {
			t.Error("Expected error when syncing with failing peer")
		}
		if !IsRecoverableError(err) {
			t.Error("Expected recoverable error type")
		}
	})
}

func TestAutoSync(t *testing.T) {
	sm, _, cleanup := setupTestSync(t)
	defer cleanup()

	// Start sync manager
	if err := sm.Start(); err != nil {
		t.Fatalf("Failed to start sync manager: %v", err)
	}
	defer sm.Stop()

	// Wait for one sync cycle
	time.Sleep(100 * time.Millisecond)

	// Verify that periodic sync is running
	select {
	case <-sm.progressChan:
		// Successfully received progress update
	case <-time.After(time.Second):
		t.Error("No sync progress received within timeout")
	}
}

func TestConflictResolution(t *testing.T) {
	sm, tmpDir, cleanup := setupTestSync(t)
	defer cleanup()

	repoPath := filepath.Join(tmpDir, "test-owner/test-repo")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("Failed to create test repo dir: %v", err)
	}

	repo, err := git.PlainInit(repoPath, false)
	if err != nil {
		t.Fatalf("Failed to initialize test repo: %v", err)
	}

	// Create a test file with conflict
	filePath := filepath.Join(repoPath, "conflict.txt")
	if err := os.WriteFile(filePath, []byte("local change"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create internal Conflict type from model.Conflict data
	conflict := &Conflict{
		FilePath:  "conflict.txt",
		BaseHash:  "base-hash",
		OurHash:   "local-hash",
		TheirHash: "remote-hash",
	}

	// Test conflict resolution through the conflict manager
	err = sm.conflictManager.HandleConflict(context.Background(), repo, conflict, nil, "manual")
	if err != nil {
		t.Errorf("Failed to handle conflict: %v", err)
	}
}
