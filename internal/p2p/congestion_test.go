package p2p

import (
	"testing"
	"time"
)

func TestCongestionController(t *testing.T) {
	t.Run("SlowStart", func(t *testing.T) {
		cc := NewCongestionController()
		initialWindow := cc.GetWindow()

		// Simulate successful transmissions during slow start
		for i := 0; i < 5; i++ {
			cc.OnAck(1024) // 1KB ack
		}

		newWindow := cc.GetWindow()
		if newWindow <= initialWindow {
			t.Errorf("Window should increase during slow start: initial=%f, new=%f",
				initialWindow, newWindow)
		}
	})

	t.Run("CongestionAvoidance", func(t *testing.T) {
		cc := NewCongestionController()
		cc.ssthresh = 20480 // 20KB threshold

		// Push into congestion avoidance
		for cc.state != CongestionAvoidance {
			cc.OnAck(1024)
		}

		initialWindow := cc.GetWindow()
		previousIncrease := 0.0

		// Track window increases during congestion avoidance
		for i := 0; i < 5; i++ {
			beforeWindow := cc.GetWindow()
			cc.OnAck(1024)
			increase := cc.GetWindow() - beforeWindow

			if i > 0 && increase >= previousIncrease {
				t.Error("Window increase should be decreasing in congestion avoidance")
			}
			previousIncrease = increase
		}

		if cc.GetWindow() <= initialWindow {
			t.Error("Window should still increase during congestion avoidance")
		}
	})

	t.Run("LossHandling", func(t *testing.T) {
		cc := NewCongestionController()

		// Build up window
		for i := 0; i < 10; i++ {
			cc.OnAck(1024)
		}
		windowBeforeLoss := cc.GetWindow()

		// Simulate loss
		cc.OnLoss()
		windowAfterLoss := cc.GetWindow()

		if windowAfterLoss >= windowBeforeLoss {
			t.Error("Window should decrease after loss")
		}
		if windowAfterLoss != cc.ssthresh {
			t.Error("Window should equal ssthresh after loss")
		}
	})

	t.Run("TimeoutRecovery", func(t *testing.T) {
		cc := NewCongestionController()

		// Build up window
		for i := 0; i < 10; i++ {
			cc.OnAck(1024)
		}

		// Simulate timeout
		cc.OnTimeout()

		if cc.GetWindow() != 1024*10 {
			t.Errorf("Window should reset to initial size after timeout, got %f",
				cc.GetWindow())
		}
		if cc.state != SlowStart {
			t.Error("Should enter slow start after timeout")
		}
	})

	t.Run("RTTEstimation", func(t *testing.T) {
		cc := NewCongestionController()

		// Series of RTT samples
		samples := []time.Duration{
			100 * time.Millisecond,
			120 * time.Millisecond,
			90 * time.Millisecond,
			110 * time.Millisecond,
		}

		var lastRTT time.Duration
		for _, sample := range samples {
			cc.UpdateRTT(sample)
			if lastRTT != 0 {
				// RTT should adapt smoothly
				diff := cc.GetRTT() - lastRTT
				if diff > 20*time.Millisecond {
					t.Error("RTT changed too abruptly")
				}
			}
			lastRTT = cc.GetRTT()
		}
	})

	t.Run("TransmissionQuota", func(t *testing.T) {
		cc := NewCongestionController()
		window := cc.GetWindow()

		// Test different amounts of bytes in flight
		tests := []struct {
			bytesInFlight float64
			expectQuota   bool
		}{
			{0, true},
			{window / 2, true},
			{window, false},
			{window * 2, false},
		}

		for _, tt := range tests {
			quota := cc.GetTransmissionQuota(tt.bytesInFlight)
			hasQuota := quota > 0
			if hasQuota != tt.expectQuota {
				t.Errorf("For bytes in flight %f: expected quota available=%v, got %v",
					tt.bytesInFlight, tt.expectQuota, hasQuota)
			}
		}
	})

	t.Run("BackoffCalculation", func(t *testing.T) {
		cc := NewCongestionController()

		// Simulate some RTT measurements
		cc.UpdateRTT(100 * time.Millisecond)
		cc.UpdateRTT(120 * time.Millisecond)
		cc.UpdateRTT(90 * time.Millisecond)

		backoff := cc.CalculateBackoff()
		if backoff < cc.GetRTT() {
			t.Error("Backoff duration should be greater than RTT")
		}
		if backoff < 1*time.Second {
			t.Error("Backoff should not be less than 1 second")
		}
		if backoff > 60*time.Second {
			t.Error("Backoff should not exceed 60 seconds")
		}
	})
}
