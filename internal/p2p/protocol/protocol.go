package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jmendick/gitsync/internal/git"
)

// MessageType represents the type of protocol message
type MessageType string

const (
	SyncRequest  MessageType = "SYNC_REQUEST"
	SyncResponse MessageType = "SYNC_RESPONSE"
)

// Message represents a protocol message
type Message struct {
	Type    MessageType    `json:"type"`
	Payload map[string]any `json:"payload"`
}

// ProtocolHandler handles the synchronization protocol messages.
type ProtocolHandler struct {
	gitManager *git.GitRepositoryManager
}

// NewProtocolHandler creates a new ProtocolHandler.
func NewProtocolHandler(gitManager *git.GitRepositoryManager) *ProtocolHandler {
	return &ProtocolHandler{
		gitManager: gitManager,
	}
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
