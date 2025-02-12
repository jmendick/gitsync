package git

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/jmendick/gitsync/internal/auth"
)

// RepositoryMetadata contains GitHub-specific repository information
type RepositoryMetadata struct {
	Owner         string
	Name          string
	CloneURL      string
	Private       bool
	DefaultBranch string
}

// ParseGitHubURL extracts owner and repo name from GitHub URL
func ParseGitHubURL(repoURL string) (*RepositoryMetadata, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid GitHub repository URL")
	}

	return &RepositoryMetadata{
		Owner:    parts[0],
		Name:     parts[1],
		CloneURL: repoURL,
	}, nil
}

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

// RemoteConfig represents configuration for a remote
type RemoteConfig struct {
	Name     string
	URLs     []string
	Priority int // Priority for synchronization (higher number = higher priority)
}

// MergeStrategy represents different merge strategies
type MergeStrategy string

const (
	// MergeStrategyOctopus uses the octopus merge strategy for multiple branches
	MergeStrategyOctopus MergeStrategy = "octopus"
	// MergeStrategyResolve uses the resolve merge strategy
	MergeStrategyResolve MergeStrategy = "resolve"
	// MergeStrategyOurs takes our version in conflicts
	MergeStrategyOurs MergeStrategy = "ours"
)

// OpenRepository opens an existing Git repository or initializes a new one if it doesn't exist.
func (m *GitRepositoryManager) OpenRepository(repoName string) (*git.Repository, error) {
	repoPath := filepath.Join(m.baseDir, repoName)
	repo, err := git.PlainOpen(repoPath)
	if err == git.ErrRepositoryNotExists {
		repo, err = git.PlainInit(repoPath, false) // 'false' for not bare repository
		if err != nil {
			return nil, fmt.Errorf("failed to initialize repository: %w", err)
		}
		fmt.Printf("Initialized new Git repository at: %s\n", repoPath)
		return repo, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}
	fmt.Printf("Opened existing Git repository at: %s\n", repoPath)
	return repo, nil
}

// CloneRepository clones a remote Git repository to the local repository directory.
func (m *GitRepositoryManager) CloneRepository(ctx context.Context, repoName string, remoteURL string) (*git.Repository, error) {
	repoPath := filepath.Join(m.baseDir, repoName)
	_, err := os.Stat(repoPath)
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("repository '%s' already exists locally", repoName)
	}

	repo, err := git.PlainCloneContext(ctx, repoPath, false, &git.CloneOptions{
		URL:      remoteURL,
		Progress: os.Stdout, // Optionally show clone progress
	})
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	fmt.Printf("Cloned repository '%s' from '%s' to '%s'\n", repoName, remoteURL, repoPath)
	return repo, nil
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

// SyncBranch synchronizes a specific branch with its remote counterpart
func (m *GitRepositoryManager) SyncBranch(ctx context.Context, repo *git.Repository, branchName string, strategy MergeStrategy) error {
	// First, fetch the latest changes
	if err := m.FetchRepository(ctx, repo); err != nil && err != transport.ErrEmptyRemoteRepository {
		return fmt.Errorf("failed to fetch updates: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Checkout the target branch
	err = worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Create: false,
	})
	if err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	// Get the remote branch reference
	remoteBranch := plumbing.NewRemoteReferenceName("origin", branchName)
	remoteRef, err := repo.Reference(remoteBranch, true)
	if err != nil {
		return fmt.Errorf("failed to get remote branch reference: %w", err)
	}

	// Create a temporary reference for merge
	mergeRefName := plumbing.NewBranchReferenceName("temp-merge-" + branchName)
	mergeRef := plumbing.NewHashReference(mergeRefName, remoteRef.Hash())
	if err := repo.Storer.SetReference(mergeRef); err != nil {
		return fmt.Errorf("failed to create merge reference: %w", err)
	}
	defer repo.Storer.RemoveReference(mergeRefName)

	// Apply merge strategy
	mergeOptions := &git.CheckoutOptions{
		Branch: mergeRefName,
		Force:  true,
	}

	switch strategy {
	case MergeStrategyOurs:
		// For "ours" strategy, we keep our changes and ignore theirs
		return nil
	default:
		// Default to resolve strategy - apply their changes and handle conflicts manually
		err = worktree.Checkout(mergeOptions)
		if err != nil {
			return fmt.Errorf("failed to merge changes: %w", err)
		}
	}

	return nil
}

// ListBranches returns a list of all branches in the repository
func (m *GitRepositoryManager) ListBranches(repo *git.Repository) ([]string, error) {
	branches := []string{}

	branchRefs, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	err = branchRefs.ForEach(func(ref *plumbing.Reference) error {
		branchName := ref.Name().Short()
		branches = append(branches, branchName)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate branches: %w", err)
	}

	return branches, nil
}

// CreateBranch creates a new branch in the repository
func (m *GitRepositoryManager) CreateBranch(repo *git.Repository, branchName string, startPoint *plumbing.Reference) error {
	headRef := startPoint
	if startPoint == nil {
		var err error
		headRef, err = repo.Head()
		if err != nil {
			return fmt.Errorf("failed to get HEAD reference: %w", err)
		}
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash:   headRef.Hash(),
		Branch: plumbing.NewBranchReferenceName(branchName),
		Create: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	fmt.Printf("Created new branch '%s' at %s\n", branchName, headRef.Hash())
	return nil
}

// DeleteBranch deletes a branch from the repository
func (m *GitRepositoryManager) DeleteBranch(repo *git.Repository, branchName string) error {
	err := repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(branchName))
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}

	fmt.Printf("Deleted branch '%s'\n", branchName)
	return nil
}

// SyncAllBranches synchronizes all tracked branches with their remote counterparts
func (m *GitRepositoryManager) SyncAllBranches(ctx context.Context, repo *git.Repository, strategy MergeStrategy) error {
	branches, err := m.ListBranches(repo)
	if err != nil {
		return fmt.Errorf("failed to list branches: %w", err)
	}

	for _, branch := range branches {
		if err := m.SyncBranch(ctx, repo, branch, strategy); err != nil {
			fmt.Printf("Warning: failed to sync branch '%s': %v\n", branch, err)
			continue
		}
	}

	return nil
}

// FetchRepository fetches updates from all remotes.
func (m *GitRepositoryManager) FetchRepository(ctx context.Context, repo *git.Repository) error {
	remotes, err := repo.Remotes()
	if err != nil {
		return fmt.Errorf("failed to get remotes: %w", err)
	}

	for _, remote := range remotes {
		err := repo.FetchContext(ctx, &git.FetchOptions{
			RemoteName: remote.Config().Name,
			Progress:   os.Stdout,
			Force:      true,
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			fmt.Printf("Warning: failed to fetch from remote '%s': %v\n", remote.Config().Name, err)
			continue
		}
	}

	return nil
}

// GetHeadReference returns the HEAD reference of the repository.
func (m *GitRepositoryManager) GetHeadReference(repo *git.Repository) (*plumbing.Reference, error) {
	headRef, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD reference: %w", err)
	}
	return headRef, nil
}

// Commit creates a new commit in the repository.
func (m *GitRepositoryManager) Commit(repo *git.Repository, message string, authorName string, authorEmail string) (plumbing.Hash, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add changes to the staging area
	if _, err := worktree.Add("."); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to add changes: %w", err)
	}

	// Commit the changes
	commitHash, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to commit changes: %w", err)
	}

	fmt.Printf("Created new commit with hash: %s\n", commitHash.String())
	return commitHash, nil
}

// Push pushes the local commits to the remote repository.
func (m *GitRepositoryManager) Push(repo *git.Repository) error {
	// Push the changes to the remote repository
	if err := repo.Push(&git.PushOptions{
		RemoteName: "origin",
	}); err != nil {
		return fmt.Errorf("failed to push changes: %w", err)
	}

	fmt.Println("Pushed changes to remote repository.")
	return nil
}

// Diff shows the differences between the working directory and the index.
func (m *GitRepositoryManager) Diff(repo *git.Repository) (string, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	diffs, err := worktree.Status()
	if err != nil {
		return "", fmt.Errorf("failed to get diffs: %w", err)
	}

	diffStr := diffs.String()
	fmt.Println("Differences:", diffStr)
	return diffStr, nil
}

func (m *GitRepositoryManager) GetRepoMetadata(ctx context.Context, user *auth.User, owner, repo string) (*RepositoryMetadata, error) {
	if !user.IsGitHubUser() {
		return nil, fmt.Errorf("GitHub authentication required")
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+user.Git.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get repository metadata: %d", resp.StatusCode)
	}

	var repoData struct {
		CloneURL      string `json:"clone_url"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoData); err != nil {
		return nil, err
	}

	return &RepositoryMetadata{
		Owner:         owner,
		Name:          repo,
		CloneURL:      repoData.CloneURL,
		Private:       repoData.Private,
		DefaultBranch: repoData.DefaultBranch,
	}, nil
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
	if err := permStore.RefreshFromGitHub(user); err != nil {
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
	var repos []string
	for repoPath := range m.repos {
		repos = append(repos, repoPath)
	}
	return repos
}
