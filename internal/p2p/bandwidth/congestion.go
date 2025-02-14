package bandwidth

import (
	"math"
	"sync"
	"time"
)

// CongestionController implements TCP-like congestion control
type CongestionController struct {
	windowSize float64
	ssthresh   float64
	state      congestionState
	rttStats   *RTTStats
	mu         sync.Mutex
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
		windowSize: 1024 * 10, // Start with 10KB
		ssthresh:   math.MaxFloat64,
		state:      slowStart,
		rttStats:   newRTTStats(100), // Keep last 100 samples
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
	return bytesInFlight < cc.windowSize
}

func (cc *CongestionController) OnAck(bytes int64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	switch cc.state {
	case slowStart:
		cc.windowSize += float64(bytes)
		if cc.windowSize >= cc.ssthresh {
			cc.state = congestionAvoidance
		}
	case congestionAvoidance:
		// Additive increase
		cc.windowSize += float64(bytes) * float64(bytes) / cc.windowSize
	case fastRecovery:
		cc.state = congestionAvoidance
	}
}

func (cc *CongestionController) OnLoss() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.ssthresh = math.Max(cc.windowSize/2, 1024*10)
	cc.windowSize = cc.ssthresh
	cc.state = congestionAvoidance
}

func (cc *CongestionController) OnTimeout() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.ssthresh = math.Max(cc.windowSize/2, 1024*10)
	cc.windowSize = 1024 * 10 // Reset to initial window size
	cc.state = slowStart
}

func (cc *CongestionController) UpdateRTT(rtt time.Duration) {
	cc.rttStats.AddSample(rtt)
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
