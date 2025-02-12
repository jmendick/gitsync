package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/model"
)

// MessageType represents the type of protocol message
type MessageType string

const (
	// Existing message types
	SyncRequest      MessageType = "SYNC_REQUEST"
	SyncResponse     MessageType = "SYNC_RESPONSE"
	PeerAnnounce     MessageType = "PEER_ANNOUNCE"
	PeerInfo         MessageType = "PEER_INFO"
	PeerListRequest  MessageType = "PEER_LIST_REQUEST"
	PeerListResponse MessageType = "PEER_LIST_RESPONSE"
	Heartbeat        MessageType = "HEARTBEAT"

	// New message types for enhanced protocol
	RepoStateAdvertise   MessageType = "REPO_STATE_ADVERTISE"
	RepoStateRequest     MessageType = "REPO_STATE_REQUEST"
	BranchSyncRequest    MessageType = "BRANCH_SYNC_REQUEST"
	BranchSyncResponse   MessageType = "BRANCH_SYNC_RESPONSE"
	ConflictNotification MessageType = "CONFLICT_NOTIFICATION"
	ConflictResolution   MessageType = "CONFLICT_RESOLUTION"
	CapabilityExchange   MessageType = "CAPABILITY_EXCHANGE"
)

// Message represents a protocol message
type Message struct {
	Type    MessageType    `json:"type"`
	Payload map[string]any `json:"payload"`
}

// New message structures for enhanced protocol
type RepositoryState struct {
	Name           string            `json:"name"`
	Branches       map[string]string `json:"branches"` // branch name -> commit hash
	HeadCommit     string            `json:"head_commit"`
	LastUpdateTime time.Time         `json:"last_update_time"`
}

type BranchSyncInfo struct {
	RepoName    string   `json:"repo_name"`
	BranchName  string   `json:"branch_name"`
	CommitHash  string   `json:"commit_hash"`
	CommitRange []string `json:"commit_range"` // For partial syncs
}

type ConflictInfo struct {
	RepoName     string `json:"repo_name"`
	BranchName   string `json:"branch_name"`
	ConflictType string `json:"conflict_type"`
	FilePath     string `json:"file_path"`
	Resolution   string `json:"resolution,omitempty"`
}

type PeerCapabilities struct {
	ProtocolVersion    string   `json:"protocol_version"`
	SupportedFeatures  []string `json:"supported_features"`
	MaxSyncBatchSize   int64    `json:"max_sync_batch_size"`
	CompressionSupport bool     `json:"compression_support"`
}

// Message structures
type PeerAnnounceMessage struct {
	PeerInfo     model.PeerInfo `json:"peer_info"`
	TimeStamp    time.Time      `json:"timestamp"`
	Repositories []string       `json:"repositories"`
}

type HeartbeatMessage struct {
	PeerID    string    `json:"peer_id"`
	TimeStamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
}

// ProtocolHandler handles the synchronization protocol messages.
type ProtocolHandler struct {
	gitManager *git.GitRepositoryManager
	// Existing handlers
	onPeerAnnounce    func(model.PeerInfo)
	onPeerListRequest func() []model.PeerInfo
	onHeartbeat       func(string, time.Time)
	// New handlers
	onRepoStateRequest     func(string) (*RepositoryState, error)
	onBranchSyncRequest    func(BranchSyncInfo) error
	onConflictNotification func(ConflictInfo) error
	capabilities           PeerCapabilities
}

// NewProtocolHandler creates a new ProtocolHandler.
func NewProtocolHandler(gitManager *git.GitRepositoryManager) *ProtocolHandler {
	return &ProtocolHandler{
		gitManager: gitManager,
	}
}

// SetPeerAnnounceHandler sets the handler for peer announcements
func (ph *ProtocolHandler) SetPeerAnnounceHandler(handler func(model.PeerInfo)) {
	ph.onPeerAnnounce = handler
}

// SetPeerListRequestHandler sets the handler for peer list requests
func (ph *ProtocolHandler) SetPeerListRequestHandler(handler func() []model.PeerInfo) {
	ph.onPeerListRequest = handler
}

// SetHeartbeatHandler sets the handler for peer heartbeats
func (ph *ProtocolHandler) SetHeartbeatHandler(handler func(string, time.Time)) {
	ph.onHeartbeat = handler
}

// New handler setters for enhanced protocol
func (ph *ProtocolHandler) SetRepoStateRequestHandler(handler func(string) (*RepositoryState, error)) {
	ph.onRepoStateRequest = handler
}

func (ph *ProtocolHandler) SetBranchSyncRequestHandler(handler func(BranchSyncInfo) error) {
	ph.onBranchSyncRequest = handler
}

func (ph *ProtocolHandler) SetConflictNotificationHandler(handler func(ConflictInfo) error) {
	ph.onConflictNotification = handler
}

// HandleMessage processes an incoming message from a peer.
func (ph *ProtocolHandler) HandleMessage(conn io.ReadWriter) error {
	// Read the message
	decoder := json.NewDecoder(conn)
	var msg Message
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("failed to decode message: %w", err)
	}

	// Process message based on type
	switch msg.Type {
	case SyncRequest:
		return ph.handleSyncRequest(conn, msg.Payload)
	case SyncResponse:
		return ph.handleSyncResponse(msg.Payload)
	case PeerAnnounce:
		return ph.handlePeerAnnounce(msg.Payload)
	case PeerListRequest:
		return ph.handlePeerListRequest(conn)
	case Heartbeat:
		return ph.handleHeartbeat(msg.Payload)
	case RepoStateRequest:
		return ph.handleRepoStateRequest(conn, msg.Payload)
	case RepoStateAdvertise:
		return ph.handleRepoStateAdvertise(msg.Payload)
	case BranchSyncRequest:
		return ph.handleBranchSyncRequest(conn, msg.Payload)
	case ConflictNotification:
		return ph.handleConflictNotification(msg.Payload)
	case CapabilityExchange:
		return ph.handleCapabilityExchange(conn, msg.Payload)
	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// SendSyncRequestMessage sends a sync request message to a peer.
func (ph *ProtocolHandler) SendSyncRequestMessage(conn io.Writer, repoName string) error {
	msg := Message{
		Type: SyncRequest,
		Payload: map[string]any{
			"repository": repoName,
		},
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to encode sync request: %w", err)
	}
	return nil
}

// SendSyncResponseMessage sends a sync response message to a peer.
func (ph *ProtocolHandler) SendSyncResponseMessage(conn io.Writer, success bool, message string) error {
	msg := Message{
		Type: SyncResponse,
		Payload: map[string]any{
			"success": success,
			"message": message,
		},
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to encode sync response: %w", err)
	}
	return nil
}

// handleSyncRequest processes an incoming sync request
func (ph *ProtocolHandler) handleSyncRequest(conn io.Writer, payload map[string]any) error {
	repoName, ok := payload["repository"].(string)
	if !ok {
		return ph.SendSyncResponseMessage(conn, false, "invalid repository name in request")
	}

	// Open or initialize the repository
	repo, err := ph.gitManager.OpenRepository(repoName)
	if err != nil {
		return ph.SendSyncResponseMessage(conn, false, fmt.Sprintf("failed to open repository: %v", err))
	}

	// Try to fetch updates
	err = ph.gitManager.FetchRepository(context.Background(), repo)
	if err != nil {
		return ph.SendSyncResponseMessage(conn, false, fmt.Sprintf("failed to fetch repository: %v", err))
	}

	// Get the HEAD reference
	headRef, err := ph.gitManager.GetHeadReference(repo)
	if err != nil {
		return ph.SendSyncResponseMessage(conn, false, fmt.Sprintf("failed to get HEAD reference: %v", err))
	}

	return ph.SendSyncResponseMessage(conn, true, fmt.Sprintf("synchronized repository %s at commit %s", repoName, headRef.Hash()))
}

// handleSyncResponse processes an incoming sync response
func (ph *ProtocolHandler) handleSyncResponse(payload map[string]any) error {
	success, _ := payload["success"].(bool)
	message, _ := payload["message"].(string)

	fmt.Printf("Received sync response: success=%v, message='%s'\n", success, message)
	return nil
}

// New message handling methods
func (ph *ProtocolHandler) handlePeerAnnounce(payload map[string]any) error {
	var announce PeerAnnounceMessage
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	if err := json.Unmarshal(data, &announce); err != nil {
		return fmt.Errorf("failed to unmarshal peer announce: %w", err)
	}

	if ph.onPeerAnnounce != nil {
		ph.onPeerAnnounce(announce.PeerInfo)
	}
	return nil
}

func (ph *ProtocolHandler) handlePeerListRequest(conn io.Writer) error {
	var peers []model.PeerInfo
	if ph.onPeerListRequest != nil {
		peers = ph.onPeerListRequest()
	}

	response := Message{
		Type: PeerListResponse,
		Payload: map[string]any{
			"peers": peers,
		},
	}

	encoder := json.NewEncoder(conn)
	return encoder.Encode(response)
}

func (ph *ProtocolHandler) handleHeartbeat(payload map[string]any) error {
	var heartbeat HeartbeatMessage
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	if err := json.Unmarshal(data, &heartbeat); err != nil {
		return fmt.Errorf("failed to unmarshal heartbeat: %w", err)
	}

	if ph.onHeartbeat != nil {
		ph.onHeartbeat(heartbeat.PeerID, heartbeat.TimeStamp)
	}
	return nil
}

func (ph *ProtocolHandler) handleRepoStateRequest(conn io.Writer, payload map[string]any) error {
	repoName, ok := payload["repository"].(string)
	if !ok {
		return fmt.Errorf("invalid repository name in request")
	}

	if ph.onRepoStateRequest == nil {
		return fmt.Errorf("no repo state request handler configured")
	}

	state, err := ph.onRepoStateRequest(repoName)
	if err != nil {
		return err
	}

	response := Message{
		Type: RepoStateAdvertise,
		Payload: map[string]any{
			"state": state,
		},
	}

	return json.NewEncoder(conn).Encode(response)
}

func (ph *ProtocolHandler) handleRepoStateAdvertise(payload map[string]any) error {
	var state RepositoryState
	data, err := json.Marshal(payload["state"])
	if err != nil {
		return fmt.Errorf("failed to marshal state data: %w", err)
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal repository state: %w", err)
	}

	// Process the repository state
	// Compare with local state and initiate sync if needed
	return nil
}

func (ph *ProtocolHandler) handleBranchSyncRequest(conn io.Writer, payload map[string]any) error {
	var syncInfo BranchSyncInfo
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal sync info: %w", err)
	}

	if err := json.Unmarshal(data, &syncInfo); err != nil {
		return fmt.Errorf("failed to unmarshal branch sync info: %w", err)
	}

	if ph.onBranchSyncRequest != nil {
		if err := ph.onBranchSyncRequest(syncInfo); err != nil {
			return err
		}
	}

	response := Message{
		Type: BranchSyncResponse,
		Payload: map[string]any{
			"success": true,
			"branch":  syncInfo.BranchName,
		},
	}

	return json.NewEncoder(conn).Encode(response)
}

func (ph *ProtocolHandler) handleConflictNotification(payload map[string]any) error {
	var conflictInfo ConflictInfo
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal conflict info: %w", err)
	}

	if err := json.Unmarshal(data, &conflictInfo); err != nil {
		return fmt.Errorf("failed to unmarshal conflict info: %w", err)
	}

	if ph.onConflictNotification != nil {
		return ph.onConflictNotification(conflictInfo)
	}
	return nil
}

func (ph *ProtocolHandler) handleCapabilityExchange(conn io.Writer, payload map[string]any) error {
	var peerCapabilities PeerCapabilities
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}

	if err := json.Unmarshal(data, &peerCapabilities); err != nil {
		return fmt.Errorf("failed to unmarshal capabilities: %w", err)
	}

	// Send our capabilities in response
	response := Message{
		Type: CapabilityExchange,
		Payload: map[string]any{
			"capabilities": ph.capabilities,
		},
	}

	return json.NewEncoder(conn).Encode(response)
}
