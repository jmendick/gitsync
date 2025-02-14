package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
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
	FirstSeen      time.Time      `json:"first_seen"`
	Statistics     PeerStatistics `json:"statistics"`
}

// PeerStatistics tracks peer reliability metrics
type PeerStatistics struct {
	SuccessfulSyncs  int       `json:"successful_syncs"`
	FailedSyncs      int       `json:"failed_syncs"`
	AverageLatency   float64   `json:"average_latency"`
	LastSyncTime     time.Time `json:"last_sync_time"`
	ReliabilityScore float64   `json:"reliability_score"`
	SharedRepos      []string  `json:"shared_repos"`
	UpTime           float64   `json:"up_time"`
	BandwidthScore   float64   `json:"bandwidth_score"`
}

// GetPeerScore returns a weighted score for peer selection
func (ps *PeerStatistics) GetPeerScore() float64 {
	if ps.SuccessfulSyncs+ps.FailedSyncs == 0 {
		return 0.5 // Default score for new peers
	}

	// Calculate basic reliability from sync success rate
	syncRate := float64(ps.SuccessfulSyncs) / float64(ps.SuccessfulSyncs+ps.FailedSyncs)

	// Penalize high latency (normalize to 0-1 range, assuming 1000ms as max acceptable)
	latencyScore := 1.0 - math.Min(1.0, ps.AverageLatency/1000.0)

	// Factor in recency of last sync
	recencyScore := 1.0
	if !ps.LastSyncTime.IsZero() {
		hoursSinceLastSync := time.Since(ps.LastSyncTime).Hours()
		recencyScore = math.Max(0, 1.0-hoursSinceLastSync/24.0) // Decay over 24 hours
	}

	// Combine scores with weights
	return (syncRate * 0.4) + (latencyScore * 0.3) + (recencyScore * 0.2) + (ps.UpTime * 0.1)
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

	// Persistence-related fields
	storageDir string
	peerCache  *peerCacheFile
}

type peerCacheFile struct {
	LastUpdated time.Time             `json:"last_updated"`
	Peers       map[string]*PeerState `json:"peers"`
	RoutingInfo map[string][]string   `json:"routing_info"`
	Statistics  discoveryStatistics   `json:"statistics"`
}

type discoveryStatistics struct {
	TotalPeersFound   int            `json:"total_peers_found"`
	LastDiscoveryTime time.Time      `json:"last_discovery_time"`
	ActivePeersCount  int            `json:"active_peers_count"`
	ZoneDistribution  map[string]int `json:"zone_distribution"`
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

	// Initialize persistence if enabled
	if cfg.GetDiscovery().PersistenceEnabled {
		if err := ds.initializeStorage(); err != nil {
			return nil, fmt.Errorf("failed to initialize storage: %w", err)
		}
	}

	// Load cached peer data if available
	if err := ds.loadPeerCache(); err != nil {
		fmt.Printf("Warning: Failed to load peer cache: %v\n", err)
	}

	// Start background tasks
	go ds.startPeriodicDiscovery()
	go ds.startHeartbeatMonitor()
	go ds.startPeerStateCleanup()
	go ds.startPersistenceTask()

	return ds, nil
}

func (ds *DiscoveryService) initializeStorage() error {
	discoveryConfig := ds.config.GetDiscovery()
	if discoveryConfig.StorageDir == "" {
		ds.storageDir = filepath.Join(ds.config.GetRepositoryDir(), ".gitsync", "peers")
	} else {
		ds.storageDir = discoveryConfig.StorageDir
	}

	if err := os.MkdirAll(ds.storageDir, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	return nil
}

func (ds *DiscoveryService) loadPeerCache() error {
	if !ds.config.GetDiscovery().PersistenceEnabled {
		return nil
	}

	cacheFile := filepath.Join(ds.storageDir, "peer_cache.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			ds.peerCache = &peerCacheFile{
				LastUpdated: time.Now(),
				Peers:       make(map[string]*PeerState),
				RoutingInfo: make(map[string][]string),
				Statistics: discoveryStatistics{
					ZoneDistribution: make(map[string]int),
				},
			}
			return nil
		}
		return err
	}

	ds.peerCache = &peerCacheFile{}
	if err := json.Unmarshal(data, ds.peerCache); err != nil {
		return fmt.Errorf("failed to unmarshal peer cache: %w", err)
	}

	// Load cached peers into memory
	ds.mu.Lock()
	for id, state := range ds.peerCache.Peers {
		// Only load peers that are within the cache time window
		if time.Since(state.LastSeen) <= ds.config.GetDiscovery().PeerCacheTime {
			ds.peers[id] = state
		}
	}
	ds.mu.Unlock()

	return nil
}

func (ds *DiscoveryService) savePeerCache() error {
	if !ds.config.GetDiscovery().PersistenceEnabled {
		return nil
	}

	ds.mu.RLock()
	ds.peerCache.LastUpdated = time.Now()
	ds.peerCache.Peers = ds.peers
	ds.mu.RUnlock()

	ds.routingTableMu.RLock()
	ds.peerCache.RoutingInfo = ds.routingTable
	ds.routingTableMu.RUnlock()

	// Update statistics
	ds.peerCache.Statistics.ActivePeersCount = len(ds.peers)
	ds.peerCache.Statistics.LastDiscoveryTime = time.Now()

	// Calculate zone distribution
	zoneDistribution := make(map[string]int)
	for _, state := range ds.peers {
		if len(state.Info.Addresses) > 0 {
			zone := ds.calculateZone(state.Info.Addresses[0])
			zoneDistribution[zone]++
		}
	}
	ds.peerCache.Statistics.ZoneDistribution = zoneDistribution

	data, err := json.MarshalIndent(ds.peerCache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal peer cache: %w", err)
	}

	cacheFile := filepath.Join(ds.storageDir, "peer_cache.json")
	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write peer cache: %w", err)
	}

	return nil
}

func (ds *DiscoveryService) startPersistenceTask() {
	if !ds.config.GetDiscovery().PersistenceEnabled {
		return
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ds.ctx.Done():
			// Save one last time before shutting down
			ds.savePeerCache()
			return
		case <-ticker.C:
			if err := ds.savePeerCache(); err != nil {
				fmt.Printf("Error saving peer cache: %v\n", err)
			}
		}
	}
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

// GetPeerByID returns a peer's info by their ID
func (ds *DiscoveryService) GetPeerByID(peerID string) *model.PeerInfo {
	info, exists := ds.GetPeerInfo(peerID)
	if !exists {
		return nil
	}
	return info
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

// UpdatePeerStatistics updates statistics for a peer
func (ds *DiscoveryService) UpdatePeerStatistics(peerID string, syncSuccess bool, latency time.Duration) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if peer, exists := ds.peers[peerID]; exists {
		if syncSuccess {
			peer.Statistics.SuccessfulSyncs++
		} else {
			peer.Statistics.FailedSyncs++
		}

		// Update average latency with exponential moving average
		alpha := 0.2 // Smoothing factor
		if peer.Statistics.AverageLatency == 0 {
			peer.Statistics.AverageLatency = float64(latency.Milliseconds())
		} else {
			peer.Statistics.AverageLatency = (alpha * float64(latency.Milliseconds())) +
				((1 - alpha) * peer.Statistics.AverageLatency)
		}

		peer.Statistics.LastSyncTime = time.Now()
		peer.Statistics.ReliabilityScore = peer.Statistics.GetPeerScore()

		// Update uptime based on heartbeat history
		peer.Statistics.UpTime = ds.calculatePeerUptime(peerID)

		// Calculate bandwidth score based on recent transfers
		peer.Statistics.BandwidthScore = ds.calculateBandwidthScore(peerID)
	}
}

// calculatePeerUptime calculates the peer's uptime ratio
func (ds *DiscoveryService) calculatePeerUptime(peerID string) float64 {
	peer, exists := ds.peers[peerID]
	if !exists {
		return 0
	}

	if peer.FirstSeen.IsZero() {
		return 1.0 // New peer
	}

	totalTime := time.Since(peer.FirstSeen)
	activeTime := totalTime - peer.Statistics.getDowntime()

	return float64(activeTime) / float64(totalTime)
}

// calculateBandwidthScore calculates a score based on transfer speeds
func (ds *DiscoveryService) calculateBandwidthScore(peerID string) float64 {
	// TODO: Implement bandwidth tracking
	return 1.0
}

// getDowntime returns the total downtime for a peer
func (ps *PeerStatistics) getDowntime() time.Duration {
	// TODO: Implement downtime tracking based on missed heartbeats
	return 0
}

// GetPeerStatistics returns the statistics for a specific peer
func (ds *DiscoveryService) GetPeerStatistics(peerID string) (*PeerStatistics, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if peer, exists := ds.peers[peerID]; exists {
		return &peer.Statistics, nil
	}
	return nil, fmt.Errorf("peer not found")
}

// GetDiscoveryStatistics returns overall discovery statistics
func (ds *DiscoveryService) GetDiscoveryStatistics() discoveryStatistics {
	if ds.peerCache != nil {
		return ds.peerCache.Statistics
	}
	return discoveryStatistics{}
}
