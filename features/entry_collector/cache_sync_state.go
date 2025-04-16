package entry_collector

type CacheSyncState int

const (
	CacheSyncStateIdle CacheSyncState = iota
	CacheSyncStateRunning
	CacheSyncStateQueued
)
