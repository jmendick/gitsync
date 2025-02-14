package sync

import (
	"math"
	"os"
	"sync"
	"testing"
	"time"
)

func TestMetricsCollector(t *testing.T) {
	t.Run("BasicMetricRecording", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "gitsync-metrics-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		mc := NewMetricsCollector(tempDir, 24*time.Hour)

		// Record some test metrics
		mc.RecordMetric(MetricBandwidthUsage, 1024.0, map[string]string{"peer": "test-peer"})
		mc.RecordMetric(MetricBandwidthUsage, 2048.0, map[string]string{"peer": "test-peer"})

		avg := mc.GetMetricAverage(MetricBandwidthUsage, time.Hour)
		if avg != 1536.0 { // (1024 + 2048) / 2
			t.Errorf("Expected average 1536.0, got %f", avg)
		}
	})

	t.Run("PercentileCalculation", func(t *testing.T) {
		mc := NewMetricsCollector("", time.Hour)

		// Record ordered values
		values := []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0}
		for _, v := range values {
			mc.RecordMetric(MetricPeerLatency, v, nil)
		}

		tests := []struct {
			percentile float64
			expected   float64
		}{
			{50, 5.5},  // Median
			{90, 9.1},  // 90th percentile
			{95, 9.55}, // 95th percentile
			{99, 9.91}, // 99th percentile
		}

		for _, tt := range tests {
			got := mc.GetMetricPercentile(MetricPeerLatency, tt.percentile)
			if math.Abs(got-tt.expected) > 0.01 {
				t.Errorf("Expected p%.0f = %.2f, got %.2f", tt.percentile, tt.expected, got)
			}
		}
	})

	t.Run("MetricRetention", func(t *testing.T) {
		mc := NewMetricsCollector("", time.Minute)

		// Record metrics at different times
		now := time.Now()
		mc.mu.Lock()
		mc.metrics[MetricSyncDuration] = []MetricValue{
			{Timestamp: now.Add(-2 * time.Minute), Value: 1.0},
			{Timestamp: now.Add(-30 * time.Second), Value: 2.0},
			{Timestamp: now, Value: 3.0},
		}
		mc.mu.Unlock()

		mc.pruneOldMetrics(MetricSyncDuration)

		mc.mu.RLock()
		retained := mc.metrics[MetricSyncDuration]
		mc.mu.RUnlock()

		if len(retained) != 2 {
			t.Errorf("Expected 2 metrics retained, got %d", len(retained))
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		mc := NewMetricsCollector("", time.Hour)

		// Start multiple goroutines recording metrics
		const numGoroutines = 10
		const metricsPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < metricsPerGoroutine; j++ {
					mc.RecordMetric(MetricBandwidthUsage, float64(j), nil)
				}
			}()
		}

		wg.Wait()

		mc.mu.RLock()
		count := len(mc.metrics[MetricBandwidthUsage])
		mc.mu.RUnlock()

		expected := numGoroutines * metricsPerGoroutine
		if count != expected {
			t.Errorf("Expected %d metrics, got %d", expected, count)
		}
	})

	t.Run("TransferTracking", func(t *testing.T) {
		mc := NewMetricsCollector("", time.Hour)

		mc.StartTransfer()
		mc.StartTransfer()
		mc.EndTransfer(1000)

		stats := mc.GetCurrentStats()

		if stats["active_transfers"].(int64) != 1 {
			t.Errorf("Expected 1 active transfer, got %d", stats["active_transfers"])
		}

		if stats["bytes_transferred"].(int64) != 1000 {
			t.Errorf("Expected 1000 bytes transferred, got %d", stats["bytes_transferred"])
		}
	})

	t.Run("PersistenceAndRecovery", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "gitsync-metrics-persistence-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create and populate first collector
		mc1 := NewMetricsCollector(tempDir, time.Hour)
		mc1.RecordMetric(MetricBandwidthUsage, 1024.0, nil)
		mc1.RecordMetric(MetricBandwidthUsage, 2048.0, nil)

		if err := mc1.Save(); err != nil {
			t.Fatalf("Failed to save metrics: %v", err)
		}

		// Create second collector and load saved metrics
		mc2 := NewMetricsCollector(tempDir, time.Hour)
		if err := mc2.Load(); err != nil {
			t.Fatalf("Failed to load metrics: %v", err)
		}

		// Verify metrics were preserved
		avg1 := mc1.GetMetricAverage(MetricBandwidthUsage, time.Hour)
		avg2 := mc2.GetMetricAverage(MetricBandwidthUsage, time.Hour)

		if avg1 != avg2 {
			t.Errorf("Expected loaded average %f to match original %f", avg2, avg1)
		}
	})
}

func TestMetricsWindowCalculation(t *testing.T) {
	mc := NewMetricsCollector("", time.Hour)

	// Record metrics with different timestamps
	now := time.Now()
	mc.mu.Lock()
	mc.metrics[MetricCompression] = []MetricValue{
		{Timestamp: now.Add(-2 * time.Hour), Value: 1.0},    // Too old
		{Timestamp: now.Add(-30 * time.Minute), Value: 2.0}, // Within window
		{Timestamp: now.Add(-15 * time.Minute), Value: 3.0}, // Within window
		{Timestamp: now, Value: 4.0},                        // Within window
	}
	mc.mu.Unlock()

	// Test different window sizes
	tests := []struct {
		window   time.Duration
		expected float64
	}{
		{15 * time.Minute, 4.0}, // Only newest
		{45 * time.Minute, 3.0}, // Last three
		{3 * time.Hour, 2.5},    // All but oldest
	}

	for _, tt := range tests {
		got := mc.GetMetricAverage(MetricCompression, tt.window)
		if math.Abs(got-tt.expected) > 0.01 {
			t.Errorf("For window %v: expected %.2f, got %.2f", tt.window, tt.expected, got)
		}
	}
}
