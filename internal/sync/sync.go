package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/jmendick/gitsync/internal/auth"
	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/p2p"
	"github.com/jmendick/gitsync/internal/state"
)

// SyncManager manages the synchronization process.
type SyncManager struct {
	config           *config.Config
	p2pNode          *p2p.Node
	gitManager       GitManager
	authStore        auth.UserStore
	stateManager     StateManager
	cancelMu         sync.Mutex
	cancel           context.CancelFunc
	maxRetries       int
	progressChan     chan *SyncProgress
	batchCreator     *BatchCreator
	journal          *SyncJournal
	conflictManager  *ConflictManager
	consensusManager *ConsensusManager
	logger           *log.Logger
}

// NewSyncManager creates a new SyncManager.
func NewSyncManager(cfg *config.Config, node *p2p.Node, authStore auth.UserStore) (*SyncManager, error) {
	gitMgr := git.NewGitRepositoryManager(cfg.GetRepositoryDir(), authStore)
	stateManager, err := state.NewManager(cfg.GetRepositoryDir())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state manager: %w", err)
	}

	journal := NewSyncJournal(cfg.GetRepositoryDir())
	if err := journal.Load(); err != nil {
		return nil, fmt.Errorf("failed to load sync journal: %w", err)
	}

	conflictManager := NewConflictManager(cfg.GetRepositoryDir(), nil) // TODO: Add conflict store
	consensusManager := NewConsensusManager(
		cfg.GetSync().MinSuccessfulPeers,
		cfg.GetSync().ConflictVoteTimeout,
	)

	batchCreator := NewBatchCreator(64*1024, 5*1024*1024)

	return &SyncManager{
		config:           cfg,
		p2pNode:          node,
		gitManager:       gitMgr,
		authStore:        authStore,
		stateManager:     stateManager,
		maxRetries:       cfg.GetSync().MaxRetries,
		progressChan:     make(chan *SyncProgress, 100),
		batchCreator:     batchCreator,
		journal:          journal,
		conflictManager:  conflictManager,
		consensusManager: consensusManager,
		logger:           log.New(os.Stdout, "[SYNC] ", log.LstdFlags),
	}, nil
}

// Start starts the synchronization manager and background sync processes.
func (sm *SyncManager) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	sm.cancelMu.Lock()
	sm.cancel = cancel
	sm.cancelMu.Unlock()

	go sm.startPeriodicSync(ctx)
	return nil
}

// Stop gracefully stops the sync manager and its background processes.
func (sm *SyncManager) Stop() error {
	sm.cancelMu.Lock()
	if sm.cancel != nil {
		sm.cancel()
	}
	sm.cancelMu.Unlock()

	if err := sm.journal.Save(); err != nil {
		return fmt.Errorf("failed to save sync journal: %w", err)
	}
	return nil
}

// SyncRepository synchronizes a specific repository
func (sm *SyncManager) SyncRepository(ctx context.Context, user *auth.User, owner, repo string) error {
	// Verify access permissions
	hasAccess, err := sm.gitManager.CheckAccess(user, owner, repo)
	if err != nil {
		return fmt.Errorf("failed to check repository access: %w", err)
	}
	if !hasAccess {
		return fmt.Errorf("access denied to repository %s/%s", owner, repo)
	}

	repository, err := sm.gitManager.OpenRepository(filepath.Join(owner, repo))
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Discover peers
	peers := sm.p2pNode.GetPeerDiscovery().DiscoverPeers()
	if len(peers) == 0 {
		return fmt.Errorf("no peers found for repository %s/%s", owner, repo)
	}

	// Convert PeerInfo to peer IDs
	peerIDs := make([]string, len(peers))
	for i, peer := range peers {
		peerIDs[i] = peer.ID
	}

	// Start sync process
	return sm.syncWithPeers(ctx, repository, peerIDs, filepath.Join(owner, repo))
}

func (sm *SyncManager) startPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(sm.config.Sync.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.syncAll(ctx)
		}
	}
}

func (sm *SyncManager) syncAll(ctx context.Context) {
	repos := sm.gitManager.GetRepositoryList()
	for _, repoPath := range repos {
		parts := filepath.SplitList(repoPath)
		if len(parts) != 2 {
			sm.logger.Printf("Invalid repository path format: %s", repoPath)
			continue
		}
		owner, repoName := parts[0], parts[1]

		if user, err := sm.authStore.GetUser(owner); err == nil {
			if err := sm.SyncRepository(ctx, user, owner, repoName); err != nil {
				sm.logger.Printf("Failed to sync repository %s/%s: %v", owner, repoName, err)
			}
		}
	}
}

// syncWithPeers synchronizes repository state with available peers
func (sm *SyncManager) syncWithPeers(ctx context.Context, repository *gogit.Repository, peers []string, repoPath string) error {
	var successCount int
	var lastError error

	for _, peerID := range peers {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Get last sync point for this peer
			lastSyncPoint, err := sm.stateManager.GetLastSyncPoint(repoPath, peerID)
			if err != nil {
				sm.logger.Printf("Failed to get last sync point for peer %s: %v", peerID, err)
				continue
			}

			// Get changes since last sync
			changes, err := sm.gitManager.GetChangesSince(repository, lastSyncPoint)
			if err != nil {
				sm.logger.Printf("Failed to get changes for peer %s: %v", peerID, err)
				continue
			}

			// Create batches for synchronization - using len(peers) to distribute files across peers
			batches := sm.batchCreator.CreateBatches(changes, len(peers))

			// Sync each batch
			for _, batchFiles := range batches {
				err = sm.syncFiles(ctx, repository, peerID, batchFiles)
				if err != nil {
					lastError = err
					sm.logger.Printf("Failed to sync files with peer %s: %v", peerID, err)
					break
				}
			}

			if err == nil {
				successCount++
				// Update sync point on successful sync
				headRef, err := repository.Head()
				if err == nil {
					sm.stateManager.UpdateSyncPoint(repoPath, peerID, headRef.Hash().String(), true)
				}
			}
		}
	}

	minRequired := sm.config.GetSync().MinSuccessfulPeers
	if successCount < minRequired {
		return fmt.Errorf("failed to sync with minimum required peers (got %d, need %d): %v",
			successCount, minRequired, lastError)
	}

	return nil
}

// syncFiles synchronizes a batch of files with a peer
func (sm *SyncManager) syncFiles(ctx context.Context, repository *gogit.Repository, peerID string, files []string) error {
	// For each file in the batch
	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Send the file to the peer
			if err := sm.p2pNode.SendFile(ctx, peerID, file); err != nil {
				return fmt.Errorf("failed to sync file %s with peer %s: %w", file, peerID, err)
			}
		}
	}
	return nil
}

// Required interfaces
type GitManager interface {
	OpenRepository(path string) (*gogit.Repository, error)
	GetRepositoryList() []string
	CheckAccess(user *auth.User, owner, repo string) (bool, error)
	GetChangesSince(repo *gogit.Repository, since string) ([]string, error)
}

type StateManager interface {
	GetLastSyncPoint(repoPath, peerID string) (string, error)
	UpdateSyncPoint(repoPath, peerID, commitHash string, success bool) error
}
