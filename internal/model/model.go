package model

import (
	"time"
)

// PeerConnectionQuality represents connection quality metrics
type PeerConnectionQuality struct {
	Latency          time.Duration `json:"latency"`
	PacketLoss       float64       `json:"packet_loss"`
	Bandwidth        float64       `json:"bandwidth"`
	ReliabilityScore float64       `json:"reliability_score"`
}

// PeerSyncStats represents peer sync statistics
type PeerSyncStats struct {
	SuccessfulSyncs int       `json:"successful_syncs"`
	FailedSyncs     int       `json:"failed_syncs"`
	LastSyncTime    time.Time `json:"last_sync_time"`
	AverageLatency  float64   `json:"average_latency"`
	TotalBytes      int64     `json:"total_bytes"`
}

// PeerInfo represents information about a peer in the P2P network.
type PeerInfo struct {
	ID                string                 `json:"id"`        // Unique Peer ID
	Addresses         []string               `json:"addresses"` // Network addresses of the peer
	ConnectionQuality *PeerConnectionQuality `json:"connection_quality,omitempty"`
	SyncStats         *PeerSyncStats         `json:"sync_stats,omitempty"`
	LastSeen          time.Time              `json:"last_seen"`
	Features          []string               `json:"features,omitempty"`
}

// RepositoryMetadata represents metadata about a synchronized repository.
type RepositoryMetadata struct {
	Name  string   `json:"name"`  // Repository name
	Path  string   `json:"path"`  // Local path to the repository
	Peers []string `json:"peers"` // IDs of peers sharing this repository
	// ... other repository metadata ...
}

// SyncStatus represents the synchronization status of a repository.
type SyncStatus struct {
	RepositoryName string `json:"repository_name"`
	LastSyncTime   string `json:"last_sync_time"`
	Status         string `json:"status"` // e.g., "Syncing", "Synced", "Conflict", "Error"
	// ... other sync status details ...
}

// Conflict represents a synchronization conflict.
type Conflict struct {
	RepositoryName string `json:"repository_name"`
	FilePath       string `json:"file_path"`
	ConflictType   string `json:"conflict_type"` // e.g., "Content", "Metadata"
	// New fields for advanced conflict resolution
	OurCommit    string            `json:"our_commit"`
	TheirCommit  string            `json:"their_commit"`
	MergeStatus  string            `json:"merge_status"`
	Resolution   string            `json:"resolution"`
	FileMetadata map[string]string `json:"file_metadata"`
}

// Lock represents a file lock for conflict prevention
type Lock struct {
	FilePath   string `json:"file_path"`
	LockedBy   string `json:"locked_by"`
	AcquiredAt string `json:"acquired_at"`
	ExpiresAt  string `json:"expires_at"`
	LockID     string `json:"lock_id"`
}

// SyncOperation represents a synchronization operation
type SyncOperation struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time,omitempty"`
	Status       string    `json:"status"` // pending, running, completed, failed
	ErrorMessage string    `json:"error_message,omitempty"`
	Progress     float64   `json:"progress"`
	Strategy     string    `json:"strategy"`
	PeerID       string    `json:"peer_id"`
}

// SyncMetrics contains metrics about sync operations
type SyncMetrics struct {
	TotalBytes        int64         `json:"total_bytes"`
	TransferredBytes  int64         `json:"transferred_bytes"`
	TransferRate      float64       `json:"transfer_rate"`
	ConflictsFound    int           `json:"conflicts_found"`
	ConflictsResolved int           `json:"conflicts_resolved"`
	Duration          time.Duration `json:"duration"`
}

// ProgressReporter defines the interface for reporting progress
type ProgressReporter interface {
	UpdateProgress(operation *SyncOperation)
	OnComplete(operation *SyncOperation, metrics *SyncMetrics)
	OnError(operation *SyncOperation, err error)
}

// SyncResult contains the result of a sync operation
type SyncResult struct {
	Operation *SyncOperation `json:"operation"`
	Metrics   *SyncMetrics   `json:"metrics"`
	Changes   []string       `json:"changes"`
	Conflicts []*Conflict    `json:"conflicts"`
}
