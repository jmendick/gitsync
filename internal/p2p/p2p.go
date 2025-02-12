package p2p

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/model"
	"github.com/jmendick/gitsync/internal/p2p/discovery"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
)

// Node represents the P2P node.
type Node struct {
	config          *config.Config
	listener        net.Listener
	peerDiscovery   *discovery.DiscoveryService
	protocolHandler *protocol.ProtocolHandler
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
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

	ctx, cancel := context.WithCancel(context.Background())
	node := &Node{
		config:          cfg,
		listener:        listener,
		peerDiscovery:   discService,
		protocolHandler: protocolHandler,
		ctx:             ctx,
		cancel:          cancel,
	}

	node.wg.Add(2)
	go node.startPeerDiscovery()
	go node.startListening()

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
