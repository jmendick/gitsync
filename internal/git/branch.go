package git

import (
	"context"
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

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
		// Try to create the branch if it doesn't exist
		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branchName),
			Create: true,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout/create branch %s: %w", branchName, err)
		}
	}

	// Get the remote branch reference
	remoteBranch := plumbing.NewRemoteReferenceName("origin", branchName)
	remoteRef, err := repo.Reference(remoteBranch, true)
	if err != nil {
		// Remote branch doesn't exist yet, nothing to sync
		return nil
	}

	// Apply merge strategy
	switch strategy {
	case MergeStrategyOurs:
		// For "ours" strategy, we keep our changes
		return nil
	case MergeStrategyTheirs:
		// For "theirs" strategy, we take their version
		err = worktree.Reset(&git.ResetOptions{
			Commit: remoteRef.Hash(),
			Mode:   git.HardReset,
		})
	default:
		// Default to standard merge
		err = worktree.Pull(&git.PullOptions{
			RemoteName:    "origin",
			SingleBranch:  true,
			ReferenceName: plumbing.NewBranchReferenceName(branchName),
		})
	}

	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to sync changes: %w", err)
	}

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
