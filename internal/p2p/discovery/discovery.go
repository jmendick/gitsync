package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/util"
)

// PeerState tracks the state of a peer
type PeerState struct {
	Info           *model.PeerInfo
	LastSeen       time.Time
	Repositories   []string
	ActiveChannels int
}

// DiscoveryService handles peer discovery.
type DiscoveryService struct {
	config   *config.Config
	peers    map[string]*PeerState
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	selfInfo *model.PeerInfo

	// DHT-like routing table (simplified)
	routingTable   map[string][]string // maps region/zone to peer IDs
	routingTableMu sync.RWMutex
}

// NewDiscoveryService creates a new DiscoveryService.
func NewDiscoveryService(cfg *config.Config) (*DiscoveryService, error) {
	ctx, cancel := context.WithCancel(context.Background())

	selfInfo := &model.PeerInfo{
		ID:        util.GeneratePeerID(),
		Addresses: []string{cfg.GetListenAddress()},
	}

	ds := &DiscoveryService{
		config:       cfg,
		peers:        make(map[string]*PeerState),
		ctx:          ctx,
		cancel:       cancel,
		selfInfo:     selfInfo,
		routingTable: make(map[string][]string),
	}

	// Start background tasks
	go ds.startPeriodicDiscovery()
	go ds.startHeartbeatMonitor()
	go ds.startPeerStateCleanup()

	return ds, nil
}

// Stop stops the discovery service and releases resources.
func (ds *DiscoveryService) Stop() {
	ds.cancel()
	fmt.Println("Discovery Service stopped")
}

// DiscoverPeers discovers peers in the network using the enhanced protocol
func (ds *DiscoveryService) DiscoverPeers() []*model.PeerInfo {
	// Start with bootstrap peers
	bootstrapPeers := ds.config.GetBootstrapPeers()

	// Store discovered peers
	var discoveredPeers []*model.PeerInfo

	// Use a wait group to handle concurrent peer discovery
	var wg sync.WaitGroup
	peersChan := make(chan *model.PeerInfo, len(bootstrapPeers))

	for _, addr := range bootstrapPeers {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			if peer := ds.queryPeer(address); peer != nil {
				peersChan <- peer
				// Also add the bootstrap peer itself
				bootstrapPeer := &model.PeerInfo{
					ID:        util.GeneratePeerID(), // Generate ID for bootstrap peer
					Addresses: []string{address},
				}
				ds.addOrUpdatePeer(bootstrapPeer)
				discoveredPeers = append(discoveredPeers, bootstrapPeer)
			}
		}(addr)
	}

	// Wait for all queries to complete
	go func() {
		wg.Wait()
		close(peersChan)
	}()

	// Collect discovered peers
	for peer := range peersChan {
		ds.addOrUpdatePeer(peer)
		discoveredPeers = append(discoveredPeers, peer)
		ds.updateRoutingTable(peer)
	}

	// Also include any existing peers in our peer list
	ds.mu.RLock()
	for _, state := range ds.peers {
		discoveredPeers = append(discoveredPeers, state.Info)
	}
	ds.mu.RUnlock()

	return discoveredPeers
}

func (ds *DiscoveryService) queryPeer(addr string) *model.PeerInfo {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil
	}
	defer conn.Close()

	// Send PeerListRequest
	request := map[string]interface{}{
		"type":    "PEER_LIST_REQUEST",
		"payload": map[string]interface{}{},
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		return nil
	}

	// Read response
	var response struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}

	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return nil
	}

	if response.Type == "PEER_LIST_RESPONSE" {
		var peerList struct {
			Peers []model.PeerInfo `json:"peers"`
		}
		if err := json.Unmarshal(response.Payload, &peerList); err != nil {
			return nil
		}

		// Return the first peer from the response
		if len(peerList.Peers) > 0 {
			peer := &peerList.Peers[0]
			ds.addOrUpdatePeer(peer)
			return peer
		}
	}

	return nil
}

func (ds *DiscoveryService) updateRoutingTable(peer *model.PeerInfo) {
	ds.routingTableMu.Lock()
	defer ds.routingTableMu.Unlock()

	// Simple zone-based routing (can be enhanced with proper DHT implementation)
	zone := ds.calculateZone(peer.Addresses[0])
	if _, exists := ds.routingTable[zone]; !exists {
		ds.routingTable[zone] = make([]string, 0)
	}
	ds.routingTable[zone] = append(ds.routingTable[zone], peer.ID)
}

func (ds *DiscoveryService) addOrUpdatePeer(peer *model.PeerInfo) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	state, exists := ds.peers[peer.ID]
	if !exists {
		state = &PeerState{
			Info:     peer,
			LastSeen: time.Now(),
		}
		ds.peers[peer.ID] = state
	} else {
		state.Info = peer
		state.LastSeen = time.Now()
	}
}

func (ds *DiscoveryService) startPeriodicDiscovery() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ds.ctx.Done():
			return
		case <-ticker.C:
			ds.DiscoverPeers()
		}
	}
}

func (ds *DiscoveryService) startHeartbeatMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ds.ctx.Done():
			return
		case <-ticker.C:
			ds.sendHeartbeats()
		}
	}
}

func (ds *DiscoveryService) sendHeartbeats() {
	ds.mu.RLock()
	peers := make([]*PeerState, 0, len(ds.peers))
	for _, peer := range ds.peers {
		peers = append(peers, peer)
	}
	ds.mu.RUnlock()

	for _, peer := range peers {
		for _, addr := range peer.Info.Addresses {
			if err := ds.sendHeartbeat(addr); err != nil {
				continue
			}
			break
		}
	}
}

func (ds *DiscoveryService) startPeerStateCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ds.ctx.Done():
			return
		case <-ticker.C:
			ds.cleanupStaleHosts()
		}
	}
}

func (ds *DiscoveryService) cleanupStaleHosts() {
	threshold := time.Now().Add(-10 * time.Minute)

	ds.mu.Lock()
	defer ds.mu.Unlock()

	for id, state := range ds.peers {
		if state.LastSeen.Before(threshold) {
			delete(ds.peers, id)
		}
	}
}

// AnnounceSelf announces this node's presence to the network
func (ds *DiscoveryService) AnnounceSelf() error {
	// Announce to bootstrap peers
	for _, peerAddr := range ds.config.GetBootstrapPeers() {
		fmt.Printf("Announcing presence to bootstrap peer: %s\n", peerAddr)
	}

	return nil
}

// GetPeerInfo returns information about a specific peer
func (ds *DiscoveryService) GetPeerInfo(peerID string) (*model.PeerInfo, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	peer, exists := ds.peers[peerID]
	if !exists {
		return nil, false
	}
	return peer.Info, true
}

// Helper methods

func (ds *DiscoveryService) calculateZone(addr string) string {
	// Simple zone calculation based on IP address
	// This can be enhanced with proper network topology awareness
	host, _, _ := net.SplitHostPort(addr)
	return host[:strings.LastIndex(host, ".")]
}

func (ds *DiscoveryService) sendHeartbeat(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	heartbeat := map[string]interface{}{
		"type": "HEARTBEAT",
		"payload": map[string]interface{}{
			"peer_id":   ds.selfInfo.ID,
			"timestamp": time.Now(),
			"status":    "ACTIVE",
		},
	}

	return json.NewEncoder(conn).Encode(heartbeat)
}
