package protocol

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/p2p/bandwidth"
)

type MessageType string

const (
	// Basic sync types
	SyncRequest  MessageType = "SYNC_REQUEST"
	SyncResponse MessageType = "SYNC_RESPONSE"
	SyncBatch    MessageType = "SYNC_BATCH"
	SyncProgress MessageType = "SYNC_PROGRESS"

	// Peer management types
	PeerAnnounce     MessageType = "PEER_ANNOUNCE"
	PeerInfo         MessageType = "PEER_INFO"
	PeerListRequest  MessageType = "PEER_LIST_REQUEST"
	PeerListResponse MessageType = "PEER_LIST_RESPONSE"
	Heartbeat        MessageType = "HEARTBEAT"

	// Consensus types
	ConsensusPropose MessageType = "CONSENSUS_PROPOSE"
	ConsensusVote    MessageType = "CONSENSUS_VOTE"
	ConsensusCommit  MessageType = "CONSENSUS_COMMIT"
	ConsensusAbort   MessageType = "CONSENSUS_ABORT"

	// FileTransfer message types
	FileChunkRequest  MessageType = "FILE_CHUNK_REQUEST"
	FileChunkResponse MessageType = "FILE_CHUNK_RESPONSE"
	TransferComplete  MessageType = "TRANSFER_COMPLETE"

	// Conflict resolution types
	ConflictResolutionPropose MessageType = "CONFLICT_RESOLUTION_PROPOSE"
	ConflictResolutionVote    MessageType = "CONFLICT_RESOLUTION_VOTE"
)

// Message represents a protocol message
type Message struct {
	Type    MessageType    `json:"type"`
	Payload map[string]any `json:"payload"`
}

// FileChunk represents a piece of a file being transferred
type FileChunk struct {
	SessionID string
	Offset    int64
	Data      []byte
	Hash      string // SHA-256 hash for verification
	Total     int64  // Total file size
	Index     int    // Chunk index
	Count     int    // Total number of chunks
}

// TransferSession represents an active file transfer
type TransferSession struct {
	ID                string
	FilePath          string
	FileSize          int64
	ChunkSize         int64
	ReceivedChunks    map[int][]byte
	VerifiedChunks    map[int]bool
	OutstandingChunks map[int64]time.Time
	Stats             *TransferStats
	LastActivity      time.Time
	mu                sync.Mutex
}

// TransferStats tracks statistics for a transfer session
type TransferStats struct {
	StartTime     time.Time
	BytesTotal    int64
	BytesSent     int64
	BytesReceived int64
	Retransmits   int64
	RTTStats      []time.Duration
}

// ChunkAck represents a chunk acknowledgment
type ChunkAck struct {
	SessionID string
	Offset    int64
	RTT       time.Duration
}

// ConflictProposal represents a proposed conflict resolution
type ConflictProposal struct {
	ConflictID string           `json:"conflict_id"`
	RepoPath   string           `json:"repo_path"`
	FilePath   string           `json:"file_path"`
	Timestamp  time.Time        `json:"timestamp"`
	Strategy   string           `json:"strategy"`
	Changes    []ConflictChange `json:"changes"`
}

// ConflictChange represents a version of a conflicted file
type ConflictChange struct {
	PeerID    string    `json:"peer_id"`
	Timestamp time.Time `json:"timestamp"`
	Hash      string    `json:"hash"`
	Content   []byte    `json:"content"`
}

// ConflictVote represents a vote on a conflict resolution
type ConflictVote struct {
	ConflictID string    `json:"conflict_id"`
	PeerID     string    `json:"peer_id"`
	Accept     bool      `json:"accept"`
	Strategy   string    `json:"strategy"`
	Timestamp  time.Time `json:"timestamp"`
}

// ProtocolHandler manages protocol message handling
type ProtocolHandler struct {
	gitManager   *git.GitRepositoryManager
	bandwidthMgr *bandwidth.Manager
	ctx          context.Context
	cancel       context.CancelFunc

	// Event handlers
	onPeerAnnounce     func(*model.PeerInfo)
	onPeerListRequest  func() []model.PeerInfo
	onHeartbeat        func(string, time.Time)
	onSyncProgress     func(*model.SyncOperation)
	onMetadataUpdate   func(string, *model.SyncMetrics)
	onConsensusPropose func(*model.SyncProposal) (*model.SyncVote, error)
	onConsensusVote    func(*model.SyncVote) error
	onConsensusCommit  func(*model.SyncCommit) error

	// Transfer management
	transfersMu     sync.RWMutex
	activeTransfers map[string]*TransferSession
	chunkAcks       chan ChunkAck
}

func NewProtocolHandler(gitManager *git.GitRepositoryManager, bandwidthMgr *bandwidth.Manager) *ProtocolHandler {
	ctx, cancel := context.WithCancel(context.Background())

	ph := &ProtocolHandler{
		gitManager:      gitManager,
		bandwidthMgr:    bandwidthMgr,
		ctx:             ctx,
		cancel:          cancel,
		activeTransfers: make(map[string]*TransferSession),
		chunkAcks:       make(chan ChunkAck, 1000),
	}

	go ph.monitorTransfers()
	return ph
}

func (ph *ProtocolHandler) monitorTransfers() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ph.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			ph.transfersMu.Lock()
			for id, session := range ph.activeTransfers {
				session.mu.Lock()
				if len(session.OutstandingChunks) > 0 {
					oldestChunk := now
					for _, sendTime := range session.OutstandingChunks {
						if sendTime.Before(oldestChunk) {
							oldestChunk = sendTime
						}
					}
					if now.Sub(oldestChunk) > 10*time.Second {
						// Chunk timeout - retransmit needed
						session.Stats.Retransmits++
					}
				}
				if now.Sub(session.LastActivity) > 30*time.Second {
					// Session timeout
					delete(ph.activeTransfers, id)
				}
				session.mu.Unlock()
			}
			ph.transfersMu.Unlock()
		}
	}
}

func (ph *ProtocolHandler) handleFileChunk(chunk *FileChunk) error {
	ph.transfersMu.Lock()
	session, exists := ph.activeTransfers[chunk.SessionID]
	ph.transfersMu.Unlock()

	if !exists {
		return fmt.Errorf("unknown transfer session: %s", chunk.SessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	// Verify chunk hash
	hasher := sha256.New()
	hasher.Write(chunk.Data)
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != chunk.Hash {
		return fmt.Errorf("chunk hash mismatch")
	}

	// Store verified chunk
	session.ReceivedChunks[chunk.Index] = chunk.Data
	session.VerifiedChunks[chunk.Index] = true
	delete(session.OutstandingChunks, chunk.Offset)
	session.LastActivity = time.Now()

	// Check if transfer is complete
	if len(session.VerifiedChunks) == chunk.Count {
		return ph.assembleFile(session)
	}

	return nil
}

func (ph *ProtocolHandler) assembleFile(session *TransferSession) error {
	// Create temporary file
	tempFile, err := os.CreateTemp("", "gitsync-transfer-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	// Assemble chunks in order
	for i := 0; i < len(session.ReceivedChunks); i++ {
		chunk, exists := session.ReceivedChunks[i]
		if !exists {
			return fmt.Errorf("missing chunk %d", i)
		}
		if _, err := tempFile.Write(chunk); err != nil {
			return fmt.Errorf("failed to write chunk: %w", err)
		}
	}

	// Move assembled file to final location
	if err := os.Rename(tempFile.Name(), session.FilePath); err != nil {
		return fmt.Errorf("failed to move assembled file: %w", err)
	}

	return nil
}

func (ph *ProtocolHandler) SendFileChunk(conn net.Conn, chunk *FileChunk) error {
	return ph.SendMessage(conn, FileChunkResponse, chunk)
}

func (ph *ProtocolHandler) SendBatchWithCompression(conn net.Conn, msg *Message) (int64, error) {
	compressed, size, err := ph.compressMessage(msg)
	if err != nil {
		return 0, err
	}
	return size, ph.SendMessage(conn, msg.Type, compressed)
}

func (ph *ProtocolHandler) ReadMessage(conn net.Conn) (*Message, error) {
	var msg Message
	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (ph *ProtocolHandler) ProposeSync(conn net.Conn, proposal *model.SyncProposal) error {
	return ph.SendMessage(conn, ConsensusPropose, proposal)
}

func (ph *ProtocolHandler) ProposeConflictResolution(conn net.Conn, proposal *ConflictProposal) error {
	return ph.SendMessage(conn, ConflictResolutionPropose, proposal)
}

func (ph *ProtocolHandler) compressMessage(msg *Message) ([]byte, int64, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, 0, err
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		return nil, 0, err
	}
	if err := gw.Close(); err != nil {
		return nil, 0, err
	}

	compressed := buf.Bytes()
	return compressed, int64(len(data)), nil
}

// Message handling methods
func (ph *ProtocolHandler) SendMessage(conn net.Conn, msgType MessageType, payload interface{}) error {
	data := make(map[string]any)

	// Convert payload to map[string]any
	if payload != nil {
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return err
		}
	}

	msg := Message{
		Type:    msgType,
		Payload: data,
	}
	return json.NewEncoder(conn).Encode(msg)
}

// HandleMessage handles an incoming message from a connection
func (ph *ProtocolHandler) HandleMessage(conn net.Conn) error {
	msg, err := ph.ReadMessage(conn)
	if err != nil {
		return fmt.Errorf("failed to read message: %w", err)
	}

	switch msg.Type {
	case SyncRequest:
		return ph.handleSyncRequest(conn, msg)
	case FileChunkResponse:
		var chunk FileChunk
		if err := mapToStruct(msg.Payload, &chunk); err != nil {
			return err
		}
		return ph.handleFileChunk(&chunk)
	case Heartbeat:
		return ph.handleHeartbeat(conn, msg)
	case PeerAnnounce:
		var peer model.PeerInfo
		if err := mapToStruct(msg.Payload, &peer); err != nil {
			return err
		}
		if ph.onPeerAnnounce != nil {
			ph.onPeerAnnounce(&peer)
		}
		return nil
	case PeerListRequest:
		if ph.onPeerListRequest != nil {
			peers := ph.onPeerListRequest()
			return ph.SendMessage(conn, PeerListResponse, map[string]interface{}{
				"peers": peers,
			})
		}
		return nil
	case ConsensusPropose:
		var proposal model.SyncProposal
		if err := mapToStruct(msg.Payload, &proposal); err != nil {
			return err
		}
		if ph.onConsensusPropose != nil {
			vote, err := ph.onConsensusPropose(&proposal)
			if err != nil {
				return err
			}
			return ph.SendMessage(conn, ConsensusVote, vote)
		}
		return nil
	case ConsensusVote:
		var vote model.SyncVote
		if err := mapToStruct(msg.Payload, &vote); err != nil {
			return err
		}
		if ph.onConsensusVote != nil {
			return ph.onConsensusVote(&vote)
		}
		return nil
	case ConsensusCommit:
		var commit model.SyncCommit
		if err := mapToStruct(msg.Payload, &commit); err != nil {
			return err
		}
		if ph.onConsensusCommit != nil {
			return ph.onConsensusCommit(&commit)
		}
		return nil
	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// handleSyncRequest handles incoming sync requests
func (ph *ProtocolHandler) handleSyncRequest(conn net.Conn, msg *Message) error {
	var request struct {
		Repository string    `json:"repository"`
		Timestamp  time.Time `json:"timestamp"`
	}
	if err := mapToStruct(msg.Payload, &request); err != nil {
		return fmt.Errorf("failed to parse sync request: %w", err)
	}

	// Call sync operation handler if registered
	if ph.onSyncProgress != nil {
		ph.onSyncProgress(&model.SyncOperation{
			RepositoryID: request.Repository,
			StartTime:    request.Timestamp,
			Status:       "running",
		})
	}

	// Send sync response
	return ph.SendMessage(conn, SyncResponse, map[string]interface{}{
		"status":    "acknowledged",
		"timestamp": time.Now(),
	})
}

// handleHeartbeat handles incoming heartbeat messages
func (ph *ProtocolHandler) handleHeartbeat(conn net.Conn, msg *Message) error {
	var heartbeat struct {
		PeerID    string    `json:"peer_id"`
		Timestamp time.Time `json:"timestamp"`
		Status    string    `json:"status"`
	}
	if err := mapToStruct(msg.Payload, &heartbeat); err != nil {
		return fmt.Errorf("failed to parse heartbeat: %w", err)
	}

	// Call heartbeat handler if registered
	if ph.onHeartbeat != nil {
		ph.onHeartbeat(heartbeat.PeerID, heartbeat.Timestamp)
	}

	return nil
}

// SendSyncRequestMessage sends a sync request for a specific repository
func (ph *ProtocolHandler) SendSyncRequestMessage(conn net.Conn, repoName string) error {
	return ph.SendMessage(conn, SyncRequest, map[string]interface{}{
		"repository": repoName,
		"timestamp":  time.Now(),
	})
}

// SendHeartbeat sends a heartbeat message
func (ph *ProtocolHandler) SendHeartbeat(conn net.Conn, messageType string) error {
	return ph.SendMessage(conn, Heartbeat, map[string]interface{}{
		"type":      messageType,
		"timestamp": time.Now(),
	})
}

// SendFile initiates a file transfer to a peer
func (ph *ProtocolHandler) SendFile(conn net.Conn, filePath string) error {
	// Open and stat the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Create a new transfer session
	sessionID := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s-%d", filePath, time.Now().UnixNano()))))
	session := &TransferSession{
		ID:                sessionID,
		FilePath:          filePath,
		FileSize:          fileInfo.Size(),
		ChunkSize:         1 << 16, // 64KB chunks
		ReceivedChunks:    make(map[int][]byte),
		VerifiedChunks:    make(map[int]bool),
		OutstandingChunks: make(map[int64]time.Time),
		Stats: &TransferStats{
			StartTime:  time.Now(),
			BytesTotal: fileInfo.Size(),
		},
		LastActivity: time.Now(),
	}

	ph.transfersMu.Lock()
	ph.activeTransfers[sessionID] = session
	ph.transfersMu.Unlock()

	// Calculate number of chunks
	numChunks := int((fileInfo.Size() + session.ChunkSize - 1) / session.ChunkSize)

	// Send file chunks
	buffer := make([]byte, session.ChunkSize)
	for i := 0; i < numChunks; i++ {
		n, err := file.Read(buffer)
		if err != nil {
			return fmt.Errorf("failed to read file chunk: %w", err)
		}

		// Calculate chunk hash
		hasher := sha256.New()
		hasher.Write(buffer[:n])
		hash := hex.EncodeToString(hasher.Sum(nil))

		chunk := &FileChunk{
			SessionID: sessionID,
			Offset:    int64(i) * session.ChunkSize,
			Data:      buffer[:n],
			Hash:      hash,
			Total:     fileInfo.Size(),
			Index:     i,
			Count:     numChunks,
		}

		if err := ph.SendFileChunk(conn, chunk); err != nil {
			return fmt.Errorf("failed to send file chunk: %w", err)
		}

		session.mu.Lock()
		session.Stats.BytesSent += int64(n)
		session.OutstandingChunks[chunk.Offset] = time.Now()
		session.mu.Unlock()

		// Let the bandwidth manager control the send rate
		if ph.bandwidthMgr != nil {
			if err := ph.bandwidthMgr.AcquireBandwidth(sessionID, int64(n)); err != nil {
				return fmt.Errorf("bandwidth limit exceeded: %w", err)
			}
		}
	}

	// Send transfer complete message
	return ph.SendMessage(conn, TransferComplete, map[string]interface{}{
		"session_id": sessionID,
		"file_path":  filePath,
		"size":       fileInfo.Size(),
		"chunks":     numChunks,
	})
}

// Handler setter methods
func (ph *ProtocolHandler) SetPeerAnnounceHandler(handler func(*model.PeerInfo)) {
	ph.onPeerAnnounce = handler
}

func (ph *ProtocolHandler) SetPeerListRequestHandler(handler func() []model.PeerInfo) {
	ph.onPeerListRequest = handler
}

func (ph *ProtocolHandler) SetMetadataUpdateHandler(handler func(string, *model.SyncMetrics)) {
	ph.onMetadataUpdate = handler
}

func (ph *ProtocolHandler) SetConsensusHandlers(
	proposeHandler func(*model.SyncProposal) (*model.SyncVote, error),
	voteHandler func(*model.SyncVote) error,
	commitHandler func(*model.SyncCommit) error,
) {
	ph.onConsensusPropose = proposeHandler
	ph.onConsensusVote = voteHandler
	ph.onConsensusCommit = commitHandler
}

// Helper function to convert map to struct
func mapToStruct(m map[string]interface{}, val interface{}) error {
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, val)
}
