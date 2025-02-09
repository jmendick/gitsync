package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
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
		URL: remoteURL,
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

// ... (Add more Git related functions like Commit, Push, Diff, etc. as needed) ...