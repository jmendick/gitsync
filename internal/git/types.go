package git

import "time"

// RepositoryMetadata contains GitHub-specific repository information
type RepositoryMetadata struct {
	Owner         string
	Name          string
	CloneURL      string
	Private       bool
	DefaultBranch string
}

// RemoteConfig represents configuration for a remote
type RemoteConfig struct {
	Name     string
	URLs     []string
	Priority int // Priority for synchronization (higher number = higher priority)
}

// MergeStrategy represents different merge strategies
type MergeStrategy string

const (
	// MergeStrategyOctopus uses the octopus merge strategy for multiple branches
	MergeStrategyOctopus MergeStrategy = "octopus"
	// MergeStrategyResolve uses the resolve merge strategy
	MergeStrategyResolve MergeStrategy = "resolve"
	// MergeStrategyOurs takes our version in conflicts
	MergeStrategyOurs   MergeStrategy = "ours"
	MergeStrategyTheirs MergeStrategy = "theirs"
	MergeStrategyUnion  MergeStrategy = "union"
	MergeStrategyManual MergeStrategy = "manual"
)

// ConflictVersion represents a version of a file in conflict
type ConflictVersion struct {
	PeerID    string    `json:"peer_id"`
	Timestamp time.Time `json:"timestamp"`
	Hash      string    `json:"hash"`
	Content   []byte    `json:"content"`
}

// Conflict represents a Git merge conflict
type Conflict struct {
	ID        string            `json:"id"`
	FilePath  string            `json:"file_path"`
	Versions  []ConflictVersion `json:"versions"`
	Status    string            `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
}

// ConflictResolutionResult represents the result of conflict resolution
type ConflictResolutionResult struct {
	FilePath      string
	ResolvingHash string
	Strategy      MergeStrategy
	Success       bool
	Error         error
}
