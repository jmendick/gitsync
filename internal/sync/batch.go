package sync

import (
	"sync"
	"time"
)

// AdaptiveBatchSizer manages dynamic batch sizing for sync operations
type AdaptiveBatchSizer struct {
	minSize     int64
	maxSize     int64
	currentSize int64
	window      []float64
	windowSize  int
	mu          sync.RWMutex
}

func NewAdaptiveBatchSizer(minSize, maxSize int64) *AdaptiveBatchSizer {
	return &AdaptiveBatchSizer{
		minSize:     minSize,
		maxSize:     maxSize,
		currentSize: minSize,
		window:      make([]float64, 0, 10),
		windowSize:  10,
	}
}

func (abs *AdaptiveBatchSizer) UpdateMetrics(originalSize, compressedSize int64, duration time.Duration) {
	abs.mu.Lock()
	defer abs.mu.Unlock()

	ratio := float64(compressedSize) / float64(originalSize)

	if len(abs.window) >= abs.windowSize {
		abs.window = abs.window[1:]
	}
	abs.window = append(abs.window, ratio)

	var avgRatio float64
	if len(abs.window) > 0 {
		sum := 0.0
		for _, r := range abs.window {
			sum += r
		}
		avgRatio = sum / float64(len(abs.window))
	}

	transferRate := float64(compressedSize) / duration.Seconds()

	if avgRatio < 0.7 && transferRate > float64(abs.currentSize) {
		abs.currentSize = minInt64(abs.currentSize*2, abs.maxSize)
	} else if avgRatio > 0.9 || transferRate < float64(abs.currentSize)/2 {
		abs.currentSize = maxInt64(abs.currentSize/2, abs.minSize)
	}
}

func (abs *AdaptiveBatchSizer) GetBatchSize() int64 {
	abs.mu.RLock()
	defer abs.mu.RUnlock()
	return abs.currentSize
}

// BatchCreator handles creation and management of file batches for syncing
type BatchCreator struct {
	batchSizer *AdaptiveBatchSizer
}

func NewBatchCreator(minSize, maxSize int64) *BatchCreator {
	return &BatchCreator{
		batchSizer: NewAdaptiveBatchSizer(minSize, maxSize),
	}
}

func (bc *BatchCreator) CreateBatches(files []string, peerCount int) [][]string {
	if peerCount <= 0 || len(files) == 0 {
		return nil
	}

	batchSize := (len(files) + peerCount - 1) / peerCount
	batches := make([][]string, 0, peerCount)

	for i := 0; i < len(files); i += batchSize {
		end := i + batchSize
		if end > len(files) {
			end = len(files)
		}
		batches = append(batches, files[i:end])
	}

	return batches
}

// Utility functions
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
