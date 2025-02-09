package model

// PeerInfo represents information about a peer in the P2P network.
type PeerInfo struct {
	ID        string   `json:"id"` // Unique Peer ID
	Addresses []string `json:"addresses"` // Network addresses of the peer
	// ... other peer related information ...
}

// RepositoryMetadata represents metadata about a synchronized repository.
type RepositoryMetadata struct {
	Name    string `json:"name"`    // Repository name
	Path    string `json:"path"`    // Local path to the repository
	Peers   []string `json:"peers"`   // IDs of peers sharing this repository
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
	// ... conflict details ...
}

// ... (Define more data models as needed for your application) ...