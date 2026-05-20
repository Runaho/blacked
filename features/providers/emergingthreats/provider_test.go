package emergingthreats

import (
	"strings"
	"testing"
	"time"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
)

// testCollector collects entries submitted during parse.
type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry)                     { c.entries = append(c.entries, entry) }
func (c *testCollector) Wait()                                           {}
func (c *testCollector) Close()                                          {}
func (c *testCollector) GetProcessedCount(source string) int             { return len(c.entries) }
func (c *testCollector) StartProviderProcessing(_, _ string)             {}
func (c *testCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

const testSource = "emerging-threats"
const testProcessID = "test-process-id"

func TestParseIPList_ValidIPv4(t *testing.T) {
	input := strings.NewReader("1.2.3.4\n5.6.7.8\n10.20.30.40\n")
	collector := &testCollector{}

	err := parseIPList(input, collector, testSource, testProcessID)

	assert.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))

	for _, e := range collector.entries {
		assert.Equal(t, testSource, e.Source)
		assert.Equal(t, testProcessID, e.ProcessID)
		assert.Equal(t, "compromised", e.Category)
		assert.NotEmpty(t, e.Host)
		assert.NotEmpty(t, e.Domain)
		assert.Equal(t, e.Host, e.Domain) // IP is both host and domain
	}
}

func TestParseIPList_EmptyInput(t *testing.T) {
	input := strings.NewReader("")
	collector := &testCollector{}

	err := parseIPList(input, collector, testSource, testProcessID)

	assert.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

func TestParseIPList_SkipComments(t *testing.T) {
	input := strings.NewReader("1.2.3.4\n# this is a comment\n5.6.7.8\n")
	collector := &testCollector{}

	err := parseIPList(input, collector, testSource, testProcessID)

	assert.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
	assert.Equal(t, "1.2.3.4", collector.entries[0].Host)
	assert.Equal(t, "5.6.7.8", collector.entries[1].Host)
}

func TestParseIPList_SkipIPv6(t *testing.T) {
	input := strings.NewReader("1.2.3.4\n::1\n2001:db8::1\n5.6.7.8\n")
	collector := &testCollector{}

	err := parseIPList(input, collector, testSource, testProcessID)

	assert.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
}

func TestParseIPList_SkipInvalidLines(t *testing.T) {
	input := strings.NewReader("1.2.3.4\nnot-an-ip\n\n5.6.7.8\n999.999.999.999\n")
	collector := &testCollector{}

	err := parseIPList(input, collector, testSource, testProcessID)

	assert.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
}

func TestParseIPList_BlankLines(t *testing.T) {
	input := strings.NewReader("1.2.3.4\n\n\n5.6.7.8\n\n")
	collector := &testCollector{}

	err := parseIPList(input, collector, testSource, testProcessID)

	assert.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
}
