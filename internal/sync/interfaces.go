package sync

import (
	"context"
	"time"

	"github.com/jmendick/gitsync/internal/model"
)

// ConflictStore defines the interface for storing and retrieving conflict information
type ConflictStore interface {
	SaveConflict(repoPath string, conflict *ConflictEntry) error
	GetConflict(repoPath, filePath string) (*ConflictEntry, error)
	ListUnresolvedConflicts(repoPath string) ([]*ConflictEntry, error)
	MarkResolved(repoPath, filePath string) error
}

// SyncProgressReporter defines the interface for reporting sync progress
type SyncProgressReporter interface {
	ReportProgress(progress *SyncProgress)
	GetCurrentProgress() *SyncProgress
}

// BatchManager defines the interface for managing file batches
type BatchManager interface {
	CreateBatches(files []string, peerCount int) [][]string
	UpdateMetrics(originalSize, compressedSize int64, duration time.Duration)
	GetBatchSize() int64
}

// ConsensusProtocol defines the interface for managing sync consensus
type ConsensusProtocol interface {
	StartProposal(ctx context.Context, repoPath, commitHash string, files []string) (*model.SyncProposal, error)
	RecordVote(proposalID, peerID string, accept bool) bool
	GetConsensusState(proposalID string) (*ConsensusState, bool)
	AbortProposal(proposalID string)
}

// ConflictResolver defines the interface for resolving conflicts
type ConflictResolver interface {
	HandleConflict(ctx context.Context, repo interface{}, conflict interface{}, peers []interface{}, defaultStrategy string) error
	GetMergeDriver(baseDir, filePath string) interface{}
}

// JournalManager defines the interface for managing sync journal
type JournalManager interface {
	CreateEntry(repoPath string, files []string) *SyncJournalEntry
	UpdateProgress(entryID string, batch []string, failed bool)
	SetCurrentBatch(entryID string, batch []string, offset int)
	GetIncompleteEntries() []*SyncJournalEntry
	Save() error
	Load() error
}

// These interfaces define the contracts between different components of the sync system.
// Each component can be tested and implemented independently as long as it satisfies
// its corresponding interface.
