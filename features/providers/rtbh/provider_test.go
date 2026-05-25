package rtbh

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCollector implements entry_collector.Collector for testing
type mockCollector struct {
	entries []*entries.Entry
}

func (m *mockCollector) Submit(entry *entries.Entry)                { m.entries = append(m.entries, entry) }
func (m *mockCollector) Wait()                                       {}
func (m *mockCollector) RemoveStaleEntriesAndSyncBloom(ctx context.Context, providerName, processID string) error { return nil }
func (m *mockCollector) Close()                                      {}
func (m *mockCollector) StartProviderProcessing(name, processID string) {}
func (m *mockCollector) FinishProviderProcessing(name, processID string) (int, time.Duration, bool) {
	return len(m.entries), 0, true
}
func (m *mockCollector) GetProcessedCount(source string) int { return len(m.entries) }

func TestParseIPLines_ValidIPv4(t *testing.T) {
	collector := &mockCollector{}

	data := strings.NewReader("1.2.3.4\n5.6.7.8\n")
	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		ip := net.ParseIP(line)
		if ip == nil || ip.To4() == nil {
			return nil, nil
		}
		entry := entries.NewEntry().
			WithSource("rtbh-turkey").
			WithProcessID(processID).
			WithCategory("government-feed").
			WithIP(ip.String())
		return entry, nil
	}

	err := base.ParseLinesParallel(data, collector, "rtbh-turkey", 1, 2, processor)
	require.NoError(t, err)

	assert.Equal(t, 2, len(collector.entries))
	for _, e := range collector.entries {
		assert.Equal(t, "rtbh-turkey", e.Source)
		assert.Equal(t, "government-feed", e.Category)
		assert.NotEmpty(t, e.ProcessID)
		assert.Equal(t, e.Domain, e.Host)
		assert.True(t, net.ParseIP(e.Domain) != nil, "domain should be a valid IP")
	}
}

func TestParseIPLines_IPv6Skip(t *testing.T) {
	collector := &mockCollector{}

	data := strings.NewReader("2001:db8::1\n10.20.30.40\n::1\n")
	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		ip := net.ParseIP(line)
		if ip == nil || ip.To4() == nil {
			return nil, nil
		}
		entry := entries.NewEntry().
			WithSource("rtbh-turkey").
			WithProcessID(processID).
			WithCategory("government-feed").
			WithIP(ip.String())
		return entry, nil
	}

	err := base.ParseLinesParallel(data, collector, "rtbh-turkey", 1, 2, processor)
	require.NoError(t, err)

	assert.Equal(t, 1, len(collector.entries))
	assert.Equal(t, "10.20.30.40", collector.entries[0].Domain)
}

func TestParseIPLines_EmptyAndSkip(t *testing.T) {
	collector := &mockCollector{}

	data := strings.NewReader("# comment\n\n1.2.3.4\n   \n# another\n5.6.7.8\ninvalid\n")
	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		ip := net.ParseIP(line)
		if ip == nil || ip.To4() == nil {
			return nil, nil
		}
		entry := entries.NewEntry().
			WithSource("rtbh-turkey").
			WithProcessID(processID).
			WithCategory("government-feed").
			WithIP(ip.String())
		return entry, nil
	}

	err := base.ParseLinesParallel(data, collector, "rtbh-turkey", 1, 2, processor)
	require.NoError(t, err)

	assert.Equal(t, 2, len(collector.entries))
}

func TestParseIPLines_CRLF(t *testing.T) {
	collector := &mockCollector{}

	data := strings.NewReader("1.2.3.4\r\n5.6.7.8\r\n\r\n")
	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		ip := net.ParseIP(line)
		if ip == nil || ip.To4() == nil {
			return nil, nil
		}
		entry := entries.NewEntry().
			WithSource("rtbh-turkey").
			WithProcessID(processID).
			WithCategory("government-feed").
			WithIP(ip.String())
		return entry, nil
	}

	err := base.ParseLinesParallel(data, collector, "rtbh-turkey", 1, 2, processor)
	require.NoError(t, err)

	assert.Equal(t, 2, len(collector.entries))
}

func TestParseIPLines_BatchSizeOne(t *testing.T) {
	collector := &mockCollector{}

	lines := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		lines = append(lines, "10.0.0."+string(rune('0'+i%10))+"."+string(rune('0'+i/10)))
	}

	// Use valid loopback range for tests
	singleIP := strings.NewReader("127.0.0.1\n127.0.0.2\n127.0.0.3\n")
	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" {
			return nil, nil
		}
		ip := net.ParseIP(line)
		if ip == nil || ip.To4() == nil {
			return nil, nil
		}
		entry := entries.NewEntry().
			WithSource("rtbh-turkey").
			WithProcessID(processID).
			WithCategory("government-feed").
			WithIP(ip.String())
		return entry, nil
	}

	// batch size 1 forces edge case on channel batching
	var col entry_collector.Collector = collector
	err := base.ParseLinesParallel(singleIP, col, "rtbh-turkey", 1, 1, processor)
	require.NoError(t, err)

	assert.Equal(t, 3, len(collector.entries))
}
