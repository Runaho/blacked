package entry_collector

import (
	"blacked/features/entries"
)

// Collector defines an interface for collecting and processing entries
type Collector interface {
	Submit(entry entries.Entry)
	Wait()
	Close()
	GetProcessedCount(source string) int
	StartProviderProcessing(providerName, processID string)
	FinishProviderProcessing(providerName, processID string)
}
