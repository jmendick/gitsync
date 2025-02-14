package model

// SyncStatus represents the status of a sync operation
const (
	SyncStatusPending   = "pending"
	SyncStatusStarting  = "starting"
	SyncStatusSyncing   = "syncing"
	SyncStatusCompleted = "completed"
	SyncStatusFailed    = "failed"
	SyncStatusCancelled = "cancelled"
	SyncStatusRetrying  = "retrying"
	SyncStatusConflict  = "conflict"
)
