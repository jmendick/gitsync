package discovery

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/testutil"
)

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
