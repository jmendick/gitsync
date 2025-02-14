package bandwidth

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// BandwidthManager defines the interface for bandwidth management
type BandwidthManager interface {
	AcquireBandwidth(peerID string, bytes int64) error
	OnTransferComplete(peerID string, bytes int64, duration time.Duration, success bool)
	GetConnectionQuality(peerID string) *ConnectionQuality
}

// Manager handles bandwidth allocation and limits
type Manager struct {
	globalLimit    int64
	buckets        map[string]*rate.Limiter
	bucketsMu      sync.RWMutex
	bytesInFlight  *sync.Map
	connections    map[string]*ConnectionQuality
	congestionCtrl map[string]*CongestionController
	mu             sync.RWMutex
}

// ConnectionQuality tracks connection metrics
type ConnectionQuality struct {
	Bandwidth        float64
	Latency          time.Duration
	PacketLoss       float64
	LastMeasured     time.Time
	ReliabilityScore float64
}

// NewManager creates a new bandwidth manager
func NewManager(globalLimit int64) *Manager {
	if globalLimit <= 0 {
		globalLimit = math.MaxInt64
	}
	return &Manager{
		globalLimit:    globalLimit,
		buckets:        make(map[string]*rate.Limiter),
		bytesInFlight:  &sync.Map{},
		connections:    make(map[string]*ConnectionQuality),
		congestionCtrl: make(map[string]*CongestionController),
	}
}

// NewMockManager creates a mock bandwidth manager for testing
func NewMockManager() *Manager {
	return &Manager{
		globalLimit:    math.MaxInt64, // Effectively unlimited
		buckets:        make(map[string]*rate.Limiter),
		bytesInFlight:  &sync.Map{},
		connections:    make(map[string]*ConnectionQuality),
		congestionCtrl: make(map[string]*CongestionController),
	}
}

// AcquireBandwidth attempts to acquire bandwidth for a transfer
func (bm *Manager) AcquireBandwidth(peerID string, bytes int64) error {
	bm.bucketsMu.RLock()
	limiter, exists := bm.buckets[peerID]
	bm.bucketsMu.RUnlock()

	if !exists {
		bm.bucketsMu.Lock()
		// Set the burst size to max(bytes, globalLimit/10) to handle large transfers
		burstSize := max(float64(bytes), float64(bm.globalLimit)/10)
		limiter = rate.NewLimiter(rate.Limit(bm.globalLimit/10), int(burstSize))
		bm.buckets[peerID] = limiter
		bm.bucketsMu.Unlock()
	}

	// Get congestion controller
	bm.mu.RLock()
	cc, exists := bm.congestionCtrl[peerID]
	bm.mu.RUnlock()

	if !exists {
		bm.mu.Lock()
		cc = NewCongestionController()
		bm.congestionCtrl[peerID] = cc
		bm.mu.Unlock()
	}

	// Check congestion window
	inFlight, _ := bm.bytesInFlight.LoadOrStore(peerID, float64(0))
	bytesInFlight := inFlight.(float64)
	if !cc.CanSend(bytesInFlight + float64(bytes)) {
		return ErrCongestionLimit
	}

	// Use context with timeout to ensure rate limiting actually takes effect
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(float64(time.Second)*float64(bytes)/float64(bm.globalLimit)))
	defer cancel()

	// Try to acquire bandwidth
	if err := limiter.WaitN(ctx, int(bytes)); err != nil {
		return err
	}

	// Update bytes in flight
	bm.bytesInFlight.Store(peerID, bytesInFlight+float64(bytes))
	return nil
}

// OnTransferComplete updates metrics after a transfer completes
func (bm *Manager) OnTransferComplete(peerID string, bytes int64, duration time.Duration, success bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	cc := bm.congestionCtrl[peerID]
	quality, exists := bm.connections[peerID]
	if !exists {
		quality = &ConnectionQuality{}
		bm.connections[peerID] = quality
	}

	if cc != nil {
		cc.UpdateRTT(duration)
		if success {
			cc.OnAck(bytes)
		} else {
			cc.OnLoss()
		}
	}

	// Update bytes in flight
	inFlight, ok := bm.bytesInFlight.Load(peerID)
	if ok {
		bytesInFlight := inFlight.(float64)
		bm.bytesInFlight.Store(peerID, max(0, bytesInFlight-float64(bytes)))
	}

	// Update connection quality metrics
	quality.LastMeasured = time.Now()
	if success {
		quality.Bandwidth = float64(bytes) / duration.Seconds()
		if cc != nil {
			quality.Latency = cc.GetRTT()
		}
		// Update packet loss using exponential moving average
		quality.PacketLoss = quality.PacketLoss*0.8 + 0.0*0.2
	} else {
		// Update packet loss on failure
		quality.PacketLoss = quality.PacketLoss*0.8 + 1.0*0.2
	}

	// Update reliability score
	quality.ReliabilityScore = calculateReliabilityScore(quality)
}

func (bm *Manager) GetConnectionQuality(peerID string) *ConnectionQuality {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.connections[peerID]
}

// GetCongestionController returns the congestion controller for a peer (used in testing)
func (bm *Manager) GetCongestionController(peerID string) *CongestionController {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.congestionCtrl[peerID]
}

func calculateReliabilityScore(q *ConnectionQuality) float64 {
	if q == nil {
		return 0
	}

	// Normalize metrics to 0-1 range
	bandwidthScore := math.Min(1.0, q.Bandwidth/1e6)             // Normalize to 1MB/s
	latencyScore := 1 - math.Min(1.0, float64(q.Latency)/1000.0) // Normalize to 1s
	reliabilityScore := 1 - q.PacketLoss

	// Weight the factors
	return (bandwidthScore*0.3 + latencyScore*0.3 + reliabilityScore*0.4)
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
