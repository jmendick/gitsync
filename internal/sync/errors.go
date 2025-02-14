package sync

import (
	"fmt"
	"strings"
)

// SyncError represents a synchronization error
type SyncError struct {
	operation   string
	repository  string
	peer        string
	err         error
	recoverable bool
}

func NewSyncError(operation, repository, peer string, err error, recoverable bool) *SyncError {
	return &SyncError{
		operation:   operation,
		repository:  repository,
		peer:        peer,
		err:         err,
		recoverable: recoverable,
	}
}

func (e *SyncError) Error() string {
	return fmt.Sprintf("sync error in %s for repo %s with peer %s: %v",
		e.operation, e.repository, e.peer, e.err)
}

func (e *SyncError) IsRecoverable() bool {
	return e.recoverable
}

func (e *SyncError) Unwrap() error {
	return e.err
}

// IsFatalError determines if an error is fatal to the sync process
func IsFatalError(err error) bool {
	if err == nil {
		return false
	}

	// Consider authentication, permission, and corruption errors as fatal
	errStr := err.Error()
	fatalPatterns := []string{
		"authentication failed",
		"permission denied",
		"repository corrupted",
		"invalid object",
		"access denied",
	}

	for _, pattern := range fatalPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// IsRecoverableError determines if an error can be recovered from
func IsRecoverableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	recoverablePatterns := []string{
		"connection refused",
		"timeout",
		"temporary failure",
		"no such host",
		"network is unreachable",
		"connection reset",
	}

	for _, pattern := range recoverablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// Constants for message types
const (
	MessageTypeFileChunk        = "file_chunk"
	MessageTypeTransferComplete = "transfer_complete"
	MessageTypeConflictProposal = "conflict_proposal"
	MessageTypeConflictVote     = "conflict_vote"
	MessageTypeSyncProposal     = "sync_proposal"
	MessageTypeSyncVote         = "sync_vote"
)
