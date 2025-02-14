package sync

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jmendick/gitsync/internal/model"
)

// ConsensusManager handles synchronization consensus between peers
type ConsensusManager struct {
	minNodes    int
	voteTimeout time.Duration
	proposals   map[string]*ConsensusState
	mu          sync.RWMutex
}

type ConsensusState struct {
	Committed bool
	Aborted   bool
	Votes     map[string]bool
}

func NewConsensusManager(minNodes int, voteTimeout time.Duration) *ConsensusManager {
	return &ConsensusManager{
		minNodes:    minNodes,
		voteTimeout: voteTimeout,
		proposals:   make(map[string]*ConsensusState),
	}
}

func (cm *ConsensusManager) StartProposal(ctx context.Context, repoPath, commitHash string, files []string) (*model.SyncProposal, error) {
	proposal := &model.SyncProposal{
		ID:         uuid.New().String(),
		RepoPath:   repoPath,
		CommitHash: commitHash,
		Changes:    files,
		Timestamp:  time.Now(),
	}

	cm.mu.Lock()
	cm.proposals[proposal.ID] = &ConsensusState{
		Votes: make(map[string]bool),
	}
	cm.mu.Unlock()

	return proposal, nil
}

func (cm *ConsensusManager) RecordVote(proposalID, peerID string, accept bool) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	state, exists := cm.proposals[proposalID]
	if !exists {
		return false
	}

	state.Votes[peerID] = accept

	// Check if we have reached consensus
	acceptCount := 0
	for _, vote := range state.Votes {
		if vote {
			acceptCount++
		}
	}

	if acceptCount >= cm.minNodes {
		state.Committed = true
		return true
	}

	return false
}

func (cm *ConsensusManager) GetConsensusState(proposalID string) (*ConsensusState, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	state, exists := cm.proposals[proposalID]
	return state, exists
}

func (cm *ConsensusManager) AbortProposal(proposalID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	state, exists := cm.proposals[proposalID]
	if exists {
		state.Aborted = true
	}
}

func (cm *ConsensusManager) CleanupProposal(proposalID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.proposals, proposalID)
}
