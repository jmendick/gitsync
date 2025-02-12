package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/model"
)

// DiscoveryService handles peer discovery.
type DiscoveryService struct {
	config      *config.Config
	peers       map[string]*model.PeerInfo
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	selfAddress string
}

// NewDiscoveryService creates a new DiscoveryService.
func NewDiscoveryService(cfg *config.Config) (*DiscoveryService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ds := &DiscoveryService{
		config:      cfg,
		peers:       make(map[string]*model.PeerInfo),
		ctx:         ctx,
		cancel:      cancel,
		selfAddress: cfg.GetListenAddress(),
	}

	// Start periodic peer discovery
	go ds.startPeriodicDiscovery()
	return ds, nil
}

// Stop stops the discovery service and releases resources.
func (ds *DiscoveryService) Stop() {
	ds.cancel()
	fmt.Println("Discovery Service stopped")
}

// DiscoverPeers discovers peers in the network.
func (ds *DiscoveryService) DiscoverPeers() []*model.PeerInfo {
	bootstrapPeers := ds.config.GetBootstrapPeers()
	discoveredPeers := make([]*model.PeerInfo, 0)

	// Try to connect to bootstrap peers
	for _, peerAddr := range bootstrapPeers {
		peerInfo := &model.PeerInfo{
			ID:        peerAddr, // Using address as ID for now
			Addresses: []string{peerAddr},
		}

		if ds.tryConnectPeer(peerInfo) {
			ds.addPeer(peerInfo)
			discoveredPeers = append(discoveredPeers, peerInfo)
		}
	}

	ds.mu.RLock()
	for _, peer := range ds.peers {
		discoveredPeers = append(discoveredPeers, peer)
	}
	ds.mu.RUnlock()

	return discoveredPeers
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
	return peer, exists
}

// Internal methods

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

func (ds *DiscoveryService) addPeer(peer *model.PeerInfo) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.peers[peer.ID] = peer
}

func (ds *DiscoveryService) tryConnectPeer(peer *model.PeerInfo) bool {
	// Try each address for the peer until one succeeds
	for _, addr := range peer.Addresses {
		// Create connection with timeout
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			fmt.Printf("Failed to connect to peer %s at address %s: %v\n", peer.ID, addr, err)
			continue
		}

		// Successfully connected
		fmt.Printf("Successfully connected to peer %s at address %s\n", peer.ID, addr)
		conn.Close() // Close the test connection
		return true
	}

	return false // All connection attempts failed
}
