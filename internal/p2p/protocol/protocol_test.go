package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/testutil"
)

func TestProtocolMessageEncoding(t *testing.T) {
	tests := []struct {
		name     string
		message  Message
		wantType MessageType
	}{
		{
			name: "sync request message",
			message: Message{
				Type: SyncRequest,
				Payload: map[string]any{
					"repository": "test-repo",
				},
			},
			wantType: SyncRequest,
		},
		{
			name: "peer announce message",
			message: Message{
				Type: PeerAnnounce,
				Payload: map[string]any{
					"peer_info": testutil.CreateTestPeerInfo("test-peer", "127.0.0.1:8080"),
					"timestamp": time.Now(),
				},
			},
			wantType: PeerAnnounce,
		},
		{
			name: "peer list request message",
			message: Message{
				Type:    PeerListRequest,
				Payload: map[string]any{},
			},
			wantType: PeerListRequest,
		},
		{
			name: "heartbeat message",
			message: Message{
				Type: Heartbeat,
				Payload: map[string]any{
					"peer_id":   "test-peer",
					"timestamp": time.Now(),
					"status":    "ACTIVE",
				},
			},
			wantType: Heartbeat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := testutil.SendTestMessage(&buf, string(tt.message.Type), tt.message.Payload); err != nil {
				t.Fatalf("Failed to encode message: %v", err)
			}

			var decoded Message
			decoder := json.NewDecoder(&buf)
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatalf("Failed to decode message: %v", err)
			}

			if decoded.Type != tt.wantType {
				t.Errorf("Got message type %s, want %s", decoded.Type, tt.wantType)
			}
		})
	}
}

func TestProtocolHandler(t *testing.T) {
	mockGitManager := &git.GitRepositoryManager{}
	handler := NewProtocolHandler(mockGitManager)

	tests := []struct {
		name      string
		setupFunc func(*ProtocolHandler)
		message   Message
		wantErr   bool
	}{
		{
			name: "handle peer announce",
			setupFunc: func(h *ProtocolHandler) {
				h.SetPeerAnnounceHandler(func(info model.PeerInfo) {
					if info.ID != "test-peer" {
						t.Errorf("Got peer ID %s, want test-peer", info.ID)
					}
				})
			},
			message: Message{
				Type: PeerAnnounce,
				Payload: map[string]any{
					"peer_info": testutil.CreateTestPeerInfo("test-peer", "127.0.0.1:8080"),
					"timestamp": time.Now(),
				},
			},
			wantErr: false,
		},
		{
			name: "handle peer list request",
			setupFunc: func(h *ProtocolHandler) {
				h.SetPeerListRequestHandler(func() []model.PeerInfo {
					return []model.PeerInfo{*testutil.CreateTestPeerInfo("test-peer", "127.0.0.1:8080")}
				})
			},
			message: Message{
				Type:    PeerListRequest,
				Payload: map[string]any{},
			},
			wantErr: false,
		},
		{
			name: "handle heartbeat",
			setupFunc: func(h *ProtocolHandler) {
				h.SetHeartbeatHandler(func(peerID string, timestamp time.Time) {
					if peerID != "test-peer" {
						t.Errorf("Got peer ID %s, want test-peer", peerID)
					}
				})
			},
			message: Message{
				Type: Heartbeat,
				Payload: map[string]any{
					"peer_id":   "test-peer",
					"timestamp": time.Now(),
					"status":    "ACTIVE",
				},
			},
			wantErr: false,
		},
		{
			name: "handle invalid message type",
			message: Message{
				Type:    "INVALID_TYPE",
				Payload: map[string]any{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupFunc != nil {
				tt.setupFunc(handler)
			}

			var buf bytes.Buffer
			if err := testutil.SendTestMessage(&buf, string(tt.message.Type), tt.message.Payload); err != nil {
				t.Fatalf("Failed to encode test message: %v", err)
			}

			err := handler.HandleMessage(&buf)
			if (err != nil) != tt.wantErr {
				t.Errorf("HandleMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPeerListResponse(t *testing.T) {
	handler := NewProtocolHandler(nil)
	testPeers := []model.PeerInfo{
		*testutil.CreateTestPeerInfo("peer1", "127.0.0.1:8001"),
		*testutil.CreateTestPeerInfo("peer2", "127.0.0.1:8002"),
	}

	var buf bytes.Buffer
	responseReceived := make(chan struct{})

	go func() {
		defer close(responseReceived)
		var response Message
		if err := json.NewDecoder(&buf).Decode(&response); err != nil {
			t.Errorf("Failed to decode response: %v", err)
			return
		}

		if response.Type != PeerListResponse {
			t.Errorf("Got response type %s, want %s", response.Type, PeerListResponse)
		}

		var peerList struct {
			Peers []model.PeerInfo `json:"peers"`
		}
		peerData, err := json.Marshal(response.Payload["peers"])
		if err != nil {
			t.Errorf("Failed to marshal peer data: %v", err)
			return
		}

		if err := json.Unmarshal(peerData, &peerList.Peers); err != nil {
			t.Errorf("Failed to unmarshal peer list: %v", err)
			return
		}

		if len(peerList.Peers) != len(testPeers) {
			t.Errorf("Got %d peers, want %d", len(peerList.Peers), len(testPeers))
		}
	}()

	handler.SetPeerListRequestHandler(func() []model.PeerInfo {
		return testPeers
	})

	if err := testutil.SendTestMessage(&buf, string(PeerListRequest), map[string]any{}); err != nil {
		t.Fatalf("Failed to send peer list request: %v", err)
	}

	if err := handler.HandleMessage(&buf); err != nil {
		t.Fatalf("Failed to handle peer list request: %v", err)
	}

	testutil.AssertEventually(t, func() bool {
		select {
		case <-responseReceived:
			return true
		default:
			return false
		}
	}, time.Second)
}
