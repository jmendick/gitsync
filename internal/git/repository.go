package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
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
