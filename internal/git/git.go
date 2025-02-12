package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// GitRepositoryManager manages Git repositories.
type GitRepositoryManager struct {
	repoDir string
}

// NewGitRepositoryManager creates a new GitRepositoryManager.
func NewGitRepositoryManager(repoDir string) (*GitRepositoryManager, error) {
	if repoDir == "" {
		return nil, fmt.Errorf("repository directory cannot be empty")
	}
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		if err := os.MkdirAll(repoDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create repository directory: %w", err)
		}
	}
	return &GitRepositoryManager{repoDir: repoDir}, nil
}

// OpenRepository opens an existing Git repository or initializes a new one if it doesn't exist.
func (m *GitRepositoryManager) OpenRepository(repoName string) (*git.Repository, error) {
	repoPath := filepath.Join(m.repoDir, repoName)
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
	repoPath := filepath.Join(m.repoDir, repoName)
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

// FetchRepository fetches updates from remote.
func (m *GitRepositoryManager) FetchRepository(ctx context.Context, repo *git.Repository) error {
	err := repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin", // Assuming 'origin' is the remote name
		Progress:   os.Stdout,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate { // Ignore "already up-to-date" errors
		return fmt.Errorf("failed to fetch repository: %w", err)
	}
	if err == nil {
		fmt.Println("Fetched updates from remote.")
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
