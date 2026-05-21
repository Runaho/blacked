package abuseipdb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"blacked/features/entries"
	"blacked/features/entry_collector"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCollector implements entry_collector.Collector
type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry)       { c.entries = append(c.entries, entry) }
func (c *testCollector) Wait()                               {}
func (c *testCollector) Close()                              {}
func (c *testCollector) GetProcessedCount(source string) int { return len(c.entries) }
func (c *testCollector) StartProviderProcessing(_, _ string) {}
func (c *testCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

const testPID = "test-process-id"
const testSourceURL = "https://api.abuseipdb.com/api/v2/blacklist?confidenceMinimum=90&limit=10000"

// --- Parse tests ---

func TestParseAbuseIPDBData_ValidIPs(t *testing.T) {
	// Create test data with valid AbuseIPDB response format
	response := AbuseIPDBResponse{
		Data: []AbuseIPDBEntry{
			{IPAddress: "1.2.3.4", AbuseConfidenceScore: 100, CountryCode: "US"},
			{IPAddress: "5.6.7.8", AbuseConfidenceScore: 95, CountryCode: "DE"},
			{IPAddress: "9.10.11.12", AbuseConfidenceScore: 92, CountryCode: "UK"},
		},
	}

	data, err := json.Marshal(response)
	require.NoError(t, err)

	collector := &testCollector{}

	// Create a parse function that matches the provider implementation
	parseFunc := func(data []byte, collector entry_collector.Collector, sourceURL, processID string) error {
		var resp AbuseIPDBResponse
		if err := json.NewDecoder(bytes.NewReader(data)).Decode(&resp); err != nil {
			return err
		}

		for _, abuseEntry := range resp.Data {
			newEntry := entries.NewEntry().
				WithSource("abuseipdb").
				WithProcessID(processID).
				WithCategory("abuse")
			newEntry.Host = abuseEntry.IPAddress
			newEntry.SourceURL = sourceURL
			collector.Submit(newEntry)
		}
		return nil
	}

	err = parseFunc(data, collector, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))

	for i, e := range collector.entries {
		assert.Equal(t, "abuseipdb", e.Source)
		assert.Equal(t, testPID, e.ProcessID)
		assert.Equal(t, "abuse", e.Category)
		assert.Equal(t, testSourceURL, e.SourceURL)
		if i == 0 {
			assert.Equal(t, "1.2.3.4", e.Host)
		}
	}
}

func TestParseAbuseIPDBData_EmptyResponse(t *testing.T) {
	collector := &testCollector{}
	parseFunc := func(data []byte, collector entry_collector.Collector, sourceURL, processID string) error {
		var resp AbuseIPDBResponse
		if err := json.NewDecoder(bytes.NewReader(data)).Decode(&resp); err != nil {
			return err
		}
		for _, abuseEntry := range resp.Data {
			newEntry := entries.NewEntry().
				WithSource("abuseipdb").
				WithProcessID(processID).
				WithCategory("abuse")
			newEntry.Host = abuseEntry.IPAddress
			newEntry.SourceURL = sourceURL
			collector.Submit(newEntry)
		}
		return nil
	}

	err := parseFunc([]byte("{}\n"), collector, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

func TestParseAbuseIPDBData_MalformedJSON(t *testing.T) {
	collector := &testCollector{}
	parseFunc := func(data []byte, collector entry_collector.Collector, sourceURL, processID string) error {
		var resp AbuseIPDBResponse
		return json.NewDecoder(bytes.NewReader(data)).Decode(&resp)
	}

	err := parseFunc([]byte("{invalid json"), collector, testSourceURL, testPID)
	assert.Error(t, err)
}

func TestParseAbuseIPDBData_LargeSet(t *testing.T) {
	// Create test data with 1000 IPs
	entriesList := []AbuseIPDBEntry{}
	for i := 0; i < 1000; i++ {
		entriesList = append(entriesList, AbuseIPDBEntry{
			IPAddress: fmt.Sprintf("1.2.3.%d", i),
			AbuseConfidenceScore: 100,
			CountryCode: "US",
		})
	}
	response := AbuseIPDBResponse{Data: entriesList}
	data, err := json.Marshal(response)
	require.NoError(t, err)

	collector := &testCollector{}
	parseFunc := func(data []byte, collector entry_collector.Collector, sourceURL, processID string) error {
		var resp AbuseIPDBResponse
		if err := json.NewDecoder(bytes.NewReader(data)).Decode(&resp); err != nil {
			return err
		}
		for _, abuseEntry := range resp.Data {
			newEntry := entries.NewEntry().
				WithSource("abuseipdb").
				WithProcessID(processID).
				WithCategory("abuse")
			newEntry.Host = abuseEntry.IPAddress
			newEntry.SourceURL = sourceURL
			collector.Submit(newEntry)
		}
		return nil
	}

	err = parseFunc(data, collector, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 1000, len(collector.entries))
}