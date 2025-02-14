package p2p

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jmendick/gitsync/internal/p2p/bandwidth"
)

func TestBandwidthManager(t *testing.T) {
	t.Run("RateLimiting", func(t *testing.T) {
		bm := bandwidth.NewManager(1000) // 1KB/s limit
		peerID := "test-peer"

		// Try to acquire more than allowed in one second
		start := time.Now()
		err := bm.AcquireBandwidth(peerID, 1500)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Expected successful bandwidth acquisition, got error: %v", err)
		}
		if duration < 1500*time.Millisecond {
			t.Errorf("Rate limiting not working: acquired %d bytes in %v, expected at least 1.5s", 1500, duration)
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		bm := bandwidth.NewManager(10000) // 10KB/s
		peerID := "test-peer"

		const numGoroutines = 5
		const bytesPerGoroutine = 2000 // 2KB per goroutine

		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		start := time.Now()

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				err := bm.AcquireBandwidth(peerID, bytesPerGoroutine)
				if err != nil {
					t.Errorf("Failed to acquire bandwidth: %v", err)
				}
			}()
		}

		wg.Wait()
		duration := time.Since(start)
		expectedMinDuration := time.Duration(float64(numGoroutines*bytesPerGoroutine) / 10000 * float64(time.Second))

		if duration < expectedMinDuration {
			t.Errorf("Rate limiting not effective: took %v, expected at least %v", duration, expectedMinDuration)
		}
	})

	t.Run("PeerIsolation", func(t *testing.T) {
		bm := bandwidth.NewManager(2000) // 2KB/s total
		peer1 := "peer1"
		peer2 := "peer2"

		start := time.Now()
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			err := bm.AcquireBandwidth(peer1, 1000)
			if err != nil {
				t.Errorf("Peer1 failed to acquire bandwidth: %v", err)
			}
		}()

		go func() {
			defer wg.Done()
			err := bm.AcquireBandwidth(peer2, 1000)
			if err != nil {
				t.Errorf("Peer2 failed to acquire bandwidth: %v", err)
			}
		}()

		wg.Wait()
		duration := time.Since(start)

		// Should complete in ~1s since peers have separate limits
		if duration >= 2*time.Second {
			t.Error("Peer bandwidth limits are not properly isolated")
		}
	})
}

func TestBandwidthManagerWithCongestion(t *testing.T) {
	t.Run("TransferWithCongestionControl", func(t *testing.T) {
		bm := bandwidth.NewManager(1000000) // 1MB/s
		peerID := "test-peer"

		// Test slow start: each successful transfer should allow more bandwidth
		prevDuration := time.Duration(0)
		for i := 0; i < 5; i++ {
			start := time.Now()
			err := bm.AcquireBandwidth(peerID, 1024*2)
			if err != nil {
				t.Fatalf("Failed to acquire bandwidth: %v", err)
			}
			duration := time.Since(start)

			// In slow start, each successful transfer should be faster
			if i > 0 && duration >= prevDuration {
				t.Errorf("Transfer %d not faster than previous: %v >= %v", i, duration, prevDuration)
			}
			prevDuration = duration

			bm.OnTransferComplete(peerID, 1024*2, 100*time.Millisecond, true)
		}
	})

	t.Run("CongestionAvoidance", func(t *testing.T) {
		bm := bandwidth.NewManager(1000000)
		peerID := "test-peer"

		// Build up window with successful transfers
		for i := 0; i < 10; i++ {
			err := bm.AcquireBandwidth(peerID, 1024*10)
			if err != nil {
				t.Fatalf("Failed to acquire bandwidth: %v", err)
			}
			bm.OnTransferComplete(peerID, 1024*10, 50*time.Millisecond, true)
		}

		// Additional transfers should show linear window increase
		var durations []time.Duration
		for i := 0; i < 3; i++ {
			start := time.Now()
			err := bm.AcquireBandwidth(peerID, 1024*10)
			if err != nil {
				t.Fatalf("Failed to acquire bandwidth: %v", err)
			}
			durations = append(durations, time.Since(start))
			bm.OnTransferComplete(peerID, 1024*10, 50*time.Millisecond, true)
		}

		// In congestion avoidance, window increases linearly
		// Check that improvement in transfer times is roughly linear
		if len(durations) > 2 {
			firstImprovement := float64(durations[0] - durations[1])
			secondImprovement := float64(durations[1] - durations[2])
			if math.Abs(firstImprovement-secondImprovement) > float64(50*time.Millisecond) {
				t.Error("Window increase in congestion avoidance not linear")
			}
		}
	})

	t.Run("LossResponse", func(t *testing.T) {
		bm := bandwidth.NewManager(1000000)
		peerID := "test-peer"

		// Build up window with successful transfers
		for i := 0; i < 5; i++ {
			bm.AcquireBandwidth(peerID, 1024)
			bm.OnTransferComplete(peerID, 1024, 50*time.Millisecond, true)
		}

		// Measure throughput before loss
		start := time.Now()
		err := bm.AcquireBandwidth(peerID, 1024*2)
		if err != nil {
			t.Fatalf("Failed to acquire bandwidth: %v", err)
		}
		beforeLossDuration := time.Since(start)

		// Simulate packet loss
		bm.OnTransferComplete(peerID, 1024*2, 50*time.Millisecond, false)

		// Measure throughput after loss
		start = time.Now()
		err = bm.AcquireBandwidth(peerID, 1024*2)
		if err != nil {
			t.Fatalf("Failed to acquire bandwidth: %v", err)
		}
		afterLossDuration := time.Since(start)

		// After loss, transfers should take longer due to reduced window
		if afterLossDuration <= beforeLossDuration {
			t.Errorf("Transfer after loss should be slower: before=%v, after=%v", beforeLossDuration, afterLossDuration)
		}
	})

	t.Run("RTTTracking", func(t *testing.T) {
		bm := bandwidth.NewManager(1000000)
		peerID := "test-peer"

		transfers := []struct {
			bytes    int64
			duration time.Duration
		}{
			{1024, 100 * time.Millisecond},
			{1024, 120 * time.Millisecond},
			{1024, 90 * time.Millisecond},
		}

		for _, tt := range transfers {
			bm.AcquireBandwidth(peerID, tt.bytes)
			bm.OnTransferComplete(peerID, tt.bytes, tt.duration, true)
		}

		cc := bm.GetCongestionController(peerID)
		rtt := cc.GetRTT()

		if rtt == 0 {
			t.Error("RTT should be tracked")
		}
		// SRTT should be between min and max of samples
		if rtt < 90*time.Millisecond || rtt > 120*time.Millisecond {
			t.Errorf("Smoothed RTT %v outside expected range [90ms, 120ms]", rtt)
		}
	})

	t.Run("ConnectionQualityTracking", func(t *testing.T) {
		bm := bandwidth.NewManager(1000000)
		peerID := "test-peer"

		// Simulate successful and failed transfers
		bm.AcquireBandwidth(peerID, 1024)
		bm.OnTransferComplete(peerID, 1024, 100*time.Millisecond, true)
		bm.OnTransferComplete(peerID, 1024, 100*time.Millisecond, false)

		quality := bm.GetConnectionQuality(peerID)
		if quality == nil {
			t.Fatal("Expected connection quality to be tracked")
		}

		if quality.PacketLoss == 0 {
			t.Error("Packet loss should be non-zero after failed transfer")
		}
		if quality.Bandwidth == 0 {
			t.Error("Bandwidth should be non-zero after successful transfer")
		}
		if quality.ReliabilityScore == 0 {
			t.Error("Reliability score should be calculated")
		}
	})
}
