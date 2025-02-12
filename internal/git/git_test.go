package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func setupTestRepo(t *testing.T) (*GitRepositoryManager, *git.Repository, string) {
	// Create a temporary directory for the test repository
	tempDir, err := os.MkdirTemp("", "gitsync-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize the repository manager
	manager, err := NewGitRepositoryManager(tempDir)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create repository manager: %v", err)
	}

	// Create a test repository
	repo, err := manager.OpenRepository("test-repo")
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create test repository: %v", err)
	}

	return manager, repo, tempDir
}

func createTestCommit(t *testing.T, repo *git.Repository, filename, content string) plumbing.Hash {
	w, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Failed to get worktree: %v", err)
	}

	// Create a test file
	filepath := filepath.Join(w.Filesystem.Root(), filename)
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Stage the file
	_, err = w.Add(filename)
	if err != nil {
		t.Fatalf("Failed to stage file: %v", err)
	}

	// Create a commit
	hash, err := w.Commit("Test commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test Author",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Failed to create commit: %v", err)
	}

	return hash
}

func TestGitRepositoryManager_BranchOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	// Create an initial commit
	createTestCommit(t, repo, "test.txt", "initial content")

	// Test CreateBranch
	t.Run("CreateBranch", func(t *testing.T) {
		err := manager.CreateBranch(repo, "feature", nil)
		if err != nil {
			t.Errorf("Failed to create branch: %v", err)
		}

		// Verify branch exists
		branches, err := manager.ListBranches(repo)
		if err != nil {
			t.Errorf("Failed to list branches: %v", err)
		}

		found := false
		for _, branch := range branches {
			if branch == "feature" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Created branch not found in branch list")
		}
	})

	// Test DeleteBranch
	t.Run("DeleteBranch", func(t *testing.T) {
		err := manager.DeleteBranch(repo, "feature")
		if err != nil {
			t.Errorf("Failed to delete branch: %v", err)
		}

		// Verify branch is deleted
		branches, err := manager.ListBranches(repo)
		if err != nil {
			t.Errorf("Failed to list branches: %v", err)
		}

		for _, branch := range branches {
			if branch == "feature" {
				t.Error("Branch still exists after deletion")
			}
		}
	})
}

func TestGitRepositoryManager_RemoteOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	// Test AddRemote
	t.Run("AddRemote", func(t *testing.T) {
		err := manager.AddRemote(repo, "test-remote", []string{"https://example.com/test.git"})
		if err != nil {
			t.Errorf("Failed to add remote: %v", err)
		}

		// Verify remote exists
		remotes, err := manager.ListRemotes(repo)
		if err != nil {
			t.Errorf("Failed to list remotes: %v", err)
		}

		found := false
		for _, remote := range remotes {
			if remote.Name == "test-remote" {
				found = true
				if len(remote.URLs) != 1 || remote.URLs[0] != "https://example.com/test.git" {
					t.Error("Remote URL doesn't match expected value")
				}
				break
			}
		}
		if !found {
			t.Error("Added remote not found in remote list")
		}
	})
}

func TestGitRepositoryManager_SyncOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	// Create an initial commit
	createTestCommit(t, repo, "test.txt", "initial content")

	// Test SyncBranch
	t.Run("SyncBranch", func(t *testing.T) {
		ctx := context.Background()
		err := manager.SyncBranch(ctx, repo, "master", MergeStrategyOurs)
		if err != nil {
			// It's okay if this fails due to no remote being configured
			if err.Error() != "remote not found" {
				t.Errorf("Unexpected error in SyncBranch: %v", err)
			}
		}
	})

	// Test FetchRepository
	t.Run("FetchRepository", func(t *testing.T) {
		ctx := context.Background()
		err := manager.FetchRepository(ctx, repo)
		if err != nil {
			// It's okay if this fails due to no remote being configured
			if err.Error() != "remote not found" {
				t.Errorf("Unexpected error in FetchRepository: %v", err)
			}
		}
	})
}

func TestGitRepositoryManager_CommitOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	// Test Commit
	t.Run("Commit", func(t *testing.T) {
		// Create a test file
		w, err := repo.Worktree()
		if err != nil {
			t.Fatalf("Failed to get worktree: %v", err)
		}

		filepath := filepath.Join(w.Filesystem.Root(), "test.txt")
		if err := os.WriteFile(filepath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		hash, err := manager.Commit(repo, "Test commit", "Test Author", "test@example.com")
		if err != nil {
			t.Errorf("Failed to create commit: %v", err)
		}

		// Verify commit exists
		commit, err := repo.CommitObject(hash)
		if err != nil {
			t.Errorf("Failed to get commit object: %v", err)
		}

		if commit.Message != "Test commit" {
			t.Errorf("Commit message doesn't match. Got %s, want %s", commit.Message, "Test commit")
		}
	})

	// Test Diff
	t.Run("Diff", func(t *testing.T) {
		// Modify the test file
		w, err := repo.Worktree()
		if err != nil {
			t.Fatalf("Failed to get worktree: %v", err)
		}

		filepath := filepath.Join(w.Filesystem.Root(), "test.txt")
		if err := os.WriteFile(filepath, []byte("modified content"), 0644); err != nil {
			t.Fatalf("Failed to modify test file: %v", err)
		}

		diff, err := manager.Diff(repo)
		if err != nil {
			t.Errorf("Failed to get diff: %v", err)
		}

		if diff == "" {
			t.Error("Expected non-empty diff")
		}
	})
}
