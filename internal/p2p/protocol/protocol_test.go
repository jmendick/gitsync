package protocol

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/p2p/bandwidth"
)

// MockConn implements net.Conn for testing
type MockConn struct {
	ReadBuf  *bytes.Buffer
	WriteBuf *bytes.Buffer
}

func NewMockConn() *MockConn {
	return &MockConn{
		ReadBuf:  new(bytes.Buffer),
		WriteBuf: new(bytes.Buffer),
	}
}

// Implement net.Conn interface
func (m *MockConn) Read(b []byte) (n int, err error)   { return m.ReadBuf.Read(b) }
func (m *MockConn) Write(b []byte) (n int, err error)  { return m.WriteBuf.Write(b) }
func (m *MockConn) Close() error                       { return nil }
func (m *MockConn) LocalAddr() net.Addr                { return nil }
func (m *MockConn) RemoteAddr() net.Addr               { return nil }
func (m *MockConn) SetDeadline(t time.Time) error      { return nil }
func (m *MockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *MockConn) SetWriteDeadline(t time.Time) error { return nil }

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
					"peer_info": model.PeerInfo{
						ID:        "test-peer",
						Addresses: []string{"127.0.0.1:8080"},
						LastSeen:  time.Now(),
					},
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
		{
			name: "file chunk request",
			message: Message{
				Type: FileChunkRequest,
				Payload: map[string]any{
					"session_id": "transfer-123",
					"offset":     int64(0),
					"size":       int64(1024),
				},
			},
			wantType: FileChunkRequest,
		},
		{
			name: "file chunk response",
			message: Message{
				Type: FileChunkResponse,
				Payload: map[string]any{
					"session_id": "transfer-123",
					"offset":     int64(0),
					"data":       []byte("test data"),
					"hash":       "abc123",
					"total":      int64(1024),
					"index":      0,
					"count":      10,
				},
			},
			wantType: FileChunkResponse,
		},
		{
			name: "consensus propose",
			message: Message{
				Type: ConsensusPropose,
				Payload: map[string]any{
					"proposal": &model.SyncProposal{
						RepoPath:  "test-repo",
						Changes:   []string{"main:abc123"},
						Timestamp: time.Now(),
					},
				},
			},
			wantType: ConsensusPropose,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewMockConn()

			if err := json.NewEncoder(conn.WriteBuf).Encode(tt.message); err != nil {
				t.Fatalf("Failed to encode message: %v", err)
			}

			// Set up read buffer for decoding
			conn.ReadBuf = bytes.NewBuffer(conn.WriteBuf.Bytes())

			var decoded Message
			if err := json.NewDecoder(conn.ReadBuf).Decode(&decoded); err != nil {
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
	bandwidthMgr := bandwidth.NewManager(1024 * 1024)
	handler := NewProtocolHandler(mockGitManager, bandwidthMgr)

	tests := []struct {
		name      string
		setupFunc func(*ProtocolHandler)
		message   Message
		wantErr   bool
	}{
		{
			name: "handle peer announce",
			setupFunc: func(h *ProtocolHandler) {
				h.SetPeerAnnounceHandler(func(info *model.PeerInfo) {
					if info.ID != "test-peer" {
						t.Errorf("Got peer ID %s, want test-peer", info.ID)
					}
				})
			},
			message: Message{
				Type: PeerAnnounce,
				Payload: map[string]any{
					"peer_info": model.PeerInfo{
						ID:        "test-peer",
						Addresses: []string{"127.0.0.1:8080"},
						LastSeen:  time.Now(),
					},
					"timestamp": time.Now(),
				},
			},
			wantErr: false,
		},
		{
			name: "handle peer list request",
			setupFunc: func(h *ProtocolHandler) {
				h.SetPeerListRequestHandler(func() []model.PeerInfo {
					return []model.PeerInfo{{
						ID:        "test-peer",
						Addresses: []string{"127.0.0.1:8080"},
						LastSeen:  time.Now(),
					}}
				})
			},
			message: Message{
				Type:    PeerListRequest,
				Payload: map[string]any{},
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

			conn := NewMockConn()
			if err := json.NewEncoder(conn.WriteBuf).Encode(tt.message); err != nil {
				t.Fatalf("Failed to encode message: %v", err)
			}

			// Set up read buffer
			conn.ReadBuf = bytes.NewBuffer(conn.WriteBuf.Bytes())

			err := handler.HandleMessage(conn)
			if (err != nil) != tt.wantErr {
				t.Errorf("HandleMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFileTransferHandling(t *testing.T) {
	bandwidthMgr := bandwidth.NewManager(1024 * 1024)
	handler := NewProtocolHandler(nil, bandwidthMgr)

	testData := []byte("test file content")
	testHash := "abc123"
	totalSize := int64(len(testData))
	chunkSize := int64(5)

	// Test sending file chunks
	conn := NewMockConn()
	for offset := int64(0); offset < totalSize; offset += chunkSize {
		chunk := &FileChunk{
			SessionID: "test-transfer",
			Offset:    offset,
			Data:      testData[offset:min(offset+chunkSize, totalSize)],
			Hash:      testHash,
			Total:     totalSize,
			Index:     int(offset / chunkSize),
			Count:     int((totalSize + chunkSize - 1) / chunkSize),
		}

		err := handler.SendFileChunk(conn, chunk)
		if err != nil {
			t.Fatalf("Failed to send file chunk: %v", err)
		}

		// Set up read buffer
		conn.ReadBuf = bytes.NewBuffer(conn.WriteBuf.Bytes())
		conn.WriteBuf.Reset()

		err = handler.HandleMessage(conn)
		if err != nil {
			t.Fatalf("Failed to handle chunk message: %v", err)
		}
	}
}

func TestConsensusHandling(t *testing.T) {
	bandwidthMgr := bandwidth.NewManager(1024 * 1024)
	handler := NewProtocolHandler(nil, bandwidthMgr)

	proposalReceived := make(chan *model.SyncProposal)
	voteReceived := make(chan *model.SyncVote)
	commitReceived := make(chan *model.SyncCommit)

	handler.SetConsensusHandlers(
		// Propose handler
		func(proposal *model.SyncProposal) (*model.SyncVote, error) {
			proposalReceived <- proposal
			return &model.SyncVote{
				VoterID: "test-peer",
				Accept:  true,
			}, nil
		},
		// Vote handler
		func(vote *model.SyncVote) error {
			voteReceived <- vote
			return nil
		},
		// Commit handler
		func(commit *model.SyncCommit) error {
			commitReceived <- commit
			return nil
		},
	)

	testProposal := &model.SyncProposal{
		RepoPath:  "test-repo",
		Changes:   []string{"main:abc123"},
		Timestamp: time.Now(),
	}

	conn := NewMockConn()

	// Test proposal flow
	err := handler.ProposeSync(conn, testProposal)
	if err != nil {
		t.Fatalf("Failed to send proposal: %v", err)
	}

	// Set up read buffer
	conn.ReadBuf = bytes.NewBuffer(conn.WriteBuf.Bytes())
	conn.WriteBuf.Reset()

	err = handler.HandleMessage(conn)
	if err != nil {
		t.Fatalf("Failed to handle proposal: %v", err)
	}

	select {
	case received := <-proposalReceived:
		if received.RepoPath != testProposal.RepoPath {
			t.Errorf("Got repo path %s, want %s", received.RepoPath, testProposal.RepoPath)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for proposal")
	}

	// Verify vote was sent back
	var response Message
	if err := json.NewDecoder(conn.WriteBuf).Decode(&response); err != nil {
		t.Fatalf("Failed to decode vote response: %v", err)
	}
	if response.Type != ConsensusVote {
		t.Errorf("Got response type %s, want %s", response.Type, ConsensusVote)
	}
}

func TestCompression(t *testing.T) {
	bandwidthMgr := bandwidth.NewManager(1024 * 1024)
	handler := NewProtocolHandler(nil, bandwidthMgr)

	t.Run("LargeMessageCompression", func(t *testing.T) {
		// Create a large message that should benefit from compression
		largePayload := strings.Repeat("this is test data that should compress well ", 1000)
		msg := Message{
			Type: SyncBatch,
			Payload: map[string]any{
				"data": largePayload,
			},
		}

		// Get compressed data
		compressed, originalSize, err := handler.compressMessage(&msg)
		if err != nil {
			t.Fatalf("Failed to compress message: %v", err)
		}

		compressionRatio := float64(len(compressed)) / float64(originalSize)
		if compressionRatio > 0.5 { // Expect at least 50% compression
			t.Errorf("Expected better compression ratio than %.2f", compressionRatio)
		}

		// Verify decompression
		var decompressed bytes.Buffer
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			t.Fatalf("Failed to create gzip reader: %v", err)
		}
		defer reader.Close()

		if _, err := io.Copy(&decompressed, reader); err != nil {
			t.Fatalf("Failed to decompress data: %v", err)
		}

		var decoded Message
		if err := json.Unmarshal(decompressed.Bytes(), &decoded); err != nil {
			t.Fatalf("Failed to decode decompressed message: %v", err)
		}

		if !reflect.DeepEqual(decoded.Type, msg.Type) {
			t.Error("Decompressed message type doesn't match original")
		}
	})
}

// Helper function for FileTransferHandling test
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
