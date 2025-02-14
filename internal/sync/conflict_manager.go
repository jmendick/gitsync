package sync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/google/uuid"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
)

// Conflict represents a Git merge conflict
type Conflict struct {
	FilePath  string
	BaseHash  string
	OurHash   string
	TheirHash string
}

// ConflictSession tracks an active conflict resolution
type ConflictSession struct {
	ConflictID string
	RepoPath   string
	FilePath   string
	Proposal   *protocol.ConflictProposal
	Votes      map[string]*protocol.ConflictVote
	StartTime  time.Time
	Deadline   time.Time
	mu         sync.RWMutex
}

// ConflictManager handles merge conflicts and their resolution
type ConflictManager struct {
	baseDir         string
	activeConflicts map[string]*ConflictSession
	conflictStore   ConflictStore
	conflictVotes   chan *protocol.ConflictVote
	mu              sync.RWMutex
}

func NewConflictManager(baseDir string, store ConflictStore) *ConflictManager {
	return &ConflictManager{
		baseDir:         baseDir,
		activeConflicts: make(map[string]*ConflictSession),
		conflictStore:   store,
		conflictVotes:   make(chan *protocol.ConflictVote, 100),
	}
}

func getRepoPath(repo *git.Repository) (string, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}
	return wt.Filesystem.Root(), nil
}

func (cm *ConflictManager) HandleConflict(ctx context.Context, repo *git.Repository, conflict *Conflict, peers []*model.PeerInfo, defaultStrategy string) error {
	repoPath, err := getRepoPath(repo)
	if err != nil {
		return fmt.Errorf("failed to get repository path: %w", err)
	}

	// Create conflict proposal
	proposal := &protocol.ConflictProposal{
		ConflictID: uuid.New().String(),
		RepoPath:   repoPath,
		FilePath:   conflict.FilePath,
		Timestamp:  time.Now(),
		Strategy:   defaultStrategy,
		Changes:    make([]protocol.ConflictChange, 0),
	}

	session := &ConflictSession{
		ConflictID: proposal.ConflictID,
		RepoPath:   repoPath,
		FilePath:   conflict.FilePath,
		Proposal:   proposal,
		Votes:      make(map[string]*protocol.ConflictVote),
		StartTime:  time.Now(),
		Deadline:   time.Now().Add(30 * time.Second),
	}

	// Get conflict versions
	versions, err := cm.getConflictVersions(repo, conflict.FilePath)
	if err != nil {
		return fmt.Errorf("failed to get conflict versions: %w", err)
	}

	for _, v := range versions {
		proposal.Changes = append(proposal.Changes, protocol.ConflictChange{
			PeerID:    v.PeerID,
			Timestamp: v.Timestamp,
			Hash:      v.Hash,
			Content:   v.Content,
		})
	}

	cm.mu.Lock()
	cm.activeConflicts[proposal.ConflictID] = session
	cm.mu.Unlock()

	defer func() {
		cm.mu.Lock()
		delete(cm.activeConflicts, proposal.ConflictID)
		cm.mu.Unlock()
	}()

	requiredVotes := int(math.Ceil(float64(len(peers)) * 0.51))
	votingCtx, cancel := context.WithDeadline(ctx, session.Deadline)
	defer cancel()

	acceptCount := 0
	for {
		select {
		case vote := <-cm.conflictVotes:
			if vote.ConflictID == proposal.ConflictID {
				session.mu.Lock()
				session.Votes[vote.PeerID] = vote
				if vote.Accept {
					acceptCount++
				}
				session.mu.Unlock()

				if acceptCount >= requiredVotes {
					return cm.applyConflictResolution(repo, conflict, proposal)
				}
			}

		case <-votingCtx.Done():
			session.mu.RLock()
			finalAcceptCount := 0
			for _, v := range session.Votes {
				if v.Accept {
					finalAcceptCount++
				}
			}
			session.mu.RUnlock()

			if finalAcceptCount >= requiredVotes {
				return cm.applyConflictResolution(repo, conflict, proposal)
			}
			return fmt.Errorf("conflict resolution voting timed out without consensus")
		}
	}
}

// getConflictVersions retrieves different versions of a file in conflict
func (cm *ConflictManager) getConflictVersions(repo *git.Repository, filePath string) ([]ConflictVersion, error) {
	repoPath, err := getRepoPath(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository path: %w", err)
	}

	versions := make([]ConflictVersion, 0)

	// Get base version
	baseContent, err := os.ReadFile(filepath.Join(repoPath, filePath))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read base version: %w", err)
	}

	// Get our version
	ourContent, err := os.ReadFile(filepath.Join(repoPath, filePath))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read our version: %w", err)
	}

	// Get their version
	theirContent, err := os.ReadFile(filepath.Join(repoPath, filePath+".merge_head"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read their version: %w", err)
	}

	// Add base version if it exists
	if baseContent != nil {
		versions = append(versions, ConflictVersion{
			PeerID:    "base",
			Timestamp: time.Now(),
			Hash:      calculateHash(baseContent),
			Content:   baseContent,
		})
	}

	// Add our version if it exists
	if ourContent != nil {
		versions = append(versions, ConflictVersion{
			PeerID:    "ours",
			Timestamp: time.Now(),
			Hash:      calculateHash(ourContent),
			Content:   ourContent,
		})
	}

	// Add their version if it exists
	if theirContent != nil {
		versions = append(versions, ConflictVersion{
			PeerID:    "theirs",
			Timestamp: time.Now(),
			Hash:      calculateHash(theirContent),
			Content:   theirContent,
		})
	}

	return versions, nil
}

// applyConflictResolution applies the resolved conflict to the repository
func (cm *ConflictManager) applyConflictResolution(repo *git.Repository, conflict *Conflict, proposal *protocol.ConflictProposal) error {
	repoPath, err := getRepoPath(repo)
	if err != nil {
		return fmt.Errorf("failed to get repository path: %w", err)
	}

	filePath := filepath.Join(repoPath, conflict.FilePath)

	// Apply the chosen strategy
	switch proposal.Strategy {
	case "union":
		driver := &UnionMergeDriver{
			BasePath: repoPath,
			FilePath: conflict.FilePath,
		}
		return driver.Merge()
	case "ours":
		// Keep our version, no action needed
		return nil
	case "theirs":
		// Use their version
		theirPath := filePath + ".merge_head"
		content, err := os.ReadFile(theirPath)
		if err != nil {
			return fmt.Errorf("failed to read their version: %w", err)
		}
		if err = os.WriteFile(filePath, content, 0644); err != nil {
			return fmt.Errorf("failed to write resolved file: %w", err)
		}
		return os.Remove(theirPath)
	default:
		return fmt.Errorf("unsupported conflict resolution strategy: %s", proposal.Strategy)
	}
}

// UnionMergeDriver implements a union merge strategy
type UnionMergeDriver struct {
	BasePath string
	FilePath string
}

func (d *UnionMergeDriver) Merge() error {
	oursPath := filepath.Join(d.BasePath, d.FilePath)
	theirsPath := filepath.Join(d.BasePath, d.FilePath+".merge_head")

	ours, err := os.ReadFile(oursPath)
	if err != nil {
		return err
	}

	theirs, err := os.ReadFile(theirsPath)
	if err != nil {
		return err
	}

	merged := unionMerge(string(ours), string(theirs))
	return os.WriteFile(oursPath, []byte(merged), 0644)
}

func unionMerge(ours, theirs string) string {
	// Implementation remains the same...
	return "" // TODO: Implement
}

type ConflictVersion struct {
	PeerID    string
	Timestamp time.Time
	Hash      string
	Content   []byte
}

type ConflictEntry struct {
	RepoPath   string
	FilePath   string
	Status     string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

type ConflictProposal struct {
	Strategy string
	Changes  []ConflictChange
}

type ConflictChange struct {
	Content   []byte
	Timestamp time.Time
	PeerID    string
	Hash      string
}

// calculateHash generates a hash for file content
func calculateHash(content []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(content))
}
