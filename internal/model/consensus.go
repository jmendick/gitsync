package model

import (
	"time"
)

// ConsensusState represents the current state of consensus
type ConsensusState struct {
	ProposalID   string                 `json:"proposal_id"`
	CurrentRound int                    `json:"current_round"`
	Votes        map[string]bool        `json:"votes"`
	StartTime    time.Time              `json:"start_time"`
	Deadline     time.Time              `json:"deadline"`
	Committed    bool                   `json:"committed"`
	Aborted      bool                   `json:"aborted"`
	Participants []string               `json:"participants"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// SyncProposal represents a proposed sync operation
type SyncProposal struct {
	ID           string    `json:"id"`
	RepoPath     string    `json:"repo_path"`
	CommitHash   string    `json:"commit_hash"`
	Changes      []string  `json:"changes"`
	Dependencies []string  `json:"dependencies,omitempty"`
	BatchSize    int64     `json:"batch_size"`
	Timestamp    time.Time `json:"timestamp"`
	ProposerID   string    `json:"proposer_id"`
}

// SyncVote represents a peer's vote on a sync proposal
type SyncVote struct {
	ProposalID string                 `json:"proposal_id"`
	VoterID    string                 `json:"voter_id"`
	Accept     bool                   `json:"accept"`
	Reason     string                 `json:"reason,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// SyncCommit represents a committed sync operation
type SyncCommit struct {
	ProposalID   string        `json:"proposal_id"`
	CommitHash   string        `json:"commit_hash"`
	Participants []string      `json:"participants"`
	Changes      []string      `json:"changes"`
	Timestamp    time.Time     `json:"timestamp"`
	BatchResults []BatchResult `json:"batch_results,omitempty"`
}

// BatchResult represents the result of a sync batch
type BatchResult struct {
	PeerID     string `json:"peer_id"`
	BatchIndex int    `json:"batch_index"`
	Success    bool   `json:"success"`
	BytesSent  int64  `json:"bytes_sent"`
	Duration   string `json:"duration"`
}

// ConsensusMessage represents a message in the consensus protocol
type ConsensusMessage struct {
	Type      ConsensusMessageType `json:"type"`
	Timestamp time.Time            `json:"timestamp"`
	SenderID  string               `json:"sender_id"`
	Round     int                  `json:"round"`
	Payload   interface{}          `json:"payload"`
}

type ConsensusMessageType string

const (
	ProposeSync ConsensusMessageType = "PROPOSE_SYNC"
	VoteSync    ConsensusMessageType = "VOTE_SYNC"
	CommitSync  ConsensusMessageType = "COMMIT_SYNC"
	AbortSync   ConsensusMessageType = "ABORT_SYNC"
)
