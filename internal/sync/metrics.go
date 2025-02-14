package sync

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType identifies different types of metrics
type MetricType string

const (
	MetricSyncDuration   MetricType = "sync_duration"
	MetricBandwidthUsage MetricType = "bandwidth_usage"
	MetricCompression    MetricType = "compression_ratio"
	MetricBatchSize      MetricType = "batch_size"
	MetricConflicts      MetricType = "conflicts"
	MetricPeerLatency    MetricType = "peer_latency"
)

// MetricValue represents a collected metric value
type MetricValue struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// MetricsCollector collects and stores sync performance metrics
type MetricsCollector struct {
	baseDir     string
	metricsFile string
	metrics     map[MetricType][]MetricValue
	window      time.Duration
	mu          sync.RWMutex

	// Real-time counters
	activeTransfers   atomic.Int64
	bytesTransferred  atomic.Int64
	conflictsResolved atomic.Int64
}

func NewMetricsCollector(baseDir string, window time.Duration) *MetricsCollector {
	return &MetricsCollector{
		baseDir:     baseDir,
		metricsFile: filepath.Join(baseDir, ".gitsync", "metrics.json"),
		metrics:     make(map[MetricType][]MetricValue),
		window:      window,
	}
}

func (mc *MetricsCollector) RecordMetric(metricType MetricType, value float64, labels map[string]string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metric := MetricValue{
		Timestamp: time.Now(),
		Value:     value,
		Labels:    labels,
	}

	mc.metrics[metricType] = append(mc.metrics[metricType], metric)
	mc.pruneOldMetrics(metricType)
}

func (mc *MetricsCollector) pruneOldMetrics(metricType MetricType) {
	cutoff := time.Now().Add(-mc.window)
	metrics := mc.metrics[metricType]

	i := 0
	for ; i < len(metrics); i++ {
		if metrics[i].Timestamp.After(cutoff) {
			break
		}
	}

	if i > 0 {
		mc.metrics[metricType] = metrics[i:]
	}
}

func (mc *MetricsCollector) GetMetricAverage(metricType MetricType, duration time.Duration) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	var sum float64
	var count int

	for _, m := range mc.metrics[metricType] {
		if m.Timestamp.After(cutoff) {
			sum += m.Value
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func (mc *MetricsCollector) GetMetricPercentile(metricType MetricType, percentile float64) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metrics := mc.metrics[metricType]
	if len(metrics) == 0 {
		return 0
	}

	// Create sorted copy of values
	values := make([]float64, len(metrics))
	for i, m := range metrics {
		values[i] = m.Value
	}
	sort.Float64s(values)

	// Calculate percentile index
	index := (percentile / 100) * float64(len(values)-1)
	if index == float64(int(index)) {
		return values[int(index)]
	}

	// Interpolate between two values
	lower := values[int(math.Floor(index))]
	upper := values[int(math.Ceil(index))]
	fraction := index - math.Floor(index)
	return lower + (upper-lower)*fraction
}

// StartTransfer starts tracking a new transfer
func (mc *MetricsCollector) StartTransfer() {
	mc.activeTransfers.Add(1)
}

// EndTransfer records the completion of a transfer
func (mc *MetricsCollector) EndTransfer(bytes int64) {
	mc.activeTransfers.Add(-1)
	mc.bytesTransferred.Add(bytes)
}

// RecordConflictResolution records a resolved conflict
func (mc *MetricsCollector) RecordConflictResolution() {
	mc.conflictsResolved.Add(1)
}

// GetCurrentStats returns current sync statistics
func (mc *MetricsCollector) GetCurrentStats() map[string]interface{} {
	return map[string]interface{}{
		"active_transfers":   mc.activeTransfers.Load(),
		"bytes_transferred":  mc.bytesTransferred.Load(),
		"conflicts_resolved": mc.conflictsResolved.Load(),
		"avg_bandwidth":      mc.GetMetricAverage(MetricBandwidthUsage, time.Minute),
		"p95_latency":        mc.GetMetricPercentile(MetricPeerLatency, 95),
		"avg_compression":    mc.GetMetricAverage(MetricCompression, time.Minute),
	}
}

func (mc *MetricsCollector) Save() error {
	mc.mu.RLock()
	data, err := json.MarshalIndent(mc.metrics, "", "  ")
	mc.mu.RUnlock()

	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(mc.metricsFile), 0755); err != nil {
		return err
	}

	return os.WriteFile(mc.metricsFile, data, 0644)
}

func (mc *MetricsCollector) Load() error {
	data, err := os.ReadFile(mc.metricsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()
	return json.Unmarshal(data, &mc.metrics)
}
