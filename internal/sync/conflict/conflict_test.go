package conflict

import (
	"context"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/jmendick/gitsync/internal/auth"
	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock ConsensusManager for testing
type MockConsensusManager struct {
	mock.Mock
}

func (m *MockConsensusManager) StartProposal(ctx context.Context, proposal *SyncProposal) (bool, error) {
	args := m.Called(ctx, proposal)
	return args.Bool(0), args.Error(1)
}

// Mock GitRepositoryManager for testing
type MockGitRepositoryManager struct {
	mock.Mock
}

func (m *MockGitRepositoryManager) GetConflictVersions(repo *gogit.Repository, path string) ([]git.ConflictVersion, error) {
	args := m.Called(repo, path)
	return args.Get(0).([]git.ConflictVersion), args.Error(1)
}

func (m *MockGitRepositoryManager) OpenRepository(repoPath string) (*gogit.Repository, error) {
	args := m.Called(repoPath)
	return args.Get(0).(*gogit.Repository), args.Error(1)
}

func (m *MockGitRepositoryManager) CloneRepository(ctx context.Context, repoName string, remoteURL string) (*gogit.Repository, error) {
	args := m.Called(ctx, repoName, remoteURL)
	return args.Get(0).(*gogit.Repository), args.Error(1)
}

func (m *MockGitRepositoryManager) AddRemote(repo *gogit.Repository, remoteName string, urls []string) error {
	args := m.Called(repo, remoteName, urls)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) ListRemotes(repo *gogit.Repository) ([]*config.RemoteConfig, error) {
	args := m.Called(repo)
	return args.Get(0).([]*config.RemoteConfig), args.Error(1)
}

func (m *MockGitRepositoryManager) ListBranches(repo *gogit.Repository) ([]string, error) {
	args := m.Called(repo)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockGitRepositoryManager) CreateBranch(repo *gogit.Repository, branchName string, startPoint *plumbing.Reference) error {
	args := m.Called(repo, branchName, startPoint)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) DeleteBranch(repo *gogit.Repository, branchName string) error {
	args := m.Called(repo, branchName)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) SyncAllBranches(ctx context.Context, repo *gogit.Repository, strategy git.MergeStrategy) error {
	args := m.Called(ctx, repo, strategy)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) FetchRepository(ctx context.Context, repo *gogit.Repository) error {
	args := m.Called(ctx, repo)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) GetHeadReference(repo *gogit.Repository) (*plumbing.Reference, error) {
	args := m.Called(repo)
	return args.Get(0).(*plumbing.Reference), args.Error(1)
}

func (m *MockGitRepositoryManager) Commit(repo *gogit.Repository, message, authorName, authorEmail string) (plumbing.Hash, error) {
	args := m.Called(repo, message, authorName, authorEmail)
	return args.Get(0).(plumbing.Hash), args.Error(1)
}

func (m *MockGitRepositoryManager) Push(repo *gogit.Repository) error {
	args := m.Called(repo)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) Diff(repo *gogit.Repository) (string, error) {
	args := m.Called(repo)
	return args.String(0), args.Error(1)
}

func (m *MockGitRepositoryManager) GetRepoMetadata(ctx context.Context, user *auth.User, owner, repo string) (*git.RepositoryMetadata, error) {
	args := m.Called(ctx, user, owner, repo)
	return args.Get(0).(*git.RepositoryMetadata), args.Error(1)
}

func (m *MockGitRepositoryManager) Clone(ctx context.Context, user *auth.User, owner, repo string) error {
	args := m.Called(ctx, user, owner, repo)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) CheckAccess(user *auth.User, owner, repo string) (bool, error) {
	args := m.Called(user, owner, repo)
	return args.Bool(0), args.Error(1)
}

func (m *MockGitRepositoryManager) GetRepositoryList() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *MockGitRepositoryManager) GetChangesSince(repo *gogit.Repository, commitHash string) ([]string, error) {
	args := m.Called(repo, commitHash)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockGitRepositoryManager) GetFilteredChanges(repo *gogit.Repository, includes, excludes []string) ([]string, error) {
	args := m.Called(repo, includes, excludes)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockGitRepositoryManager) ResolveConflict(repo *gogit.Repository, path string, strategy string) error {
	args := m.Called(repo, path, strategy)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) SyncBranch(ctx context.Context, repo *gogit.Repository, branchName string, strategy git.MergeStrategy) error {
	args := m.Called(ctx, repo, branchName, strategy)
	return args.Error(0)
}

func (m *MockGitRepositoryManager) ResetToClean(repo *gogit.Repository) error {
	args := m.Called(repo)
	return args.Error(0)
}

func NewMockGitRepositoryManager() *MockGitRepositoryManager {
	return &MockGitRepositoryManager{}
}

func TestConflictDetection(t *testing.T) {
	repo, cleanup := testutil.SetupTestRepo(t)
	defer cleanup()

	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	wrapper := NewGitRepoWrapper(repo, "test-repo")
	resolver := NewConflictResolver(gitMgr, consensusMgr)
	resolver.SetRepository(wrapper)

	// Create test branches with conflicts
	main := createTestCommit(t, repo, "main.go", "package main // main branch")
	feature := createTestCommit(t, repo, "main.go", "package main // feature branch")

	conflicts, err := resolver.DetectConflicts(repo, main, feature)
	assert.NoError(t, err)
	assert.NotEmpty(t, conflicts)
}

func TestSmartConflictResolution(t *testing.T) {
	repo, cleanup := testutil.SetupTestRepo(t)
	defer cleanup()

	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	wrapper := NewGitRepoWrapper(repo, "test-repo")
	resolver := NewConflictResolver(gitMgr, consensusMgr)
	resolver.SetRepository(wrapper)

	testCases := []struct {
		name          string
		filePath      string
		content       string
		strategy      ConflictResolutionStrategy
		expectedError bool
	}{
		{
			name:     "generated file conflict",
			filePath: "generated.pb.go",
			content:  "package test",
			strategy: StrategySmartMerge,
		},
		{
			name:     "dependency file conflict",
			filePath: "go.sum",
			content:  "test/module v1.0.0",
			strategy: StrategySmartMerge,
		},
		{
			name:     "take ours strategy",
			filePath: "main.go",
			content:  "package main",
			strategy: StrategyOurs,
		},
		{
			name:     "take theirs strategy",
			filePath: "util.go",
			content:  "package util",
			strategy: StrategyTheirs,
		},
		{
			name:          "manual resolution required",
			filePath:      "complex.go",
			content:       "package complex",
			strategy:      StrategyManual,
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test file with conflict
			conflict := &model.Conflict{
				FilePath:     tc.filePath,
				ConflictType: "content",
			}

			// Set up mock versions
			ours := createTestCommitWithTime(t, repo, tc.filePath, tc.content+" ours", time.Now().Add(-1*time.Hour))
			theirs := createTestCommitWithTime(t, repo, tc.filePath, tc.content+" theirs", time.Now())

			mockVersions := make([]git.ConflictVersion, 0, 2)
			mockVersions = append(mockVersions, git.ConflictVersion{
				PeerID:    "ours",
				Timestamp: time.Now().Add(-1 * time.Hour),
				Hash:      ours.Hash().String(),
				Content:   []byte(tc.content + " ours"),
			})
			mockVersions = append(mockVersions, git.ConflictVersion{
				PeerID:    "theirs",
				Timestamp: time.Now(),
				Hash:      theirs.Hash().String(),
				Content:   []byte(tc.content + " theirs"),
			})

			gitMgr.On("GetConflictVersions", repo, tc.filePath).Return(mockVersions, nil)
			gitMgr.On("ResolveConflict", repo, tc.filePath, string(tc.strategy)).Return(nil)

			// Test resolution
			err := resolver.ResolveConflict(repo, conflict, tc.strategy)
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			gitMgr.AssertExpectations(t)
		})
	}
}

func TestConflictMetadataGathering(t *testing.T) {
	repo, cleanup := testutil.SetupTestRepo(t)
	defer cleanup()

	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	// Create test commits with different timestamps
	oursTime := time.Now().Add(-1 * time.Hour)
	theirsTime := time.Now()

	testCases := []struct {
		name         string
		filePath     string
		content      string
		expectedType string
		isGenerated  bool
		isDependency bool
	}{
		{
			name:         "generated file",
			filePath:     "api.pb.go",
			content:      "package api",
			expectedType: "generated",
			isGenerated:  true,
		},
		{
			name:         "dependency file",
			filePath:     "go.sum",
			content:      "module v1.0.0",
			expectedType: "dependency",
			isDependency: true,
		},
		{
			name:         "go source file",
			filePath:     "main.go",
			content:      "package main",
			expectedType: "go",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create commits and set up mock versions
			ours := createTestCommitWithTime(t, repo, tc.filePath, tc.content+" ours", oursTime)
			theirs := createTestCommitWithTime(t, repo, tc.filePath, tc.content+" theirs", theirsTime)

			mockVersions := []git.ConflictVersion{
				{
					PeerID:    "ours",
					Timestamp: oursTime,
					Hash:      ours.Hash().String(),
					Content:   []byte(tc.content + " ours"),
				},
				{
					PeerID:    "theirs",
					Timestamp: theirsTime,
					Hash:      theirs.Hash().String(),
					Content:   []byte(tc.content + " theirs"),
				},
			}
			gitMgr.On("GetConflictVersions", repo, tc.filePath).Return(mockVersions, nil)

			metadata, err := resolver.gatherConflictMetadata(repo, tc.filePath, ours, theirs)
			assert.NoError(t, err)
			assert.NotNil(t, metadata)

			assert.Equal(t, tc.filePath, metadata.FilePath)
			fileType := determineFileType(tc.filePath)
			assert.Equal(t, tc.expectedType, fileType)

			gitMgr.AssertExpectations(t)
		})
	}
}

func TestConsensusBasedResolution(t *testing.T) {
	repo, cleanup := testutil.SetupTestRepo(t)
	defer cleanup()

	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	// Set up test scenario
	filePath := "shared.go"
	conflict := &model.Conflict{
		FilePath:     filePath,
		ConflictType: "content",
	}

	// Create test commits and mock versions
	ours := createTestCommit(t, repo, filePath, "package shared // ours")
	theirs := createTestCommit(t, repo, filePath, "package shared // theirs")

	mockVersions := []git.ConflictVersion{
		{
			PeerID:    "ours",
			Timestamp: time.Now(),
			Hash:      ours.Hash().String(),
			Content:   []byte("package shared // ours"),
		},
		{
			PeerID:    "theirs",
			Timestamp: time.Now(),
			Hash:      theirs.Hash().String(),
			Content:   []byte("package shared // theirs"),
		},
	}
	gitMgr.On("GetConflictVersions", repo, filePath).Return(mockVersions, nil)

	// Set up consensus manager expectations
	consensusMgr.On("StartProposal", mock.Anything, mock.Anything).Return(true, nil)

	// Test initiating consensus
	ctx := context.Background()
	state, err := resolver.initiateVoting(ctx, conflict)
	assert.NoError(t, err)
	assert.NotNil(t, state)
	assert.Equal(t, "voting", state.Status)

	// Test voting
	vote1 := &ConflictVote{
		PeerID:     "peer1",
		Resolution: string(StrategyOurs),
		Timestamp:  time.Now(),
	}
	vote2 := &ConflictVote{
		PeerID:     "peer2",
		Resolution: string(StrategyOurs),
		Timestamp:  time.Now(),
	}

	err = resolver.ReceiveVote(state.ID, vote1)
	assert.NoError(t, err)
	err = resolver.ReceiveVote(state.ID, vote2)
	assert.NoError(t, err)

	// Verify consensus
	updatedState, exists := resolver.GetConflictState(state.ID)
	assert.True(t, exists)
	assert.True(t, updatedState.Resolved)
	assert.Equal(t, string(StrategyOurs), updatedState.Resolution)

	gitMgr.AssertExpectations(t)
	consensusMgr.AssertExpectations(t)
}

func TestOverlappingEditPrevention(t *testing.T) {
	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	// Enable prevention settings
	resolver.UpdatePreventionSettings(ConflictPreventionSettings{
		EnableLocking:           true,
		LockTimeout:             1 * time.Second,
		PreventOverlappingEdits: true,
	})

	filePath := "test.go"

	// First edit should succeed
	hasOverlap := resolver.checkOverlappingEdits(filePath)
	assert.False(t, hasOverlap)

	// Second edit should detect overlap
	hasOverlap = resolver.checkOverlappingEdits(filePath)
	assert.True(t, hasOverlap)

	// Wait for lock timeout
	time.Sleep(2 * time.Second)

	// Should allow edit after timeout
	hasOverlap = resolver.checkOverlappingEdits(filePath)
	assert.False(t, hasOverlap)
}

func TestCustomResolutionRules(t *testing.T) {
	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	// Add custom rules
	resolver.AddRule("*.pb.go", StrategyTheirs, nil)
	resolver.AddRule("*.sum", StrategyOurs, nil)
	resolver.AddRule("*.custom", StrategyManual, func(conflict *model.Conflict) error {
		return nil
	})

	testCases := []struct {
		name          string
		filePath      string
		expectedRule  ConflictResolutionStrategy
		shouldBeFound bool
	}{
		{
			name:          "generated proto file",
			filePath:      "api.pb.go",
			expectedRule:  StrategyTheirs,
			shouldBeFound: true,
		},
		{
			name:          "dependency file",
			filePath:      "go.sum",
			expectedRule:  StrategyOurs,
			shouldBeFound: true,
		},
		{
			name:          "custom file",
			filePath:      "data.custom",
			expectedRule:  StrategyManual,
			shouldBeFound: true,
		},
		{
			name:          "no rule match",
			filePath:      "main.go",
			shouldBeFound: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rule := resolver.findMatchingRule(tc.filePath)
			if tc.shouldBeFound {
				assert.NotNil(t, rule)
				assert.Equal(t, tc.expectedRule, rule.Strategy)
			} else {
				assert.Nil(t, rule)
			}
		})
	}
}

func TestFileLockingMechanism(t *testing.T) {
	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	// Test file locking
	filePath := "test.go"

	// First lock should succeed
	assert.False(t, resolver.checkOverlappingEdits(filePath))

	// Add an edit
	resolver.activeEdits.edits[filePath] = time.Now()

	// Second lock should fail
	assert.True(t, resolver.checkOverlappingEdits(filePath))

	// Wait for timeout
	resolver.activeEdits.edits[filePath] = time.Now().Add(-2 * resolver.preventionSettings.LockTimeout)

	// Lock should succeed after timeout
	assert.False(t, resolver.checkOverlappingEdits(filePath))
}

func TestConflictResolutionStrategies(t *testing.T) {
	repo, cleanup := testutil.SetupTestRepo(t)
	defer cleanup()

	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	testCases := []struct {
		name          string
		strategy      ConflictResolutionStrategy
		expectedError bool
	}{
		{
			name:          "ours strategy",
			strategy:      StrategyOurs,
			expectedError: false,
		},
		{
			name:          "theirs strategy",
			strategy:      StrategyTheirs,
			expectedError: false,
		},
		{
			name:          "union strategy",
			strategy:      StrategyUnion,
			expectedError: true, // Not implemented yet
		},
		{
			name:          "smart merge strategy",
			strategy:      StrategySmartMerge,
			expectedError: true, // Requires metadata
		},
		{
			name:          "interactive strategy",
			strategy:      StrategyInteractive,
			expectedError: true, // Requires UI
		},
	}

	conflict := &model.Conflict{
		FilePath:     "test.go",
		ConflictType: "content",
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := resolver.ResolveConflict(repo, conflict, tc.strategy)
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConflictStateManagement(t *testing.T) {
	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	// Test conflict state creation and retrieval
	conflict := &model.Conflict{
		FilePath: "test.go",
	}

	ctx := context.Background()
	state, err := resolver.initiateVoting(ctx, conflict)
	assert.NoError(t, err)
	assert.NotNil(t, state)

	// Test state retrieval
	retrieved, exists := resolver.GetConflictState(state.ID)
	assert.True(t, exists)
	assert.Equal(t, state.ID, retrieved.ID)

	// Test conflict cancellation
	err = resolver.CancelConflict(state.ID)
	assert.NoError(t, err)

	// Verify conflict was removed
	_, exists = resolver.GetConflictState(state.ID)
	assert.False(t, exists)
}

func TestVotingMechanism(t *testing.T) {
	consensusMgr := &MockConsensusManager{}
	gitMgr := NewMockGitRepositoryManager()
	resolver := NewConflictResolver(gitMgr, consensusMgr)

	// Create test conflict state
	state := &ConflictState{
		ID:        "test-conflict",
		FilePath:  "test.go",
		Votes:     make(map[string]*ConflictVote),
		StartTime: time.Now(),
		Status:    "voting",
	}

	resolver.activeConflicts[state.ID] = state

	// Test voting
	votes := []struct {
		peerID     string
		resolution string
	}{
		{"peer1", string(StrategyOurs)},
		{"peer2", string(StrategyOurs)},
		{"peer3", string(StrategyTheirs)},
	}

	for _, v := range votes {
		vote := &ConflictVote{
			PeerID:     v.peerID,
			Resolution: v.resolution,
			Timestamp:  time.Now(),
		}
		err := resolver.ReceiveVote(state.ID, vote)
		assert.NoError(t, err)
	}

	// Verify majority resolution was selected
	assert.True(t, state.Resolved)
	assert.Equal(t, string(StrategyOurs), state.Resolution)
}

// Helper functions
func createTestCommit(t *testing.T, repo *gogit.Repository, filePath, content string) *plumbing.Reference {
	return createTestCommitWithTime(t, repo, filePath, content, time.Now())
}

func createTestCommitWithTime(t *testing.T, repo *gogit.Repository, filePath, content string, timestamp time.Time) *plumbing.Reference {
	w, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Failed to get worktree: %v", err)
	}

	err = testutil.WriteFile(w.Filesystem, filePath, content)
	if err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	_, err = w.Add(filePath)
	if err != nil {
		t.Fatalf("Failed to stage file: %v", err)
	}

	_, err = w.Commit("test commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@example.com",
			When:  timestamp,
		},
	})
	if err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Failed to get HEAD: %v", err)
	}

	return head
}
