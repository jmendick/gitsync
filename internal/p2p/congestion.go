package p2p

import (
	"math"
	"sync"
	"time"
)

// CongestionState represents the current congestion control state
type CongestionState int

const (
	SlowStart CongestionState = iota
	CongestionAvoidance
	FastRecovery
)

// CongestionController implements TCP-like congestion control
type CongestionController struct {
	cwnd           float64 // Congestion window size in bytes
	ssthresh       float64 // Slow start threshold
	state          CongestionState
	rtt            time.Duration
	rttVar         time.Duration
	lastWindowSize float64
	mu             sync.RWMutex
}

func NewCongestionController() *CongestionController {
	return &CongestionController{
		cwnd:     1024 * 10, // Start with 10KB window
		ssthresh: math.MaxFloat64,
		state:    SlowStart,
	}
}

// UpdateRTT updates RTT estimates using Karn's algorithm
func (cc *CongestionController) UpdateRTT(sampleRTT time.Duration) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if cc.rtt == 0 {
		// First RTT measurement
		cc.rtt = sampleRTT
		cc.rttVar = sampleRTT / 2
	} else {
		// EWMA for RTT estimation
		alpha := 0.125
		beta := 0.25
		diff := float64(sampleRTT - cc.rtt)
		cc.rttVar = time.Duration((1-beta)*float64(cc.rttVar) + beta*math.Abs(diff))
		cc.rtt = time.Duration((1-alpha)*float64(cc.rtt) + alpha*float64(sampleRTT))
	}
}

// OnAck handles successful packet acknowledgment
func (cc *CongestionController) OnAck(bytes int64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	switch cc.state {
	case SlowStart:
		cc.cwnd += float64(bytes)
		if cc.cwnd >= cc.ssthresh {
			cc.state = CongestionAvoidance
		}
	case CongestionAvoidance:
		// Additive increase
		cc.cwnd += float64(bytes) * float64(bytes) / cc.cwnd
	case FastRecovery:
		cc.cwnd = cc.ssthresh
		cc.state = CongestionAvoidance
	}

	cc.lastWindowSize = cc.cwnd
}

// OnLoss handles packet loss events
func (cc *CongestionController) OnLoss() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.ssthresh = math.Max(cc.cwnd/2, 1024*10) // Minimum 10KB
	cc.cwnd = cc.ssthresh
	cc.state = CongestionAvoidance
}

// OnTimeout handles timeout events
func (cc *CongestionController) OnTimeout() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.ssthresh = math.Max(cc.cwnd/2, 1024*10)
	cc.cwnd = 1024 * 10 // Reset to initial window
	cc.state = SlowStart
}

// GetWindow returns the current congestion window size
func (cc *CongestionController) GetWindow() float64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.cwnd
}

// GetRTT returns current RTT estimate
func (cc *CongestionController) GetRTT() time.Duration {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.rtt
}

// ShouldBackoff determines if transmission should be delayed
func (cc *CongestionController) ShouldBackoff(bytesInFlight float64) bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return bytesInFlight >= cc.cwnd
}

// CalculateBackoff calculates how long to wait before retrying
func (cc *CongestionController) CalculateBackoff() time.Duration {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	// Base RTO on RTT and variance (RFC 6298)
	rto := cc.rtt + 4*cc.rttVar

	// Bounds check
	if rto < 1*time.Second {
		rto = 1 * time.Second
	}
	if rto > 60*time.Second {
		rto = 60 * time.Second
	}

	return rto
}

// GetTransmissionQuota returns how many bytes can be sent
func (cc *CongestionController) GetTransmissionQuota(bytesInFlight float64) float64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	available := cc.cwnd - bytesInFlight
	if available < 0 {
		return 0
	}
	return available
}
