package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/jmendick/gitsync/internal/auth"
)

// GitRepositoryManager manages Git repositories with GitHub integration
type GitRepositoryManager struct {
	baseDir     string
	repos       map[string]*RepositoryMetadata
	authStore   auth.UserStore
	permStores  map[string]*auth.PermissionStore
	permChecker *auth.RepoPermissionChecker
}

// NewGitRepositoryManager creates a new GitRepositoryManager instance
func NewGitRepositoryManager(baseDir string, authStore auth.UserStore) *GitRepositoryManager {
	permChecker := auth.NewRepoPermissionChecker(authStore)
	return &GitRepositoryManager{
		baseDir:     baseDir,
		repos:       make(map[string]*RepositoryMetadata),
		authStore:   authStore,
		permStores:  make(map[string]*auth.PermissionStore),
		permChecker: permChecker,
	}
}

// AddRemote adds a new remote to the repository
func (m *GitRepositoryManager) AddRemote(repo *git.Repository, remoteName string, urls []string) error {
	remoteConfig := &config.RemoteConfig{
		Name: remoteName,
		URLs: urls,
		Fetch: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+refs/heads/*:refs/remotes/%s/*", remoteName)),
		},
	}

	_, err := repo.CreateRemote(remoteConfig)
	if err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}

	fmt.Printf("Added remote '%s' with URLs: %v\n", remoteName, urls)
	return nil
}

// ListRemotes returns a list of configured remotes
func (m *GitRepositoryManager) ListRemotes(repo *git.Repository) ([]*config.RemoteConfig, error) {
	remotes, err := repo.Remotes()
	if err != nil {
		return nil, fmt.Errorf("failed to list remotes: %w", err)
	}

	var configs []*config.RemoteConfig
	for _, remote := range remotes {
		configs = append(configs, remote.Config())
	}
	return configs, nil
}

// Clone clones a GitHub repository using the user's credentials
func (m *GitRepositoryManager) Clone(ctx context.Context, user *auth.User, owner, repo string) error {
	metadata, err := m.GetRepoMetadata(ctx, user, owner, repo)
	if err != nil {
		return err
	}

	// Create permission store for the repository
	repoPath := filepath.Join(m.baseDir, owner, repo)
	permStore, err := auth.NewPermissionStore(repoPath, m.permChecker)
	if err != nil {
		return fmt.Errorf("failed to initialize permission store: %w", err)
	}

	// Initialize repository permissions from GitHub
	err = permStore.RefreshFromGitHub(user)
	if err != nil {
		return fmt.Errorf("failed to initialize repository permissions: %w", err)
	}

	// Use GitHub token for authentication
	cloneURL := strings.Replace(metadata.CloneURL, "https://",
		fmt.Sprintf("https://%s:%s@", user.Git.Username, user.Git.AccessToken), 1)

	// Set up clone options with credentials
	cloneOpts := &git.CloneOptions{
		URL:           cloneURL,
		Progress:      os.Stdout,
		SingleBranch:  true,
		ReferenceName: plumbing.NewBranchReferenceName(metadata.DefaultBranch),
	}

	if _, err := git.PlainCloneContext(ctx, repoPath, false, cloneOpts); err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	m.repos[fmt.Sprintf("%s/%s", owner, repo)] = metadata
	m.permStores[fmt.Sprintf("%s/%s", owner, repo)] = permStore
	return nil
}

// CheckAccess verifies if a user has access to a repository
func (m *GitRepositoryManager) CheckAccess(user *auth.User, owner, repo string) (bool, error) {
	permStore, ok := m.permStores[fmt.Sprintf("%s/%s", owner, repo)]
	if !ok {
		return false, fmt.Errorf("repository not found")
	}

	return permStore.CheckAccess(user)
}

// GetRepositoryList returns a list of all managed repositories
func (m *GitRepositoryManager) GetRepositoryList() []string {
	repos := make([]string, 0, len(m.repos))
	for repoPath := range m.repos {
		repos = append(repos, repoPath)
	}
	return repos
}

// ResetToClean resets the repository to a clean state
func (m *GitRepositoryManager) ResetToClean(repo *git.Repository) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}

	err = worktree.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: head.Hash(),
	})
	if err != nil {
		return fmt.Errorf("failed to reset worktree: %w", err)
	}

	err = worktree.Clean(&git.CleanOptions{
		Dir: true,
	})
	if err != nil {
		return fmt.Errorf("failed to clean worktree: %w", err)
	}

	return nil
}

// Diff shows changes between the working directory and HEAD
func (m *GitRepositoryManager) Diff(repo *git.Repository) (string, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return "", fmt.Errorf("failed to get status: %w", err)
	}

	var diffOutput strings.Builder
	for path, fileStatus := range status {
		diffOutput.WriteString(fmt.Sprintf("%s %s\n", fileStatus.Staging, path))
	}

	return diffOutput.String(), nil
}
