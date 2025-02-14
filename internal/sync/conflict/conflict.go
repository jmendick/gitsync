package conflict

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/uuid"
	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/model"
)

// ConflictResolutionStrategy defines how to handle conflicts
type ConflictResolutionStrategy string

const (
	StrategyManual      ConflictResolutionStrategy = "manual"
	StrategyOurs        ConflictResolutionStrategy = "ours"
	StrategyTheirs      ConflictResolutionStrategy = "theirs"
	StrategyUnion       ConflictResolutionStrategy = "union"
	StrategyInteractive ConflictResolutionStrategy = "interactive"
	StrategySmartMerge  ConflictResolutionStrategy = "smart-merge"
)

// ConflictMetadata contains additional information about a conflict
type ConflictMetadata struct {
	LastModifiedOurs   time.Time `json:"last_modified_ours"`
	LastModifiedTheirs time.Time `json:"last_modified_theirs"`
	FileType           string    `json:"file_type"`
	LineCount          int       `json:"line_count"`
	ConflictSize       int       `json:"conflict_size"`
	AuthorOurs         string    `json:"author_ours"`
	AuthorTheirs       string    `json:"author_theirs"`
	FilePath           string    `json:"file_path"`
	RepoRef            string    `json:"repo_ref"`
	OurRef             string    `json:"our_ref"`
	TheirRef           string    `json:"their_ref"`
}

// activeEdits tracks files being edited to prevent overlaps
type activeEdits struct {
	sync.RWMutex
	edits map[string]time.Time // Changed from *time.Time
}

// Repository path helpers
type GitRepository interface {
	GetPath() string
	GetWorkdir() string
}

// ConsensusManager interface defines consensus operations
type ConsensusManager interface {
	StartProposal(ctx context.Context, proposal *SyncProposal) (bool, error)
}

// SyncProposal represents a proposal for synchronization
type SyncProposal struct {
	ID      string
	RepoRef string
	Changes []string
}

// GitManager defines the interface for Git operations needed by ConflictResolver
type GitManager interface {
	GetConflictVersions(repo *gogit.Repository, path string) ([]git.ConflictVersion, error)
	ResolveConflict(repo *gogit.Repository, path string, strategy string) error
	GetHeadReference(repo *gogit.Repository) (*plumbing.Reference, error)
	ResetToClean(repo *gogit.Repository) error
}

// ConflictResolver handles distributed conflict resolution
type ConflictResolver struct {
	consensusManager ConsensusManager
	gitManager       GitManager // Change from *git.GitRepositoryManager to GitManager
	activeConflicts  map[string]*ConflictState
	mu               sync.RWMutex
	repo             GitRepository
	// Custom resolution rules for specific file patterns
	customRules []ConflictRule
	// Default strategy when no custom rules match
	defaultStrategy ConflictResolutionStrategy
	// Conflict prevention settings
	preventionSettings ConflictPreventionSettings
	// Track active edits
	activeEdits activeEdits
	// Custom merge tools
	customMergeTools map[string]string
	// Metadata cache
	metadataCache map[string]*ConflictMetadata
	// Resolved conflicts
	resolvedConflicts map[string]ConflictResolutionStrategy
	// Store interface for persisting conflicts
	store Store // Change from ConflictStore to Store
}

// ConflictState represents the state of an active conflict
type ConflictState struct {
	ID          string
	FilePath    string
	RepoPath    string
	ConflictRef string
	Versions    []git.ConflictVersion
	Votes       map[string]*ConflictVote
	Resolution  string
	StartTime   time.Time
	Deadline    time.Time
	Resolved    bool
	Status      string
}

// ConflictVote represents a vote for a conflict resolution
type ConflictVote struct {
	PeerID     string                 `json:"peer_id"`
	Resolution string                 `json:"resolution"`
	Timestamp  time.Time              `json:"timestamp"`
	Reason     string                 `json:"reason"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// ConflictRule defines a custom rule for conflict resolution
type ConflictRule struct {
	// Pattern is a glob pattern to match file paths
	Pattern string
	// Strategy to use for matched files
	Strategy ConflictResolutionStrategy
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

// NewConflictResolver creates a new ConflictResolver instance
func NewConflictResolver(gitMgr GitManager, consensusMgr ConsensusManager) *ConflictResolver {
	return &ConflictResolver{
		consensusManager: consensusMgr,
		gitManager:       gitMgr,
		activeConflicts:  make(map[string]*ConflictState),
		defaultStrategy:  StrategyManual,
		preventionSettings: ConflictPreventionSettings{
			EnableLocking:           true,
			LockTimeout:             15 * time.Minute,
			AutoStash:               true,
			PreventOverlappingEdits: true,
		},
		activeEdits: activeEdits{
			edits: make(map[string]time.Time),
		},
		customMergeTools:  make(map[string]string),
		metadataCache:     make(map[string]*ConflictMetadata),
		resolvedConflicts: make(map[string]ConflictResolutionStrategy),
	}
}

// AddRule adds a custom resolution rule
func (cr *ConflictResolver) AddRule(pattern string, strategy ConflictResolutionStrategy, resolver func(conflict *model.Conflict) error) {
	cr.customRules = append(cr.customRules, ConflictRule{
		Pattern:        pattern,
		Strategy:       strategy,
		CustomResolver: resolver,
	})
}

// SetDefaultStrategy sets the default conflict resolution strategy
func (cr *ConflictResolver) SetDefaultStrategy(strategy ConflictResolutionStrategy) {
	cr.defaultStrategy = strategy
}

// UpdatePreventionSettings updates conflict prevention settings
func (cr *ConflictResolver) UpdatePreventionSettings(settings ConflictPreventionSettings) {
	cr.preventionSettings = settings
}

// FindMatchingRule finds the first rule matching the given file path
func (cr *ConflictResolver) findMatchingRule(filePath string) *ConflictRule {
	for _, rule := range cr.customRules {
		matched, err := filepath.Match(rule.Pattern, filePath)
		if err == nil && matched {
			return &rule
		}
	}
	return nil
}

// DetectConflicts enhances conflict detection with prevention mechanisms
func (cr *ConflictResolver) DetectConflicts(repo *gogit.Repository, ours, theirs *plumbing.Reference) ([]*model.Conflict, error) {
	conflicts := make([]*model.Conflict, 0)

	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Perform merge using correct go-git API
	err = worktree.Pull(&gogit.PullOptions{
		RemoteName:    "origin",
		ReferenceName: theirs.Name(),
	})

	if err != nil && err != gogit.NoErrAlreadyUpToDate {
		status, err := worktree.Status()
		if err != nil {
			return nil, fmt.Errorf("failed to get status: %w", err)
		}

		// Collect conflicts
		for filePath, fileStatus := range status {
			if fileStatus.Staging == gogit.Modified {
				conflict := &model.Conflict{
					FilePath: filePath,
				}

				// Gather metadata for smart resolution
				metadata, err := cr.gatherConflictMetadata(repo, filePath, ours, theirs)
				if err != nil {
					fmt.Printf("Warning: Failed to gather metadata for %s: %v\n", filePath, err)
				} else {
					cr.metadataCache[filePath] = metadata
				}

				conflicts = append(conflicts, conflict)
			}
		}

		// Reset merge state
		err = worktree.Reset(&gogit.ResetOptions{
			Mode: gogit.HardReset,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to reset after conflict detection: %w", err)
		}
	}

	return conflicts, nil
}

// ResolveConflict resolves a specific conflict using the specified strategy
func (cr *ConflictResolver) ResolveConflict(repo *gogit.Repository, conflict *model.Conflict, strategy ConflictResolutionStrategy) error {
	if strategy == "" {
		strategy = cr.defaultStrategy
	}

	switch strategy {
	case StrategyOurs, StrategyTheirs:
		return cr.resolveWithSide(repo, conflict, strategy == StrategyOurs)

	case StrategyUnion:
		return cr.resolveWithUnion(repo, conflict)

	case StrategySmartMerge:
		return cr.resolveWithSmartMerge(repo, conflict)

	case StrategyInteractive:
		return fmt.Errorf("interactive resolution must be handled by the user interface")

	default:
		return fmt.Errorf("unhandled resolution strategy: %s", strategy)
	}
}

func (cr *ConflictResolver) resolveWithSide(repo *gogit.Repository, conflict *model.Conflict, useOurs bool) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	// Get the appropriate hash based on which side we're using
	var hash plumbing.Hash
	if useOurs {
		ref, err := repo.Head()
		if err != nil {
			return err
		}
		hash = ref.Hash()
	} else {
		ref, err := repo.Reference("MERGE_HEAD", true)
		if err != nil {
			return err
		}
		hash = ref.Hash()
	}

	// Checkout the desired version
	err = worktree.Checkout(&gogit.CheckoutOptions{
		Hash:  hash,
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("failed to checkout version: %w", err)
	}

	// Stage the resolved file
	_, err = worktree.Add(conflict.FilePath)
	return err
}

func (cr *ConflictResolver) resolveWithUnion(repo *gogit.Repository, conflict *model.Conflict) error {
	// Combine both versions, keeping all changes
	// TODO: Implement union merge strategy
	return fmt.Errorf("union strategy not yet implemented")
}

func (cr *ConflictResolver) resolveWithSmartMerge(repo *gogit.Repository, conflict *model.Conflict) error {
	metadata, exists := cr.metadataCache[conflict.FilePath]
	if !exists {
		return fmt.Errorf("no metadata available for smart merge")
	}

	// Apply smart merge rules based on metadata
	switch {
	case metadata.FileType == "generated":
		// For generated files, always take the newer version
		useOurs := metadata.LastModifiedOurs.After(metadata.LastModifiedTheirs)
		return cr.resolveWithSide(repo, conflict, useOurs)

	case strings.HasSuffix(conflict.FilePath, ".lock") ||
		strings.HasSuffix(conflict.FilePath, ".sum") ||
		strings.HasSuffix(conflict.FilePath, ".mod"):
		// For dependency files, prefer the version with more entries
		useOurs := metadata.LineCount >= metadata.ConflictSize
		return cr.resolveWithSide(repo, conflict, useOurs)

	default:
		// For other files, fall back to manual resolution
		return fmt.Errorf("smart merge requires manual intervention for %s", conflict.FilePath)
	}
}

func (cr *ConflictResolver) gatherConflictMetadata(repo *gogit.Repository, filePath string, ours, theirs *plumbing.Reference) (*ConflictMetadata, error) {
	metadata := &ConflictMetadata{
		FilePath: filePath,
		RepoRef:  cr.getRepoRef(),
	}

	// Get reference details
	if ours != nil {
		metadata.OurRef = ours.Name().String()
	}
	if theirs != nil {
		metadata.TheirRef = theirs.Name().String()
	}

	return metadata, nil
}

func determineFileType(filePath string) string {
	ext := filepath.Ext(filePath)
	switch {
	case isGeneratedFile(filePath):
		return "generated"
	case ext == ".go":
		return "go"
	case ext == ".mod", ext == ".sum":
		return "dependency"
	default:
		return "unknown"
	}
}

func isGeneratedFile(filePath string) bool {
	patterns := []string{
		".pb.go",
		".gen.go",
		"generated",
		"auto-generated",
	}

	for _, pattern := range patterns {
		if strings.Contains(filePath, pattern) {
			return true
		}
	}

	return false
}

// checkOverlappingEdits checks if a file is currently being edited
func (cr *ConflictResolver) checkOverlappingEdits(filePath string) bool {
	cr.activeEdits.RLock()
	defer cr.activeEdits.RUnlock()

	lastEdit, exists := cr.activeEdits.edits[filePath]
	if !exists {
		return false
	}

	// Check if the edit has expired
	if time.Since(lastEdit) > cr.preventionSettings.LockTimeout {
		delete(cr.activeEdits.edits, filePath)
		return false
	}

	return true
}

// SetRepository sets the repository for conflict resolution
func (cr *ConflictResolver) SetRepository(repo GitRepository) {
	cr.repo = repo
}

// HandleConflict handles a conflict by initiating a consensus-based resolution process
func (cr *ConflictResolver) HandleConflict(ctx context.Context, repo *gogit.Repository, path string) error {
	// Create conflict state
	conflictID := uuid.New().String()

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	state := &ConflictState{
		ID:        conflictID,
		FilePath:  path,
		RepoPath:  wt.Filesystem.Root(),
		Votes:     make(map[string]*ConflictVote),
		StartTime: time.Now(),
		Deadline:  time.Now().Add(5 * time.Minute),
	}

	// Get conflict versions
	versions, err := cr.gitManager.GetConflictVersions(repo, path)
	if err != nil {
		return fmt.Errorf("failed to get conflict versions: %w", err)
	}
	state.Versions = versions

	// Store conflict state
	cr.mu.Lock()
	cr.activeConflicts[conflictID] = state
	cr.mu.Unlock()

	// Create consensus proposal
	proposal := &SyncProposal{
		ID:      conflictID,
		RepoRef: cr.getRepoRef(),
		Changes: []string{path},
	}

	// Start consensus process
	if _, err := cr.consensusManager.StartProposal(ctx, proposal); err != nil {
		return fmt.Errorf("failed to start conflict resolution consensus: %w", err)
	}

	// Monitor resolution
	go cr.monitorResolution(ctx, conflictID)

	return nil
}

// ReceiveVote receives a vote for a conflict resolution
func (cr *ConflictResolver) ReceiveVote(conflictID string, vote *ConflictVote) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	state, exists := cr.activeConflicts[conflictID]
	if !exists {
		return fmt.Errorf("unknown conflict: %s", conflictID)
	}

	state.Votes[vote.PeerID] = vote

	// Check if we have consensus
	resolutionCounts := make(map[string]int)
	for _, v := range state.Votes {
		resolutionCounts[v.Resolution]++
	}

	// Find majority resolution
	var majorityResolution string
	maxVotes := 0
	for res, count := range resolutionCounts {
		if count > maxVotes {
			maxVotes = count
			majorityResolution = res
		}
	}

	// If we have clear majority
	if maxVotes > len(state.Votes)/2 {
		state.Resolution = majorityResolution
		state.Resolved = true
	}

	return nil
}

// monitorResolution monitors the resolution of a conflict
func (cr *ConflictResolver) monitorResolution(ctx context.Context, conflictID string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			cr.mu.Lock()
			state, exists := cr.activeConflicts[conflictID]
			if !exists {
				cr.mu.Unlock()
				return
			}

			if state.Resolved || time.Now().After(state.Deadline) {
				// Remove from active conflicts
				delete(cr.activeConflicts, conflictID)
				cr.mu.Unlock()
				return
			}
			cr.mu.Unlock()
		}
	}
}

// GetConflictState gets the state of a conflict
func (cr *ConflictResolver) GetConflictState(conflictID string) (*ConflictState, bool) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	state, exists := cr.activeConflicts[conflictID]
	return state, exists
}

// CancelConflict cancels a conflict
func (cr *ConflictResolver) CancelConflict(conflictID string) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if _, exists := cr.activeConflicts[conflictID]; !exists {
		return fmt.Errorf("unknown conflict: %s", conflictID)
	}

	delete(cr.activeConflicts, conflictID)
	return nil
}

func (cr *ConflictResolver) initiateVoting(ctx context.Context, conflict *model.Conflict) (*ConflictState, error) {
	state := &ConflictState{
		ID:          uuid.New().String(),
		FilePath:    conflict.FilePath,
		ConflictRef: cr.getRepoRef(),
		Votes:       make(map[string]*ConflictVote),
		StartTime:   time.Now(),
		Status:      "voting",
	}

	proposal := &SyncProposal{
		ID:      state.ID,
		RepoRef: cr.getRepoRef(),
		Changes: []string{conflict.FilePath},
	}

	if _, err := cr.consensusManager.StartProposal(ctx, proposal); err != nil {
		return nil, fmt.Errorf("failed to start conflict resolution consensus: %w", err)
	}

	cr.mu.Lock()
	cr.activeConflicts[state.ID] = state
	cr.mu.Unlock()

	return state, nil
}

// Helper method to get repository reference
func (cr *ConflictResolver) getRepoRef() string {
	if r, ok := cr.repo.(GitRepository); ok {
		return r.GetPath()
	}
	// Fallback to working directory if repository path not available
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return wd
}

// ConflictStore is moved to a separate interface file to avoid redeclaration
// Store represents persistent storage for conflicts
type Store interface {
	StoreConflict(id string, conflict *git.Conflict) error
	GetConflict(id string) (*git.Conflict, error)
	ListConflicts() ([]*git.Conflict, error)
}

func (cr *ConflictResolver) storeForManualResolution(conflict *model.Conflict) error {
	state := &ConflictState{
		ID:          uuid.New().String(),
		FilePath:    conflict.FilePath,
		ConflictRef: cr.repo.GetPath(),
		Status:      "pending",
		StartTime:   time.Now(),
	}

	if cr.store == nil {
		return fmt.Errorf("no conflict store configured")
	}

	// Convert to git.Conflict for storage
	gitConflict := &git.Conflict{
		ID:       state.ID,
		FilePath: state.FilePath,
	}
	return cr.store.StoreConflict(state.ID, gitConflict)
}

// Wrapper for go-git repository to implement GitRepository
type gitRepoWrapper struct {
	*gogit.Repository
	path string
}

func NewGitRepoWrapper(repo *gogit.Repository, path string) GitRepository {
	return &gitRepoWrapper{
		Repository: repo,
		path:       path,
	}
}

func (w *gitRepoWrapper) GetPath() string {
	return w.path
}

func (w *gitRepoWrapper) GetWorkdir() string {
	wt, err := w.Repository.Worktree()
	if err != nil {
		return ""
	}
	return wt.Filesystem.Root()
}
