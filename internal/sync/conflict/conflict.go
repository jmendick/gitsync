package conflict

import (
	"fmt"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/jmendick/gitsync/internal/model"
)

// ConflictResolver handles synchronization conflicts.
type ConflictResolver struct {
	repo *git.Repository
}

// NewConflictResolver creates a new ConflictResolver.
func NewConflictResolver() *ConflictResolver {
	return &ConflictResolver{}
}

// ResolveConflict resolves a synchronization conflict.
func (cr *ConflictResolver) ResolveConflict(conflict *model.Conflict) error {
	if conflict == nil {
		return fmt.Errorf("conflict cannot be nil")
	}

	// Get the repository worktree
	worktree, err := cr.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	switch conflict.ConflictType {
	case "Content":
		// For content conflicts, let Git handle the merge
		// This will create conflict markers in the file that the user can resolve manually
		status, err := worktree.Status()
		if err != nil {
			return fmt.Errorf("failed to get worktree status: %w", err)
		}

		// Check if the file is in conflict
		fileStatus, exists := status[conflict.FilePath]
		if !exists || fileStatus.Staging != 'U' { // 'U' represents unmerged status in go-git
			return fmt.Errorf("file %s is not in conflict state", conflict.FilePath)
		}

		fmt.Printf("Content conflict in %s has been marked for manual resolution\n", conflict.FilePath)
		return nil

	case "Metadata":
		// For metadata conflicts (like file mode changes), take the remote version
		head, err := cr.repo.Head()
		if err != nil {
			return fmt.Errorf("failed to get HEAD reference: %w", err)
		}

		err = worktree.Checkout(&git.CheckoutOptions{
			Hash:  head.Hash(),
			Force: true,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout remote version: %w", err)
		}
		fmt.Printf("Metadata conflict in %s resolved by taking remote version\n", conflict.FilePath)
		return nil

	default:
		return fmt.Errorf("unknown conflict type: %s", conflict.ConflictType)
	}
}

// DetectConflicts detects conflicts during synchronization.
func (cr *ConflictResolver) DetectConflicts() ([]*model.Conflict, error) {
	if cr.repo == nil {
		return nil, fmt.Errorf("no repository set")
	}

	worktree, err := cr.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get the current status of the working directory
	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree status: %w", err)
	}

	var conflicts []*model.Conflict

	// Get repository path for the RepositoryName field
	repoPath := filepath.Base(worktree.Filesystem.Root())

	// Check each file in the status
	for filePath, fileStatus := range status {
		if fileStatus.Staging == 'U' { // 'U' represents unmerged status in go-git
			// This is a content conflict
			conflicts = append(conflicts, &model.Conflict{
				RepositoryName: repoPath,
				FilePath:       filePath,
				ConflictType:   "Content",
			})
		} else if fileStatus.Staging != fileStatus.Worktree {
			// This might indicate a metadata conflict
			conflicts = append(conflicts, &model.Conflict{
				RepositoryName: repoPath,
				FilePath:       filePath,
				ConflictType:   "Metadata",
			})
		}
	}

	return conflicts, nil
}

// SetRepository sets the repository for conflict resolution
func (cr *ConflictResolver) SetRepository(repo *git.Repository) {
	cr.repo = repo
}
