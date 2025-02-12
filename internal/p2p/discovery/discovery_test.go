package discovery

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/testutil"
)

func setupTestDiscoveryService(t *testing.T) (*DiscoveryService, string, func()) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "gitsync-discovery-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create test configuration
	cfg := &config.Config{
		ListenAddress: ":9090",
		RepositoryDir: tempDir,
		Discovery: config.DiscoveryConfig{
			PersistenceEnabled: true,
			StorageDir:         filepath.Join(tempDir, "peer-cache"),
			PeerCacheTime:      1 * time.Hour,
			MaxStoredPeers:     100,
		},
	}

	ds, err := NewDiscoveryService(cfg)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create discovery service: %v", err)
	}

	cleanup := func() {
		ds.Stop()
		os.RemoveAll(tempDir)
	}

	return ds, tempDir, cleanup
}

func TestDiscoveryService(t *testing.T) {
	bootstrapAddr, cleanup := testutil.NewTestServer(t, func(conn net.Conn) {
		testutil.SendTestMessage(conn, "PEER_LIST_RESPONSE", map[string]interface{}{
			"peers": []interface{}{
				map[string]interface{}{
					"id":        "peer1",
					"addresses": []string{"127.0.0.1:8001"},
				},
			},
		})
	})
	defer cleanup()

	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"
	cfg.Config.BootstrapPeers = []string{bootstrapAddr}

	ds, err := NewDiscoveryService(cfg.Config)
	if err != nil {
		t.Fatalf("Failed to create discovery service: %v", err)
	}
	defer ds.Stop()

	peers := ds.DiscoverPeers()
	testutil.AssertEventually(t, func() bool {
		return len(peers) > 0
	}, time.Second)

	testPeer := testutil.CreateTestPeerInfo("test-peer", "127.0.0.1:9000")
	ds.addOrUpdatePeer(testPeer)

	if peer, exists := ds.GetPeerInfo("test-peer"); !exists {
		t.Error("Expected to find added peer")
	} else if peer.ID != testPeer.ID {
		t.Errorf("Got peer ID %s, want %s", peer.ID, testPeer.ID)
	}
}

func TestRoutingTable(t *testing.T) {
	cfg := testutil.NewMockConfig()
	ds, err := NewDiscoveryService(cfg.Config)
	if err != nil {
		t.Fatalf("Failed to create discovery service: %v", err)
	}
	defer ds.Stop()

	testPeers := []*model.PeerInfo{
		testutil.CreateTestPeerInfo("peer1", "192.168.1.1:8080"),
		testutil.CreateTestPeerInfo("peer2", "192.168.1.2:8080"),
		testutil.CreateTestPeerInfo("peer3", "192.168.2.1:8080"),
	}

	for _, peer := range testPeers {
		ds.updateRoutingTable(peer)
	}

	ds.routingTableMu.RLock()
	defer ds.routingTableMu.RUnlock()

	zones := map[string]int{
		"192.168.1": 2,
		"192.168.2": 1,
	}

	for zone, expectedCount := range zones {
		peers, exists := ds.routingTable[zone]
		if !exists {
			t.Errorf("Expected to find peers in zone %s", zone)
		} else if len(peers) != expectedCount {
			t.Errorf("Expected %d peers in zone %s, got %d", expectedCount, zone, len(peers))
		}
	}
}

func TestPeerStateCleanup(t *testing.T) {
	cfg := testutil.NewMockConfig()
	ds, err := NewDiscoveryService(cfg.Config)
	if err != nil {
		t.Fatalf("Failed to create discovery service: %v", err)
	}
	defer ds.Stop()

	activePeer := testutil.CreateTestPeerInfo("active-peer", "127.0.0.1:8001")
	stalePeer := testutil.CreateTestPeerInfo("stale-peer", "127.0.0.1:8002")

	ds.addOrUpdatePeer(activePeer)
	ds.addOrUpdatePeer(stalePeer)

	ds.mu.Lock()
	ds.peers[stalePeer.ID].LastSeen = time.Now().Add(-15 * time.Minute)
	ds.mu.Unlock()

	ds.cleanupStaleHosts()

	testutil.AssertEventually(t, func() bool {
		_, activeExists := ds.GetPeerInfo(activePeer.ID)
		_, staleExists := ds.GetPeerInfo(stalePeer.ID)
		return activeExists && !staleExists
	}, time.Second)
}

func TestHeartbeat(t *testing.T) {
	heartbeatReceived := make(chan struct{})
	addr, cleanup := testutil.NewTestServer(t, func(conn net.Conn) {
		defer close(heartbeatReceived)
		var msg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(conn).Decode(&msg); err != nil {
			t.Errorf("Failed to decode heartbeat: %v", err)
			return
		}
		if msg.Type != "HEARTBEAT" {
			t.Errorf("Expected HEARTBEAT message, got %s", msg.Type)
		}
	})
	defer cleanup()

	cfg := testutil.NewMockConfig()
	ds, err := NewDiscoveryService(cfg.Config)
	if err != nil {
		t.Fatalf("Failed to create discovery service: %v", err)
	}
	defer ds.Stop()

	testPeer := testutil.CreateTestPeerInfo("test-peer", addr)
	ds.addOrUpdatePeer(testPeer)

	err = ds.sendHeartbeat(addr)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}

	testutil.AssertEventually(t, func() bool {
		select {
		case <-heartbeatReceived:
			return true
		default:
			return false
		}
	}, time.Second)
}

func TestPeerPersistence(t *testing.T) {
	ds, _, cleanup := setupTestDiscoveryService(t)
	defer cleanup()

	// Add test peers
	testPeers := []*model.PeerInfo{
		{
			ID:        "peer1",
			Addresses: []string{"192.168.1.1:8080"},
		},
		{
			ID:        "peer2",
			Addresses: []string{"192.168.1.2:8080"},
		},
	}

	// Add peers to discovery service
	for _, peer := range testPeers {
		ds.addOrUpdatePeer(peer)
	}

	// Save peer cache
	if err := ds.savePeerCache(); err != nil {
		t.Fatalf("Failed to save peer cache: %v", err)
	}

	// Create new discovery service instance to test loading
	ds2, err := NewDiscoveryService(ds.config)
	if err != nil {
		t.Fatalf("Failed to create second discovery service: %v", err)
	}
	defer ds2.Stop()

	// Verify loaded peers
	for _, expectedPeer := range testPeers {
		peer, exists := ds2.GetPeerInfo(expectedPeer.ID)
		if !exists {
			t.Errorf("Expected peer %s not found in loaded cache", expectedPeer.ID)
			continue
		}
		if peer.Addresses[0] != expectedPeer.Addresses[0] {
			t.Errorf("Got peer address %s, want %s", peer.Addresses[0], expectedPeer.Addresses[0])
		}
	}
}

func TestPeerStatistics(t *testing.T) {
	ds, _, cleanup := setupTestDiscoveryService(t)
	defer cleanup()

	testPeer := &model.PeerInfo{
		ID:        "test-peer",
		Addresses: []string{"192.168.1.1:8080"},
	}
	ds.addOrUpdatePeer(testPeer)

	// Update statistics
	ds.UpdatePeerStatistics(testPeer.ID, true, 100*time.Millisecond)
	ds.UpdatePeerStatistics(testPeer.ID, true, 150*time.Millisecond)
	ds.UpdatePeerStatistics(testPeer.ID, false, 200*time.Millisecond)

	// Get statistics
	stats, err := ds.GetPeerStatistics(testPeer.ID)
	if err != nil {
		t.Fatalf("Failed to get peer statistics: %v", err)
	}

	// Verify statistics
	if stats.SuccessfulSyncs != 2 {
		t.Errorf("Got %d successful syncs, want 2", stats.SuccessfulSyncs)
	}
	if stats.FailedSyncs != 1 {
		t.Errorf("Got %d failed syncs, want 1", stats.FailedSyncs)
	}
	if stats.ReliabilityScore != 0.6666666666666666 {
		t.Errorf("Got reliability score %f, want approximately 0.67", stats.ReliabilityScore)
	}
}

func TestRoutingTablePersistence(t *testing.T) {
	ds, _, cleanup := setupTestDiscoveryService(t)
	defer cleanup()

	// Add peers in different zones
	testPeers := []*model.PeerInfo{
		{
			ID:        "peer1",
			Addresses: []string{"192.168.1.1:8080"},
		},
		{
			ID:        "peer2",
			Addresses: []string{"192.168.1.2:8080"},
		},
		{
			ID:        "peer3",
			Addresses: []string{"192.168.2.1:8080"},
		},
	}

	for _, peer := range testPeers {
		ds.addOrUpdatePeer(peer)
		ds.updateRoutingTable(peer)
	}

	// Save state
	if err := ds.savePeerCache(); err != nil {
		t.Fatalf("Failed to save peer cache: %v", err)
	}

	// Load state in new instance
	ds2, err := NewDiscoveryService(ds.config)
	if err != nil {
		t.Fatalf("Failed to create second discovery service: %v", err)
	}
	defer ds2.Stop()

	// Verify routing table
	ds2.routingTableMu.RLock()
	defer ds2.routingTableMu.RUnlock()

	expectedZones := map[string]int{
		"192.168.1": 2,
		"192.168.2": 1,
	}

	for zone, expectedCount := range expectedZones {
		peers := ds2.routingTable[zone]
		if len(peers) != expectedCount {
			t.Errorf("Zone %s: got %d peers, want %d", zone, len(peers), expectedCount)
		}
	}
}

func TestPeerCacheExpiration(t *testing.T) {
	// Create config with short cache time
	tempDir, err := os.MkdirTemp("", "gitsync-discovery-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		ListenAddress: ":9090",
		RepositoryDir: tempDir,
		Discovery: config.DiscoveryConfig{
			PersistenceEnabled: true,
			StorageDir:         filepath.Join(tempDir, "peer-cache"),
			PeerCacheTime:      50 * time.Millisecond, // Short duration for testing
			MaxStoredPeers:     100,
		},
	}

	ds, err := NewDiscoveryService(cfg)
	if err != nil {
		t.Fatalf("Failed to create discovery service: %v", err)
	}
	defer ds.Stop()

	// Add a test peer
	testPeer := &model.PeerInfo{
		ID:        "test-peer",
		Addresses: []string{"192.168.1.1:8080"},
	}
	ds.addOrUpdatePeer(testPeer)

	// Save the cache
	if err := ds.savePeerCache(); err != nil {
		t.Fatalf("Failed to save peer cache: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(100 * time.Millisecond)

	// Create new instance and verify peer is not loaded
	ds2, err := NewDiscoveryService(cfg)
	if err != nil {
		t.Fatalf("Failed to create second discovery service: %v", err)
	}
	defer ds2.Stop()

	if _, exists := ds2.GetPeerInfo(testPeer.ID); exists {
		t.Error("Expected peer to be expired from cache but it was loaded")
	}
}
