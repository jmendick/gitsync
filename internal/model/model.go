package model

// PeerInfo represents information about a peer in the P2P network.
type PeerInfo struct {
	ID        string   `json:"id"`        // Unique Peer ID
	Addresses []string `json:"addresses"` // Network addresses of the peer
	// ... other peer related information ...
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

// ... (Define more data models as needed for your application) ...
