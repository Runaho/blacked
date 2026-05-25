package pihole

import (
	"context"
	"blacked/features/entries"
	"time"
)

// testCollector is a simple implementation of entry_collector.Collector for testing
type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry) {
	c.entries = append(c.entries, entry)
}

func (c *testCollector) Wait() {
	// No-op for testing
}

func (c *testCollector) Close() {
	// No-op for testing
}

func (c *testCollector) GetProcessedCount(source string) int {
	return len(c.entries)
}

func (c *testCollector) RemoveStaleEntriesAndSyncBloom(ctx context.Context, providerName, processID string) error {
	return nil
}

func (c *testCollector) StartProviderProcessing(providerName, processID string) {
	// No-op for testing
}

func (c *testCollector) FinishProviderProcessing(providerName, processID string) (count int, duration time.Duration, ok bool) {
	return len(c.entries), 0, true
}

func boolPtr(b bool) *bool       { return &b }
func strPtr(s string) *string    { return &s }
func intPtr(i int) *int          { return &i }
