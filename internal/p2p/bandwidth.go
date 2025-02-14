// This file has been moved to internal/p2p/bandwidth/manager.go
// Please see that file for the implementation
package p2p

import (
	"github.com/jmendick/gitsync/internal/p2p/bandwidth"
)

// Deprecated: Use bandwidth.Manager directly
var _ bandwidth.BandwidthManager = &bandwidth.Manager{}

// Deprecated: Use bandwidth.ConnectionQuality directly
var _ bandwidth.ConnectionQuality = bandwidth.ConnectionQuality{}
