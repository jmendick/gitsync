package sync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/auth"
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
	authStore        auth.UserStore
	cancelMu         sync.Mutex
	cancel           context.CancelFunc
}

// NewSyncManager creates a new SyncManager.
func NewSyncManager(cfg *config.Config, node *p2p.Node, authStore auth.UserStore) *SyncManager {
	gitMgr := git.NewGitRepositoryManager(cfg.GetRepositoryDir(), authStore)

	conflictResolver := conflict.NewConflictResolver()

	return &SyncManager{
		config:           cfg,
		p2pNode:          node,
		gitManager:       gitMgr,
		conflictResolver: conflictResolver,
		authStore:        authStore,
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

	repository, err := sm.gitManager.OpenRepository(fmt.Sprintf("%s/%s", owner, repo))
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Use user's GitHub token for git operations
	if err := sm.gitManager.FetchRepository(ctx, repository); err != nil {
		return fmt.Errorf("failed to fetch updates: %w", err)
	}

	return nil
}

// startPeriodicSync starts the periodic sync process for all repositories
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
	// Get list of all managed repositories
	repos := sm.gitManager.GetRepositoryList()
	for _, repoPath := range repos {
		parts := strings.Split(repoPath, "/")
		if len(parts) != 2 {
			fmt.Printf("Invalid repository path format: %s\n", repoPath)
			continue
		}
		owner, repoName := parts[0], parts[1]

		// For each repository, try to use its owner's credentials
		if user, err := sm.authStore.GetUser(owner); err == nil && user.IsGitHubUser() {
			if err := sm.SyncRepository(ctx, user, owner, repoName); err != nil {
				// Log error but continue with other repos
				fmt.Printf("Failed to sync repository %s/%s: %v\n", owner, repoName, err)
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
