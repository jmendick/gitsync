package testutil

import (
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/model"
)

// MockConfig implements the Config interface
type MockConfig struct {
	*config.Config // Embed Config to inherit methods
}

// NewMockConfig creates a new MockConfig with default values
func NewMockConfig() *MockConfig {
	return &MockConfig{
		Config: &config.Config{
			ListenAddress:  "127.0.0.1:0",
			BootstrapPeers: []string{},
			RepositoryDir:  "./test-repos",
		},
	}
}

// NewTestServer creates a TCP server for testing with custom message handling
func NewTestServer(t *testing.T, handler func(net.Conn)) (string, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	ready := make(chan struct{})
	go func() {
		close(ready)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if handler != nil {
			handler(conn)
		}
		conn.Close()
	}()

	<-ready // Wait for goroutine to start
	return listener.Addr().String(), func() {
		listener.Close()
	}
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error(message)
	return false
}

// SendTestMessage sends a message to a writer for testing
func SendTestMessage(w io.Writer, messageType string, payload interface{}) error {
	msg := struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}{
		Type:    messageType,
		Payload: payload,
	}
	return json.NewEncoder(w).Encode(msg)
}

// CreateTestPeerInfo creates a PeerInfo instance for testing
func CreateTestPeerInfo(id string, addresses ...string) *model.PeerInfo {
	if len(addresses) == 0 {
		addresses = []string{"127.0.0.1:0"}
	}
	return &model.PeerInfo{
		ID:        id,
		Addresses: addresses,
	}
}

// AssertEventually repeatedly checks a condition with timeout
func AssertEventually(t *testing.T, condition func() bool, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("Condition not met within timeout")
}
