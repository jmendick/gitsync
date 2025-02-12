package p2p

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
	"github.com/jmendick/gitsync/internal/testutil"
)

func TestNodeCreation(t *testing.T) {
	cfg := testutil.NewMockConfig()
	cfg.Config.ListenAddress = "127.0.0.1:0"

	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{})
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

	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{})
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

	testutil.AssertEventually(t, func() bool {
		select {
		case <-connectionReceived:
			return true
		default:
			return false
		}
	}, 2*time.Second)
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

	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{})
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	discovery := node.GetPeerDiscovery()
	if discovery == nil {
		t.Fatal("Peer discovery service should not be nil")
	}

	testutil.AssertEventually(t, func() bool {
		select {
		case <-connectionAttempted:
			return true
		default:
			return false
		}
	}, 2*time.Second)
}

func TestNodeShutdown(t *testing.T) {
	cfg := testutil.NewMockConfig()
	protocolHandler := protocol.NewProtocolHandler(&git.GitRepositoryManager{})
	node, err := NewNode(cfg.Config, protocolHandler)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	addr := node.listener.Addr().String()

	if err := node.Close(); err != nil {
		t.Fatalf("Failed to close node: %v", err)
	}

	testutil.WaitForCondition(t, func() bool {
		_, err := net.Dial("tcp", addr)
		return err != nil
	}, time.Second, "Expected connection to fail after node shutdown")
}
