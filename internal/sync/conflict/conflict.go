package conflict

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/jmendick/gitsync/internal/model"
)

// MergeStrategy defines different strategies for resolving conflicts
type MergeStrategy string

const (
	// StrategyTheirs always takes the remote version
	StrategyTheirs MergeStrategy = "theirs"
	// StrategyOurs always keeps the local version
	StrategyOurs MergeStrategy = "ours"
	// StrategyManual requires manual intervention
	StrategyManual MergeStrategy = "manual"
	// StrategyTimeBased takes the most recent version
	StrategyTimeBased MergeStrategy = "time-based"
	// StrategyCustom uses custom resolution rules
	StrategyCustom MergeStrategy = "custom"
)

// ConflictResolver handles synchronization conflicts.
type ConflictResolver struct {
	repo *git.Repository
	// Custom resolution rules for specific file patterns
	customRules []ConflictRule
	// Default strategy when no custom rules match
	defaultStrategy MergeStrategy
	// Conflict prevention settings
	preventionSettings ConflictPreventionSettings
}

// ConflictRule defines a custom rule for conflict resolution
type ConflictRule struct {
	// Pattern is a glob pattern to match file paths
	Pattern string
	// Strategy to use for matched files
	Strategy MergeStrategy
	// CustomResolver is called when Strategy is StrategyCustom
	CustomResolver func(conflict *model.Conflict) error
}

// ConflictPreventionSettings contains settings to prevent conflicts
type ConflictPreventionSettings struct {
	// EnableLocking enables file locking for concurrent edits
	EnableLocking bool
	// LockTimeout is how long a file can be locked
	LockTimeout time.Duration
	// AutoStash automatically stashes local changes before sync
	AutoStash bool
	// PreventOverlappingEdits prevents multiple users from editing the same file
	PreventOverlappingEdits bool
}

// NewConflictResolver creates a new ConflictResolver with default settings.
func NewConflictResolver() *ConflictResolver {
	return &ConflictResolver{
		defaultStrategy: StrategyManual,
		preventionSettings: ConflictPreventionSettings{
			EnableLocking:           true,
			LockTimeout:             15 * time.Minute,
			AutoStash:               true,
			PreventOverlappingEdits: true,
		},
	}
}

// AddRule adds a custom resolution rule
func (cr *ConflictResolver) AddRule(pattern string, strategy MergeStrategy, resolver func(conflict *model.Conflict) error) {
	cr.customRules = append(cr.customRules, ConflictRule{
		Pattern:        pattern,
		Strategy:       strategy,
		CustomResolver: resolver,
	})
}

// SetDefaultStrategy sets the default conflict resolution strategy
func (cr *ConflictResolver) SetDefaultStrategy(strategy MergeStrategy) {
	cr.defaultStrategy = strategy
}

// UpdatePreventionSettings updates conflict prevention settings
func (cr *ConflictResolver) UpdatePreventionSettings(settings ConflictPreventionSettings) {
	cr.preventionSettings = settings
}

// findMatchingRule finds the first rule matching the given file path
func (cr *ConflictResolver) findMatchingRule(filePath string) *ConflictRule {
	for _, rule := range cr.customRules {
		matched, err := filepath.Match(rule.Pattern, filePath)
		if err == nil && matched {
			return &rule
		}
	}
	return nil
}

// ResolveConflict resolves a synchronization conflict using advanced strategies
func (cr *ConflictResolver) ResolveConflict(conflict *model.Conflict) error {
	if conflict == nil {
		return fmt.Errorf("conflict cannot be nil")
	}

	// Get the repository worktree
	worktree, err := cr.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Check for custom rules first
	if rule := cr.findMatchingRule(conflict.FilePath); rule != nil {
		if rule.Strategy == StrategyCustom && rule.CustomResolver != nil {
			return rule.CustomResolver(conflict)
		}
		return cr.applyStrategy(conflict, rule.Strategy, worktree)
	}

	// Apply default strategy
	return cr.applyStrategy(conflict, cr.defaultStrategy, worktree)
}

// applyStrategy implements the actual conflict resolution logic for each strategy
func (cr *ConflictResolver) applyStrategy(conflict *model.Conflict, strategy MergeStrategy, worktree *git.Worktree) error {
	switch strategy {
	case StrategyTheirs:
		return cr.resolveWithTheirs(conflict, worktree)
	case StrategyOurs:
		return cr.resolveWithOurs(conflict, worktree)
	case StrategyTimeBased:
		return cr.resolveWithTimeBasedStrategy(conflict, worktree)
	case StrategyManual:
		return cr.markForManualResolution(conflict)
	default:
		return fmt.Errorf("unknown strategy: %s", strategy)
	}
}

func (cr *ConflictResolver) resolveWithTheirs(conflict *model.Conflict, worktree *git.Worktree) error {
	return worktree.Checkout(&git.CheckoutOptions{
		Hash:  plumbing.NewHash(conflict.TheirCommit),
		Paths: []string{conflict.FilePath},
	})
}

func (cr *ConflictResolver) resolveWithOurs(conflict *model.Conflict, worktree *git.Worktree) error {
	return worktree.Checkout(&git.CheckoutOptions{
		Hash:  plumbing.NewHash(conflict.OurCommit),
		Paths: []string{conflict.FilePath},
	})
}

func (cr *ConflictResolver) resolveWithTimeBasedStrategy(conflict *model.Conflict, worktree *git.Worktree) error {
	ourCommit, err := cr.repo.CommitObject(plumbing.NewHash(conflict.OurCommit))
	if err != nil {
		return err
	}

	theirCommit, err := cr.repo.CommitObject(plumbing.NewHash(conflict.TheirCommit))
	if err != nil {
		return err
	}

	if ourCommit.Committer.When.After(theirCommit.Committer.When) {
		return cr.resolveWithOurs(conflict, worktree)
	}
	return cr.resolveWithTheirs(conflict, worktree)
}

func (cr *ConflictResolver) markForManualResolution(conflict *model.Conflict) error {
	// Keep conflict markers in the file for manual resolution
	fmt.Printf("Manual resolution required for %s\n", conflict.FilePath)
	return nil
}

// DetectConflicts enhances conflict detection with prevention mechanisms
func (cr *ConflictResolver) DetectConflicts() ([]*model.Conflict, error) {
	if cr.repo == nil {
		return nil, fmt.Errorf("no repository set")
	}

	worktree, err := cr.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Apply conflict prevention if enabled
	if cr.preventionSettings.AutoStash {
		if err := cr.stashLocalChanges(worktree); err != nil {
			return nil, err
		}
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree status: %w", err)
	}

	var conflicts []*model.Conflict
	repoPath := filepath.Base(worktree.Filesystem.Root())

	for filePath, fileStatus := range status {
		if fileStatus.Staging == 'U' {
			conflict := &model.Conflict{
				RepositoryName: repoPath,
				FilePath:       filePath,
				ConflictType:   detectConflictType(fileStatus),
			}

			// Check for overlapping edits if enabled
			if cr.preventionSettings.PreventOverlappingEdits {
				if isBeingEdited := cr.checkOverlappingEdits(filePath); isBeingEdited {
					return nil, fmt.Errorf("file %s is currently being edited by another user", filePath)
				}
			}

			conflicts = append(conflicts, conflict)
		}
	}

	return conflicts, nil
}

func (cr *ConflictResolver) stashLocalChanges(worktree *git.Worktree) error {
	status, err := worktree.Status()
	if err != nil {
		return err
	}

	if !status.IsClean() {
		_, err := worktree.Stash(&git.StashOptions{
			Message: "Auto-stash before sync",
		})
		return err
	}
	return nil
}

func detectConflictType(status git.FileStatus) string {
	if status.Worktree == 'U' {
		return "Content"
	}
	if status.Staging != status.Worktree {
		return "Metadata"
	}
	return "Unknown"
}

// SetRepository sets the repository for conflict resolution
func (cr *ConflictResolver) SetRepository(repo *git.Repository) {
	cr.repo = repo
}
