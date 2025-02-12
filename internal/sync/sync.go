package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/p2p"
	"github.com/jmendick/gitsync/internal/sync/conflict"
)

// SyncManager manages the synchronization process.
type SyncManager struct {
	config           *config.Config
	p2pNode          *p2p.Node
	gitManager       *git.GitRepositoryManager
	conflictResolver *conflict.ConflictResolver
	cancelMu         sync.Mutex
	cancel           context.CancelFunc
}

// NewSyncManager creates a new SyncManager.
func NewSyncManager(cfg *config.Config, node *p2p.Node) *SyncManager {
	gitMgr, err := git.NewGitRepositoryManager(cfg.GetRepositoryDir())
	if err != nil {
		fmt.Printf("Error initializing Git Repository Manager: %v\n", err) // Or handle error more gracefully
		return nil                                                         // or panic, depending on error handling strategy
	}

	conflictResolver := conflict.NewConflictResolver() // Initialize conflict resolver

	return &SyncManager{
		config:           cfg,
		p2pNode:          node,
		gitManager:       gitMgr,
		conflictResolver: conflictResolver,
	}
}

// Start starts the synchronization manager and background sync processes.
func (sm *SyncManager) Start() error {
	fmt.Println("Starting Sync Manager...")
	ctx, cancel := context.WithCancel(context.Background())

	sm.cancelMu.Lock()
	sm.cancel = cancel
	sm.cancelMu.Unlock()

	go sm.startPeriodicSync(ctx) // Example: Start periodic sync in background
	return nil
}

// Stop gracefully stops the sync manager and its background processes.
func (sm *SyncManager) Stop() error {
	fmt.Println("Stopping Sync Manager...")
	sm.cancelMu.Lock()
	if sm.cancel != nil {
		sm.cancel()
	}
	sm.cancelMu.Unlock()
	return nil
}

func (sm *SyncManager) startPeriodicSync(ctx context.Context) {
	fmt.Println("Starting Periodic Synchronization...")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Stopping periodic synchronization...")
			return
		case <-ticker.C:
			if err := sm.SynchronizeRepositories(); err != nil {
				fmt.Printf("Error during periodic sync: %v\n", err)
			}
		}
	}
}

// SynchronizeRepositories synchronizes all managed repositories.
func (sm *SyncManager) SynchronizeRepositories() error {
	fmt.Println("Synchronizing Repositories...")
	// TODO: Implement logic to iterate through managed repositories and synchronize them
	// For each repository:
	// 1. Discover peers sharing the repository
	// 2. Select peers to synchronize with
	// 3. Initiate synchronization process with selected peers (using p2pNode and protocol)
	// 4. Handle conflicts if they arise (using conflictResolver)
	// Example (placeholder):
	repoName := "test-repo" // Example repo name
	repo, err := sm.gitManager.OpenRepository(repoName)
	if err != nil {
		fmt.Printf("Error opening repository '%s': %v\n", repoName, err)
		return err
	}

	err = sm.gitManager.FetchRepository(context.Background(), repo)
	if err != nil {
		fmt.Printf("Error fetching repository '%s': %v\n", repoName, err)
		return err
	}

	// Get list of peers for this repo (placeholder - get from config or discovery later)
	peers := sm.p2pNode.GetPeerDiscovery().DiscoverPeers() // Get discovered peers for now

	if len(peers) > 0 {
		peerToSync := peers[0] // Sync with the first discovered peer for now
		err = sm.p2pNode.SendSyncRequest(context.Background(), peerToSync, repoName)
		if err != nil {
			fmt.Printf("Error sending sync request to peer '%s' for repo '%s': %v\n", peerToSync.ID, repoName, err)
			return err
		}
	} else {
		fmt.Println("No peers found for repository:", repoName)
	}

	fmt.Println("Repository synchronization completed (placeholder).")
	return nil
}

// ... (Add more sync related functions like AddRepository, RemoveRepository, etc.) ...
