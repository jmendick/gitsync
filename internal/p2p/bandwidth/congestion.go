package bandwidth

import (
	"math"
	"sync"
	"time"
)

// CongestionController implements TCP-like congestion control
type CongestionController struct {
	windowSize  float64
	ssthresh    float64
	state       congestionState
	rttStats    *RTTStats
	mu          sync.Mutex
	lastUpdated time.Time
}

type congestionState int

const (
	slowStart congestionState = iota
	congestionAvoidance
	fastRecovery
)

// RTTStats tracks round-trip time statistics
type RTTStats struct {
	minRTT     time.Duration
	smoothRTT  time.Duration
	rttVar     time.Duration
	samples    []time.Duration
	maxSamples int
	mu         sync.Mutex
}

func NewCongestionController() *CongestionController {
	return &CongestionController{
		windowSize:  1024 * 10, // Start with 10KB
		ssthresh:    math.MaxFloat64,
		state:       slowStart,
		rttStats:    newRTTStats(100), // Keep last 100 samples
		lastUpdated: time.Now(),
	}
}

func newRTTStats(maxSamples int) *RTTStats {
	return &RTTStats{
		samples:    make([]time.Duration, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

func (cc *CongestionController) CanSend(bytesInFlight float64) bool {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Add time-based adjustment to prevent instant transfers
	elapsed := time.Since(cc.lastUpdated)
	if elapsed < 10*time.Millisecond {
		return false
	}

	canSend := bytesInFlight < cc.windowSize
	if canSend {
		cc.lastUpdated = time.Now()
	}
	return canSend
}

func (cc *CongestionController) OnAck(bytes int64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	switch cc.state {
	case slowStart:
		// Double window size each RTT
		cc.windowSize = math.Min(cc.windowSize+float64(bytes), cc.ssthresh)
		if cc.windowSize >= cc.ssthresh {
			cc.state = congestionAvoidance
		}
	case congestionAvoidance:
		// Linear increase
		cc.windowSize += float64(bytes) * (float64(bytes) / cc.windowSize)
	case fastRecovery:
		cc.state = congestionAvoidance
		cc.windowSize = cc.ssthresh
	}
	cc.lastUpdated = time.Now()
}

func (cc *CongestionController) OnLoss() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Multiplicative decrease
	cc.ssthresh = math.Max(cc.windowSize/2, 1024*10)
	cc.windowSize = cc.ssthresh
	cc.state = congestionAvoidance
	cc.lastUpdated = time.Now()
}

func (cc *CongestionController) OnTimeout() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// More aggressive reduction on timeout
	cc.ssthresh = math.Max(cc.windowSize/2, 1024*10)
	cc.windowSize = 1024 * 10 // Reset to initial window size
	cc.state = slowStart
	cc.lastUpdated = time.Now()
}

func (cc *CongestionController) UpdateRTT(rtt time.Duration) {
	if rtt > 0 {
		cc.rttStats.AddSample(rtt)
	}

	// Adjust window based on RTT
	cc.mu.Lock()
	defer cc.mu.Unlock()

	smoothedRTT := cc.rttStats.GetSmoothedRTT()
	if smoothedRTT > 0 {
		// Adjust window inversely to RTT changes
		rttFactor := float64(cc.rttStats.GetMinRTT()) / float64(smoothedRTT)
		cc.windowSize *= rttFactor
	}
}

func (cc *CongestionController) GetRTT() time.Duration {
	return cc.rttStats.GetSmoothedRTT()
}

// RTTStats methods
func (rs *RTTStats) AddSample(rtt time.Duration) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Update min RTT
	if rs.minRTT == 0 || rtt < rs.minRTT {
		rs.minRTT = rtt
	}

	// Update smoothed RTT using EWMA
	if rs.smoothRTT == 0 {
		rs.smoothRTT = rtt
		rs.rttVar = rtt / 2
	} else {
		alpha := 0.125 // EWMA weight for smoothed RTT
		beta := 0.25   // EWMA weight for RTT variance

		diff := time.Duration(math.Abs(float64(rtt - rs.smoothRTT)))
		rs.rttVar = time.Duration(float64(rs.rttVar)*(1-beta) + float64(diff)*beta)
		rs.smoothRTT = time.Duration(float64(rs.smoothRTT)*(1-alpha) + float64(rtt)*alpha)
	}

	// Add to samples
	rs.samples = append(rs.samples, rtt)
	if len(rs.samples) > rs.maxSamples {
		rs.samples = rs.samples[1:]
	}
}

func (rs *RTTStats) GetSmoothedRTT() time.Duration {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.smoothRTT
}

func (rs *RTTStats) GetRTTVariance() time.Duration {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.rttVar
}

func (rs *RTTStats) GetMinRTT() time.Duration {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.minRTT
}
