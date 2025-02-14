package p2p

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/p2p/bandwidth"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
	"github.com/jmendick/gitsync/internal/testutil"
)

func TestNodeCreation(t *testing.T) {
	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"

	bwManager := bandwidth.NewManager(1024 * 1024) // 1MB/s limit
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	if node.listener == nil {
		t.Error("Node listener should not be nil")
	}

	if node.peerDiscovery == nil {
		t.Error("Node peer discovery service should not be nil")
	}
}

func TestNodePeerConnection(t *testing.T) {
	connectionReceived := make(chan struct{})
	peerAddr, cleanup := testutil.NewTestServer(t, func(conn net.Conn) {
		defer close(connectionReceived)
		// Handle incoming sync request
		testutil.SendTestMessage(conn, string(protocol.SyncResponse), map[string]interface{}{
			"success": true,
			"message": "Test response",
		})
	})
	defer cleanup()

	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"
	cfg.Config.BootstrapPeers = []string{peerAddr}

	bwManager := bandwidth.NewManager(1024 * 1024)
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	peerInfo := testutil.CreateTestPeerInfo("test-peer", peerAddr)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = node.SendSyncRequest(ctx, peerInfo, "test-repo")
	if err != nil {
		t.Fatalf("Failed to send sync request: %v", err)
	}

	select {
	case <-connectionReceived:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Connection was not received within timeout")
	}
}

func TestNodeDiscovery(t *testing.T) {
	connectionAttempted := make(chan struct{})
	peerAddr, cleanup := testutil.NewTestServer(t, func(conn net.Conn) {
		defer close(connectionAttempted)
		// Simulate peer list response
		testutil.SendTestMessage(conn, string(protocol.PeerListResponse), map[string]interface{}{
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
	cfg.Config.BootstrapPeers = []string{peerAddr}

	bwManager := bandwidth.NewManager(1024 * 1024)
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	discovery := node.GetPeerDiscovery()
	if discovery == nil {
		t.Fatal("Peer discovery service should not be nil")
	}

	select {
	case <-connectionAttempted:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Discovery connection was not attempted within timeout")
	}
}

func TestNodeShutdown(t *testing.T) {
	cfg := testutil.NewMockConfig()
	bwManager := bandwidth.NewManager(1024 * 1024)
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	addr := node.listener.Addr().String()

	if err := node.Close(); err != nil {
		t.Fatalf("Failed to close node: %v", err)
	}

	// Check that the listener is actually closed
	_, err = net.Dial("tcp", addr)
	if err == nil {
		t.Fatal("Expected connection to fail after node shutdown")
	}
}

func TestConnectionQualityMonitoring(t *testing.T) {
	// Setup test server that responds to pings
	peerAddr, cleanup := testutil.NewTestServer(t, func(conn net.Conn) {
		// Echo server for ping messages
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	})
	defer cleanup()

	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"
	cfg.Config.BootstrapPeers = []string{peerAddr}

	bwManager := bandwidth.NewManager(1024 * 1024)
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	// Add peer and simulate some transfers
	peer := testutil.CreateTestPeerInfo("test-peer", peerAddr)
	node.peerDiscovery.UpdatePeerStatistics(peer.ID, true, 100*time.Millisecond)

	// Simulate successful transfers to generate quality metrics
	for i := 0; i < 3; i++ {
		err := node.SendSyncRequest(context.Background(), peer, "test-repo")
		if err != nil {
			t.Logf("Sync request %d failed (expected): %v", i, err)
		}
		node.peerDiscovery.UpdatePeerStatistics(peer.ID, true, 100*time.Millisecond)
	}

	quality := node.GetConnectionQuality("test-peer")
	if quality == nil {
		t.Fatal("Expected connection quality metrics to be available")
	}

	// Verify quality metrics are within reasonable bounds
	if quality.Latency <= 0 {
		t.Error("Expected positive latency")
	}
	if quality.PacketLoss < 0 || quality.PacketLoss > 1 {
		t.Error("Packet loss should be between 0 and 1")
	}
	if quality.Bandwidth <= 0 {
		t.Error("Expected positive bandwidth measurement")
	}
}

func TestFileTransfer(t *testing.T) {
	// Create a temporary test file
	testFile := filepath.Join(t.TempDir(), "test.txt")
	testContent := []byte("test file content")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Setup test server to receive file
	received := make(chan []byte, 1)
	peerAddr, cleanup := testutil.NewTestServer(t, func(conn net.Conn) {
		data, err := io.ReadAll(conn)
		if err != nil {
			t.Errorf("Failed to read file data: %v", err)
			return
		}
		received <- data
	})
	defer cleanup()

	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"
	bwManager := bandwidth.NewManager(1024 * 1024)
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	// Add the test peer to discovery and establish connection quality
	peer := testutil.CreateTestPeerInfo("test-peer", peerAddr)
	node.peerDiscovery.UpdatePeerStatistics(peer.ID, true, 100*time.Millisecond)

	// Send file to test peer
	ctx := context.Background()
	err = node.SendFile(ctx, "test-peer", testFile)
	if err != nil {
		t.Fatalf("Failed to send file: %v", err)
	}

	select {
	case data := <-received:
		if string(data) != string(testContent) {
			t.Error("Received file content mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("File transfer timed out")
	}
}

func TestBandwidthManagement(t *testing.T) {
	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"
	cfg.Config.Network.BandwidthLimit = 1024 * 1024 // 1MB/s

	bwManager := bandwidth.NewManager(1024 * 1024)
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	// Test bandwidth measurement
	peer := &model.PeerInfo{
		ID:        "test-peer",
		Addresses: []string{"127.0.0.1:9999"},
	}

	bandwidth, err := node.measureBandwidth(peer)
	if err != nil {
		t.Logf("Bandwidth measurement error (expected for mock peer): %v", err)
	}
	if bandwidth < 0 {
		t.Error("Bandwidth measurement should not be negative")
	}
}

func TestHeartbeat(t *testing.T) {
	heartbeatReceived := make(chan struct{})
	peerAddr, cleanup := testutil.NewTestServer(t, func(conn net.Conn) {
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Errorf("Failed to read heartbeat: %v", err)
			return
		}
		if string(buf[:n]) == "ping" {
			conn.Write([]byte("pong"))
			close(heartbeatReceived)
		}
	})
	defer cleanup()

	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"
	bwManager := bandwidth.NewManager(1024 * 1024)
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{}, bwManager)
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	peer := &model.PeerInfo{
		ID:        "test-peer",
		Addresses: []string{peerAddr},
	}

	err = node.sendPing(peer)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}

	select {
	case <-heartbeatReceived:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Heartbeat response not received within timeout")
	}
}
