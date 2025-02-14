package git

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/jmendick/gitsync/internal/auth"
	"github.com/stretchr/testify/assert"
)

type mockUserStore struct{}

func (m *mockUserStore) GetUser(username string) (*auth.User, error) {
	return &auth.User{}, nil
}

func (m *mockUserStore) CreateUser(username, password string, role auth.Role) error {
	return nil
}

func (m *mockUserStore) UpdateUser(user *auth.User) error {
	return nil
}

func (m *mockUserStore) DeleteUser(username string) error {
	return nil
}

func (m *mockUserStore) ListUsers() ([]*auth.User, error) {
	return nil, nil
}

func (m *mockUserStore) Authenticate(username, password string) (*auth.User, error) {
	return &auth.User{}, nil
}

func (m *mockUserStore) GetUserByEmail(email string) (*auth.User, error) {
	return &auth.User{}, nil
}

func setupTestRepo(t *testing.T) (*GitRepositoryManager, *git.Repository, string) {
	tempDir, err := os.MkdirTemp("", "gitsync-test")
	if err != nil {
		t.Fatal(err)
	}

	manager := NewGitRepositoryManager(tempDir, &mockUserStore{})
	repo, err := git.PlainInit(filepath.Join(tempDir, "test-repo"), false)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatal(err)
	}

	return manager, repo, tempDir
}

// Helper function to create test commits
func createTestCommit(t *testing.T, repo *git.Repository, filename, content string) plumbing.Hash {
	w, err := repo.Worktree()
	assert.NoError(t, err)

	err = os.WriteFile(filepath.Join(w.Filesystem.Root(), filename), []byte(content), 0644)
	assert.NoError(t, err)

	_, err = w.Add(filename)
	assert.NoError(t, err)

	hash, err := w.Commit("Test commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	assert.NoError(t, err)
	return hash
}

func TestBranchOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	// Create initial commit so we can create branches
	createTestCommit(t, repo, "test.txt", "initial content")

	t.Run("CreateAndListBranches", func(t *testing.T) {
		err := manager.CreateBranch(repo, "feature", nil)
		assert.NoError(t, err)

		branches, err := manager.ListBranches(repo)
		assert.NoError(t, err)
		assert.Contains(t, branches, "feature")
	})

	t.Run("DeleteBranch", func(t *testing.T) {
		err := manager.DeleteBranch(repo, "feature")
		assert.NoError(t, err)

		branches, err := manager.ListBranches(repo)
		assert.NoError(t, err)
		assert.NotContains(t, branches, "feature")
	})
}

func TestRemoteOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	t.Run("AddAndListRemotes", func(t *testing.T) {
		err := manager.AddRemote(repo, "test-remote", []string{"https://example.com/test.git"})
		assert.NoError(t, err)

		remotes, err := manager.ListRemotes(repo)
		assert.NoError(t, err)
		assert.Len(t, remotes, 1)
		assert.Equal(t, "test-remote", remotes[0].Name)
	})
}

func TestSyncOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	// Create initial commit
	createTestCommit(t, repo, "test.txt", "initial content")

	ctx := context.Background()

	t.Run("SyncBranchWithDifferentStrategies", func(t *testing.T) {
		strategies := []MergeStrategy{
			MergeStrategyOurs,
			MergeStrategyTheirs,
			MergeStrategyUnion,
			MergeStrategyOctopus,
			MergeStrategyResolve,
		}

		for _, strategy := range strategies {
			t.Run(string(strategy), func(t *testing.T) {
				err := manager.SyncBranch(ctx, repo, "master", strategy)
				assert.NoError(t, err)
			})
		}
	})

	t.Run("FetchRepository", func(t *testing.T) {
		err := manager.FetchRepository(ctx, repo)
		// Since there's no remote, this should return an error
		assert.Error(t, err)
	})
}

func TestCommitOperations(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	t.Run("CommitChanges", func(t *testing.T) {
		// Create a test file
		err := os.WriteFile(filepath.Join(tempDir, "test-repo", "test.txt"), []byte("test"), 0644)
		assert.NoError(t, err)

		hash, err := manager.Commit(repo, "Test commit", "Test Author", "test@example.com")
		assert.NoError(t, err)

		commit, err := repo.CommitObject(hash)
		assert.NoError(t, err)
		assert.Equal(t, "Test commit", commit.Message)
	})

	t.Run("ShowDiff", func(t *testing.T) {
		// Create a change
		err := os.WriteFile(filepath.Join(tempDir, "test-repo", "test2.txt"), []byte("test2"), 0644)
		assert.NoError(t, err)

		diff, err := manager.Diff(repo)
		assert.NoError(t, err)
		assert.NotEmpty(t, diff)
	})
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *RepositoryMetadata
		wantErr bool
	}{
		{
			name: "Valid GitHub URL",
			url:  "https://github.com/owner/repo",
			want: &RepositoryMetadata{
				Owner:    "owner",
				Name:     "repo",
				CloneURL: "https://github.com/owner/repo",
			},
			wantErr: false,
		},
		{
			name:    "Invalid URL",
			url:     "not-a-url",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid GitHub URL format",
			url:     "https://github.com/invalid",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGitHubURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRepositoryManagement(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gitsync-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	manager := NewGitRepositoryManager(tempDir, &mockUserStore{})

	t.Run("OpenRepository", func(t *testing.T) {
		repo, err := manager.OpenRepository("test-repo")
		assert.NoError(t, err)
		assert.NotNil(t, repo)

		// Verify repository exists on disk
		_, err = os.Stat(filepath.Join(tempDir, "test-repo", ".git"))
		assert.NoError(t, err)
	})

	t.Run("OpenExistingRepository", func(t *testing.T) {
		repo2, err := manager.OpenRepository("test-repo")
		assert.NoError(t, err)
		assert.NotNil(t, repo2)
	})
}

func TestChangeTracking(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	initialHash := createTestCommit(t, repo, "test.txt", "initial content")

	t.Run("GetChangesSince", func(t *testing.T) {
		// Create another commit
		createTestCommit(t, repo, "test2.txt", "new content")

		// Get changes since initial commit
		changes, err := manager.GetChangesSince(repo, initialHash.String())
		assert.NoError(t, err)
		assert.Contains(t, changes, "test2.txt")
	})

	t.Run("GetFilteredChanges", func(t *testing.T) {
		// Create test files
		w, err := repo.Worktree()
		assert.NoError(t, err)

		files := []struct {
			name    string
			content string
		}{
			{"doc.txt", "doc content"},
			{"src.go", "go content"},
			{"test.go", "test content"},
		}

		for _, f := range files {
			err := os.WriteFile(filepath.Join(w.Filesystem.Root(), f.name), []byte(f.content), 0644)
			assert.NoError(t, err)
		}

		// Test with include pattern
		changes, err := manager.GetFilteredChanges(repo, []string{"*.go"}, nil)
		assert.NoError(t, err)
		assert.Len(t, changes, 2)
		assert.Contains(t, changes, "src.go")
		assert.Contains(t, changes, "test.go")

		// Test with exclude pattern
		changes, err = manager.GetFilteredChanges(repo, []string{"*"}, []string{"*.txt"})
		assert.NoError(t, err)
		assert.Len(t, changes, 2)
		assert.NotContains(t, changes, "doc.txt")
	})
}

func TestConflictResolution(t *testing.T) {
	manager, repo, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	// Create initial commit
	createTestCommit(t, repo, "conflict.txt", "initial content")

	// Create a conflict scenario by simulating merge conflict
	w, err := repo.Worktree()
	assert.NoError(t, err)

	// Create two branches with different content
	err = manager.CreateBranch(repo, "branch1", nil)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(w.Filesystem.Root(), "conflict.txt"), []byte("branch1 content"), 0644)
	assert.NoError(t, err)
	_, err = manager.Commit(repo, "Branch 1 commit", "test", "test@example.com")
	assert.NoError(t, err)

	err = manager.CreateBranch(repo, "branch2", nil)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(w.Filesystem.Root(), "conflict.txt"), []byte("branch2 content"), 0644)
	assert.NoError(t, err)
	_, err = manager.Commit(repo, "Branch 2 commit", "test", "test@example.com")
	assert.NoError(t, err)

	t.Run("GetConflictVersions", func(t *testing.T) {
		versions, err := manager.GetConflictVersions(repo, "conflict.txt")
		assert.NoError(t, err)
		assert.Len(t, versions, 2)

		// Verify version details
		for _, version := range versions {
			assert.NotEmpty(t, version.PeerID)
			assert.NotEmpty(t, version.Hash)
			assert.NotEmpty(t, version.Content)
			assert.False(t, version.Timestamp.IsZero())

			// Verify content matches one of our branch versions
			content := string(version.Content)
			assert.True(t, content == "branch1 content" || content == "branch2 content",
				"Content should match one of the branch versions")
		}
	})

	t.Run("ResolveConflictWithDifferentStrategies", func(t *testing.T) {
		strategies := []string{"ours", "theirs", "union"}
		expectedContents := map[string]string{
			"ours":   "branch2 content",
			"theirs": "branch1 content",
			"union":  "<<<<<<< ours\nbranch2 content\n=======\nbranch1 content\n>>>>>>> END\n",
		}

		for _, strategy := range strategies {
			t.Run(strategy, func(t *testing.T) {
				err = manager.ResolveConflict(repo, "conflict.txt", strategy)
				assert.NoError(t, err)

				content, err := os.ReadFile(filepath.Join(w.Filesystem.Root(), "conflict.txt"))
				assert.NoError(t, err)
				if strategy != "union" {
					assert.Equal(t, expectedContents[strategy], string(content))
				} else {
					// For union strategy, just check if it contains both versions
					assert.Contains(t, string(content), "branch1 content")
					assert.Contains(t, string(content), "branch2 content")
				}
			})
		}
	})
}

// MockTransport implements http.RoundTripper for testing
type MockTransport struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

func TestGitHubIntegration(t *testing.T) {
	manager, _, tempDir := setupTestRepo(t)
	defer os.RemoveAll(tempDir)

	ctx := context.Background()

	// Mock GitHub user
	user := &auth.User{
		Git: &auth.GitCredentials{
			Username:    "testuser",
			AccessToken: "test-token",
		},
	}

	t.Run("GetRepoMetadata_Success", func(t *testing.T) {
		// Mock successful response
		mockResponse := `{
			"clone_url": "https://github.com/owner/repo.git",
			"private": true,
			"default_branch": "main"
		}`

		originalClient := http.DefaultClient
		defer func() { http.DefaultClient = originalClient }()

		mockTransport := &MockTransport{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(mockResponse)),
				}, nil
			},
		}

		http.DefaultClient = &http.Client{Transport: mockTransport}

		metadata, err := manager.GetRepoMetadata(ctx, user, "owner", "repo")
		assert.NoError(t, err)
		assert.NotNil(t, metadata)
		assert.Equal(t, "owner", metadata.Owner)
		assert.Equal(t, "repo", metadata.Name)
		assert.Equal(t, "https://github.com/owner/repo.git", metadata.CloneURL)
		assert.True(t, metadata.Private)
		assert.Equal(t, "main", metadata.DefaultBranch)
	})

	t.Run("GetRepoMetadata_Unauthorized", func(t *testing.T) {
		mockTransport := &MockTransport{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			},
		}

		http.DefaultClient = &http.Client{Transport: mockTransport}

		metadata, err := manager.GetRepoMetadata(ctx, user, "owner", "repo")
		assert.Error(t, err)
		assert.Nil(t, metadata)
		assert.Contains(t, err.Error(), "failed to get repository metadata: 401")
	})

	t.Run("GetRepoMetadata_NotFound", func(t *testing.T) {
		mockTransport := &MockTransport{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			},
		}

		http.DefaultClient = &http.Client{Transport: mockTransport}

		metadata, err := manager.GetRepoMetadata(ctx, user, "owner", "nonexistent")
		assert.Error(t, err)
		assert.Nil(t, metadata)
		assert.Contains(t, err.Error(), "failed to get repository metadata: 404")
	})
}
