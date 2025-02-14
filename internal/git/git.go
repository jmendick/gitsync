package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	MergeStrategyOurs   MergeStrategy = "ours"
	MergeStrategyTheirs MergeStrategy = "theirs"
	MergeStrategyUnion  MergeStrategy = "union"
	MergeStrategyManual MergeStrategy = "manual"
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
	err = json.NewDecoder(resp.Body).Decode(&repoData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode repository data: %w", err)
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

// GetChangesSince returns list of changes since a specific commit
func (m *GitRepositoryManager) GetChangesSince(repo *git.Repository, commitHash string) ([]string, error) {
	if commitHash == "" {
		return m.getAllFiles(repo)
	}

	from, err := repo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return nil, fmt.Errorf("failed to get commit object: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	to, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	patch, err := from.Patch(to)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %w", err)
	}

	var changes []string
	for _, filePatch := range patch.FilePatches() {
		from, to := filePatch.Files()
		if from != nil {
			changes = append(changes, from.Path())
		}
		if to != nil && (from == nil || to.Path() != from.Path()) {
			changes = append(changes, to.Path())
		}
	}

	return changes, nil
}

// GetFilteredChanges returns changes that match include patterns and don't match exclude patterns
func (m *GitRepositoryManager) GetFilteredChanges(repo *git.Repository, includes, excludes []string) ([]string, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	var changes []string
	for filePath := range status {
		if shouldIncludeFile(filePath, includes, excludes) {
			changes = append(changes, filePath)
		}
	}

	return changes, nil
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

// GetConflictVersions returns different versions of a file in conflict
func (m *GitRepositoryManager) GetConflictVersions(repo *git.Repository, path string) ([]ConflictVersion, error) {
	w, err := repo.Worktree()
	if err != nil {
		return nil, err
	}

	status, err := w.Status()
	if err != nil {
		return nil, err
	}

	fileStatus := status.File(path)
	if fileStatus.Worktree != git.Untracked && fileStatus.Staging != git.Modified {
		return nil, fmt.Errorf("file is not in conflict")
	}

	// Get our version (stage 2)
	ours := ConflictVersion{
		PeerID:    "ours",
		Timestamp: time.Now(),
	}

	// Get their version (stage 3)
	theirs := ConflictVersion{
		PeerID:    "theirs",
		Timestamp: time.Now(),
	}

	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}

	for _, e := range idx.Entries {
		if e.Name == path {
			obj, err := repo.Object(plumbing.BlobObject, e.Hash)
			if err != nil {
				continue
			}
			if blob, ok := obj.(*object.Blob); ok {
				content, err := blob.Reader()
				if err == nil {
					data, err := io.ReadAll(content)
					if err == nil {
						if e.Stage == 2 {
							ours.Hash = e.Hash.String()
							ours.Content = data
						} else if e.Stage == 3 {
							theirs.Hash = e.Hash.String()
							theirs.Content = data
						}
					}
					content.Close()
				}
			}
		}
	}

	versions := []ConflictVersion{ours, theirs}
	return versions, nil
}

// ResolveConflict resolves a conflict by choosing a strategy
func (m *GitRepositoryManager) ResolveConflict(repo *git.Repository, path string, strategy string) error {
	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	switch strategy {
	case "ours":
		return w.Checkout(&git.CheckoutOptions{
			Hash: plumbing.NewHash("HEAD"),
		})
	case "theirs":
		return w.Checkout(&git.CheckoutOptions{
			Hash: plumbing.NewHash("MERGE_HEAD"),
		})
	case "union":
		versions, err := m.GetConflictVersions(repo, path)
		if err != nil {
			return err
		}

		var combined []byte
		for _, version := range versions {
			combined = append(combined, []byte("<<<<<<< "+version.PeerID+"\n")...)
			combined = append(combined, version.Content...)
			combined = append(combined, []byte("\n=======\n")...)
		}
		combined = append(combined, []byte(">>>>>>> END\n")...)

		file, err := w.Filesystem.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := file.Write(combined); err != nil {
			return err
		}

		_, err = w.Add(path)
		return err
	default:
		return fmt.Errorf("unknown conflict resolution strategy: %s", strategy)
	}
}

// ConflictVersion represents a version of a file in conflict
type ConflictVersion struct {
	PeerID    string    `json:"peer_id"`
	Timestamp time.Time `json:"timestamp"`
	Hash      string    `json:"hash"`
	Content   []byte    `json:"content"`
}

// Conflict represents a Git merge conflict
type Conflict struct {
	ID        string            `json:"id"`
	FilePath  string            `json:"file_path"`
	Versions  []ConflictVersion `json:"versions"`
	Status    string            `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
}

// ConflictResolutionResult represents the result of conflict resolution
type ConflictResolutionResult struct {
	FilePath      string
	ResolvingHash string
	Strategy      MergeStrategy
	Success       bool
	Error         error
}

// Helper functions

func (m *GitRepositoryManager) getAllFiles(repo *git.Repository) ([]string, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, err
	}

	var files []string
	err = filepath.Walk(worktree.Filesystem.Root(), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(worktree.Filesystem.Root(), path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}
		return nil
	})

	return files, err
}

func shouldIncludeFile(filePath string, includes, excludes []string) bool {
	// If no include patterns are specified, include everything
	if len(includes) == 0 {
		includes = []string{"*"}
	}

	included := false
	for _, pattern := range includes {
		if match, _ := filepath.Match(pattern, filePath); match {
			included = true
			break
		}
	}

	if !included {
		return false
	}

	// Check exclude patterns
	for _, pattern := range excludes {
		if match, _ := filepath.Match(pattern, filePath); match {
			return false
		}
	}

	return true
}
