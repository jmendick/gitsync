package p2p

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/p2p/discovery"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
)

// ConnectionQuality represents connection quality metrics
type ConnectionQuality struct {
	Latency      time.Duration
	PacketLoss   float64
	Bandwidth    float64 // bytes per second
	RTTVariation float64
	LastMeasured time.Time
}

// BandwidthManager manages connection bandwidth
type BandwidthManager struct {
	maxBandwidth int64 // bytes per second
	currentUsage int64
	connections  map[string]*ConnectionQuality
	mu           sync.RWMutex
}

// Node represents the P2P node.
type Node struct {
	config          *config.Config
	listener        net.Listener
	peerDiscovery   *discovery.DiscoveryService
	protocolHandler *protocol.ProtocolHandler
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	bandwidthMgr    *BandwidthManager
	connQuality     map[string]*ConnectionQuality
	connQualityMu   sync.RWMutex
}

// NewNode creates a new P2P node.
func NewNode(cfg *config.Config, protocolHandler *protocol.ProtocolHandler) (*Node, error) {
	listener, err := net.Listen("tcp", cfg.GetListenAddress())
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}
	fmt.Printf("Listening on: %s\n", listener.Addr())

	discService, err := discovery.NewDiscoveryService(cfg)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to initialize discovery service: %w", err)
	}

	bandwidthMgr := &BandwidthManager{
		maxBandwidth: cfg.Network.BandwidthLimit,
		connections:  make(map[string]*ConnectionQuality),
	}

	ctx, cancel := context.WithCancel(context.Background())
	node := &Node{
		config:          cfg,
		listener:        listener,
		peerDiscovery:   discService,
		protocolHandler: protocolHandler,
		bandwidthMgr:    bandwidthMgr,
		connQuality:     make(map[string]*ConnectionQuality),
		ctx:             ctx,
		cancel:          cancel,
	}

	node.wg.Add(3)
	go node.startPeerDiscovery()
	go node.startListening()
	go node.startQualityMonitoring()

	return node, nil
}

// Close closes the P2P node and releases resources.
func (n *Node) Close() error {
	// Signal shutdown
	n.cancel()

	// Close listener to stop accepting new connections
	if n.listener != nil {
		n.listener.Close()
	}

	// Stop discovery service
	if n.peerDiscovery != nil {
		n.peerDiscovery.Stop()
	}

	// Wait for goroutines to finish
	n.wg.Wait()

	fmt.Println("P2P Node closed.")
	return nil
}

func (n *Node) startPeerDiscovery() {
	defer n.wg.Done()
	fmt.Println("Starting Peer Discovery...")

	for {
		select {
		case <-n.ctx.Done():
			return
		default:
			peers := n.peerDiscovery.DiscoverPeers()
			fmt.Printf("Discovered peers: %+v\n", peers)
			// Add delay between discovery cycles
			select {
			case <-n.ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
	}
}

func (n *Node) startListening() {
	defer n.wg.Done()
	fmt.Println("Listening for incoming connections...")

	for {
		select {
		case <-n.ctx.Done():
			return
		default:
			conn, err := n.listener.Accept()
			if err != nil {
				if n.ctx.Err() != nil {
					// Normal shutdown, don't print error
					return
				}
				fmt.Printf("Error accepting connection: %v\n", err)
				continue
			}
			go n.handleConnection(conn)
		}
	}
}

func (n *Node) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Create a context with cancel for this connection
	ctx, cancel := context.WithCancel(n.ctx)
	defer cancel()

	// Handle connection closure when node is shutting down
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	if err := n.protocolHandler.HandleMessage(conn); err != nil {
		fmt.Printf("Error handling message: %v\n", err)
	}
}

// SendSyncRequest sends a synchronization request to a peer.
func (n *Node) SendSyncRequest(ctx context.Context, peerInfo *model.PeerInfo, repoName string) error {
	for _, addr := range peerInfo.Addresses {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			fmt.Printf("Failed to connect to peer at %s: %v\n", addr, err)
			continue
		}
		defer conn.Close()

		if err := n.protocolHandler.SendSyncRequestMessage(conn, repoName); err != nil {
			fmt.Printf("Failed to send sync request to peer at %s: %v\n", addr, err)
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to connect to peer %s at any address", peerInfo.ID)
}

// GetPeerDiscovery returns the peer discovery service.
func (n *Node) GetPeerDiscovery() *discovery.DiscoveryService {
	return n.peerDiscovery
}

func (n *Node) startQualityMonitoring() {
	defer n.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.updateConnectionQualities()
		}
	}
}

func (n *Node) updateConnectionQualities() {
	n.connQualityMu.Lock()
	defer n.connQualityMu.Unlock()

	for peerID, quality := range n.connQuality {
		if time.Since(quality.LastMeasured) > 5*time.Minute {
			// Connection might be stale, measure quality
			if newQuality, err := n.measureConnectionQuality(peerID); err == nil {
				n.connQuality[peerID] = newQuality
			} else {
				// Connection might be dead
				delete(n.connQuality, peerID)
			}
		}
	}
}

func (n *Node) measureConnectionQuality(peerID string) (*ConnectionQuality, error) {
	peer := n.peerDiscovery.GetPeerByID(peerID)
	if peer == nil {
		return nil, fmt.Errorf("peer not found")
	}

	quality := &ConnectionQuality{
		LastMeasured: time.Now(),
	}

	// Measure RTT and packet loss
	rttSum := time.Duration(0)
	rttSamples := make([]time.Duration, 0, 5)
	packetsSent := 0
	packetsReceived := 0

	for i := 0; i < 5; i++ {
		start := time.Now()
		if err := n.sendPing(peer); err != nil {
			packetsSent++
			continue
		}
		rtt := time.Since(start)
		rttSum += rtt
		rttSamples = append(rttSamples, rtt)
		packetsSent++
		packetsReceived++
	}

	if packetsReceived == 0 {
		return nil, fmt.Errorf("no response from peer")
	}

	quality.Latency = rttSum / time.Duration(packetsReceived)
	quality.PacketLoss = 1 - (float64(packetsReceived) / float64(packetsSent))

	// Calculate RTT variation
	if len(rttSamples) > 1 {
		mean := float64(quality.Latency)
		var variance float64
		for _, rtt := range rttSamples {
			diff := float64(rtt) - mean
			variance += diff * diff
		}
		quality.RTTVariation = math.Sqrt(variance / float64(len(rttSamples)-1))
	}

	// Measure available bandwidth
	bandwidth, err := n.measureBandwidth(peer)
	if err != nil {
		fmt.Printf("Warning: Failed to measure bandwidth for peer %s: %v\n", peerID, err)
	} else {
		quality.Bandwidth = bandwidth
	}

	return quality, nil
}

func (n *Node) measureBandwidth(peer *model.PeerInfo) (float64, error) {
	// Implement bandwidth measurement
	// This could involve sending/receiving test data and measuring throughput
	// For now, return a placeholder implementation
	return 1000000, nil // 1 MB/s placeholder
}

func (n *Node) sendPing(peer *model.PeerInfo) error {
	// Try each address until one succeeds
	for _, addr := range peer.Addresses {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			continue
		}
		defer conn.Close()

		// Send ping message
		err = n.protocolHandler.SendHeartbeat(conn, "ping")
		if err != nil {
			continue
		}

		return nil
	}
	return fmt.Errorf("failed to ping peer at any address")
}

func (n *Node) GetConnectionQuality(peerID string) *ConnectionQuality {
	n.connQualityMu.RLock()
	defer n.connQualityMu.RUnlock()
	return n.connQuality[peerID]
}

// ConnectToPeer establishes a connection to a peer
func (n *Node) ConnectToPeer(peer *model.PeerInfo) (net.Conn, error) {
	for _, addr := range peer.Addresses {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			continue
		}
		return conn, nil
	}
	return nil, fmt.Errorf("failed to connect to peer %s at any address", peer.ID)
}

// ProtocolHandler returns the node's protocol handler
func (n *Node) ProtocolHandler() *protocol.ProtocolHandler {
	return n.protocolHandler
}

// Add file transfer functionality
func (n *Node) SendFile(ctx context.Context, peerID string, filePath string) error {
	peer := n.peerDiscovery.GetPeerByID(peerID)
	if peer == nil {
		return fmt.Errorf("peer %s not found", peerID)
	}

	conn, err := n.ConnectToPeer(peer)
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %w", err)
	}
	defer conn.Close()

	return n.protocolHandler.SendFile(conn, filePath)
}
