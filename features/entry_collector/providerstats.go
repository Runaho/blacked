package entry_collector

import (
	"sync"
	"time"
)

// ProviderStats tracks metrics for a specific provider
type ProviderStats struct {
	processedCount    int
	startTime         time.Time
	processID         string
	active            bool
	pendingOperations sync.WaitGroup // Track pending operations
}
