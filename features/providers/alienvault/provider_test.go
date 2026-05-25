package alienvault

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"blacked/features/entries"
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Disable logging for cleaner test output
	zerolog.SetGlobalLevel(zerolog.Disabled)
}

func TestNewAlienvaultProvider(t *testing.T) {
	t.Run("disabled provider", func(t *testing.T) {
		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled: boolPtr(false),
				},
			},
		}
		provider := NewAlienvaultProvider(cfg, colly.NewCollector())
		assert.Nil(t, provider)
	})

	t.Run("enabled provider with defaults", func(t *testing.T) {
		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled: boolPtr(true),
				},
			},
		}
		provider := NewAlienvaultProvider(cfg, colly.NewCollector())
		assert.NotNil(t, provider)
		assert.Equal(t, "alienvault", provider.GetName())
		assert.Equal(t, "threat_intel", provider.(*alienvaultProvider).GetCategory())
		assert.Equal(t, "0 */6 * * *", provider.GetCronSchedule())
	})

	t.Run("custom configuration", func(t *testing.T) {
		customURL := "https://otx.alienvault.com/api/v1/pulses/subscribed?limit=10"
		customCron := "0 */12 * * *"
		customCategory := "custom_threat"
		apiKey := "test-api-key"
		
		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled:   boolPtr(true),
					SourceURL: customURL,
					Cron:      customCron,
					Category:  customCategory,
					APIKey:    apiKey,
				},
			},
		}
		provider := NewAlienvaultProvider(cfg, colly.NewCollector())
		assert.NotNil(t, provider)
		avProvider := provider.(*alienvaultProvider)
		assert.Equal(t, customURL, avProvider.Source())
		assert.Equal(t, customCron, avProvider.GetCronSchedule())
		assert.Equal(t, customCategory, avProvider.GetCategory())
		assert.Equal(t, apiKey, avProvider.apiKey)
	})
}

func TestParseAlienvaultResponse(t *testing.T) {
	t.Run("parse valid response", func(t *testing.T) {
		sampleResponse := OTXResponse{
			Count: 2,
			Results: []OTXPulse{
				{
					ID:       "1",
					Name:     "Test Pulse 1",
					Indicators: []OTXIndicator{
						{Type: "IPv4", Indicator: "1.2.3.4"},
						{Type: "domain", Indicator: "malicious.example.com"},
						{Type: "URL", Indicator: "http://evil.com/malware.exe"},
						{Type: "hostname", Indicator: "bad.hostname.com"},
					},
				},
				{
					ID:       "2",
					Name:     "Test Pulse 2",
					Indicators: []OTXIndicator{
						{Type: "IPv6", Indicator: "2001:db8::1"},
						{Type: "domain", Indicator: "phishing.site"},
					},
				},
			},
		}

		jsonData, err := json.Marshal(sampleResponse)
		require.NoError(t, err)

		var collectedEntries []*entries.Entry
		mockCollector := &mockEntryCollector{entries: &collectedEntries}

		err = parseAlienvaultResponse(jsonData, mockCollector, "alienvault", "test-process-id")
		require.NoError(t, err)

		// Should have 6 valid entries (IPv4, domain, URL, hostname, IPv6, domain)
		assert.Len(t, collectedEntries, 6)

		// Verify entry types and categories
		ipEntry := findEntryByIndicator(collectedEntries, "1.2.3.4")
		assert.NotNil(t, ipEntry)
		assert.Equal(t, "malicious_ip", ipEntry.Category)

		domainEntry := findEntryByIndicator(collectedEntries, "malicious.example.com")
		assert.NotNil(t, domainEntry)
		assert.Equal(t, "malicious_domain", domainEntry.Category)

		urlEntry := findEntryByIndicator(collectedEntries, "http://evil.com/malware.exe")
		assert.NotNil(t, urlEntry)
		assert.Equal(t, "malicious_url", urlEntry.Category)

		hostnameEntry := findEntryByIndicator(collectedEntries, "bad.hostname.com")
		assert.NotNil(t, hostnameEntry)
		assert.Equal(t, "malicious_hostname", hostnameEntry.Category)

		ipv6Entry := findEntryByIndicator(collectedEntries, "2001:db8::1")
		assert.NotNil(t, ipv6Entry)
		assert.Equal(t, "malicious_ip", ipv6Entry.Category)
	})

	t.Run("skip unsupported indicator types", func(t *testing.T) {
		sampleResponse := OTXResponse{
			Count: 1,
			Results: []OTXPulse{
				{
					ID:       "1",
					Name:     "Test Pulse",
					Indicators: []OTXIndicator{
						{Type: "IPv4", Indicator: "1.2.3.4"},
						{Type: "FileHash-MD5", Indicator: "d41d8cd98f00b204e9800998ecf8427e"},
						{Type: "FileHash-SHA256", Indicator: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
					},
				},
			},
		}

		jsonData, err := json.Marshal(sampleResponse)
		require.NoError(t, err)

		var collectedEntries []*entries.Entry
		mockCollector := &mockEntryCollector{entries: &collectedEntries}

		err = parseAlienvaultResponse(jsonData, mockCollector, "alienvault", "test-process-id")
		require.NoError(t, err)

		// Should only have 1 entry (IPv4), hashes should be skipped
		assert.Len(t, collectedEntries, 1)
		assert.Equal(t, "1.2.3.4", collectedEntries[0].Host)
	})

	t.Run("handle empty and invalid indicators", func(t *testing.T) {
		sampleResponse := OTXResponse{
			Count: 1,
			Results: []OTXPulse{
				{
					ID:       "1",
					Name:     "Test Pulse",
					Indicators: []OTXIndicator{
						{Type: "IPv4", Indicator: ""},
						{Type: "domain", Indicator: "."},
						{Type: "domain", Indicator: "valid.example.com"},
					},
				},
			},
		}

		jsonData, err := json.Marshal(sampleResponse)
		require.NoError(t, err)

		var collectedEntries []*entries.Entry
		mockCollector := &mockEntryCollector{entries: &collectedEntries}

		err = parseAlienvaultResponse(jsonData, mockCollector, "alienvault", "test-process-id")
		require.NoError(t, err)

		// Should only have 1 valid entry
		assert.Len(t, collectedEntries, 1)
		assert.Equal(t, "valid.example.com", collectedEntries[0].Host)
	})
}

func TestIndicatorToEntry(t *testing.T) {
	tests := []struct {
		name      string
		indicator OTXIndicator
		wantEntry bool
		expectedCategory string
	}{
		{
			name:      "valid IPv4",
			indicator: OTXIndicator{Type: "IPv4", Indicator: "8.8.8.8"},
			wantEntry: true,
			expectedCategory: "malicious_ip",
		},
		{
			name:      "valid IPv6",
			indicator: OTXIndicator{Type: "IPv6", Indicator: "2001:4860:4860::8888"},
			wantEntry: true,
			expectedCategory: "malicious_ip",
		},
		{
			name:      "valid domain",
			indicator: OTXIndicator{Type: "domain", Indicator: "google.com"},
			wantEntry: true,
			expectedCategory: "malicious_domain",
		},
		{
			name:      "valid hostname",
			indicator: OTXIndicator{Type: "hostname", Indicator: "server.example.com"},
			wantEntry: true,
			expectedCategory: "malicious_hostname",
		},
		{
			name:      "valid URL",
			indicator: OTXIndicator{Type: "URL", Indicator: "https://evil.com/malware"},
			wantEntry: true,
			expectedCategory: "malicious_url",
		},
		{
			name:      "empty IPv4",
			indicator: OTXIndicator{Type: "IPv4", Indicator: ""},
			wantEntry: false,
		},
		{
			name:      "empty domain",
			indicator: OTXIndicator{Type: "domain", Indicator: ""},
			wantEntry: false,
		},
		{
			name:      "dot domain",
			indicator: OTXIndicator{Type: "domain", Indicator: "."},
			wantEntry: false,
		},
		{
			name:      "unsupported type",
			indicator: OTXIndicator{Type: "FileHash-MD5", Indicator: "abc123"},
			wantEntry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := indicatorToEntry(&tt.indicator, "alienvault", "test-process-id")
			if tt.wantEntry {
				require.NoError(t, err)
				assert.NotNil(t, entry)
				assert.Equal(t, tt.expectedCategory, entry.Category)
				assert.Equal(t, "alienvault", entry.Source)
				assert.Equal(t, "test-process-id", entry.ProcessID)
			} else {
				assert.Nil(t, entry)
			}
		})
	}
}

func TestAlienvaultProvider_Fetch(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		sampleResponse := OTXResponse{
			Count: 1,
			Results: []OTXPulse{
				{
					ID:       "1",
					Name:     "Test Pulse",
					Indicators: []OTXIndicator{
						{Type: "IPv4", Indicator: "1.2.3.4"},
					},
				},
			},
		}

		jsonData, err := json.Marshal(sampleResponse)
		require.NoError(t, err)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check API key header
			apiKey := r.Header.Get("X-OTX-API-KEY")
			assert.Equal(t, "test-api-key", apiKey)
			assert.Equal(t, "application/json", r.Header.Get("Accept"))

			w.WriteHeader(http.StatusOK)
			w.Write(jsonData)
		}))
		defer server.Close()

		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled:   boolPtr(true),
					SourceURL: server.URL,
					APIKey:    "test-api-key",
				},
			},
		}

		// Create a custom colly client that allows the test server domain
		customColly := colly.NewCollector()
		// Allow all domains for testing
		customColly.AllowedDomains = []string{}

		provider := NewAlienvaultProvider(cfg, customColly)
		require.NotNil(t, provider)

		avProvider := provider.(*alienvaultProvider)
		// Override rate limit for testing
		avProvider.rateLimit = 10 * time.Millisecond

		reader, err := avProvider.Fetch()
		require.NoError(t, err)
		require.NotNil(t, reader)

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.JSONEq(t, string(jsonData), string(data))
	})

	t.Run("fetch with authentication error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Invalid API key"}`))
		}))
		defer server.Close()

		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled:   boolPtr(true),
					SourceURL: server.URL,
					APIKey:    "wrong-key",
				},
			},
		}

		customColly := colly.NewCollector()
		customColly.AllowedDomains = []string{}

		provider := NewAlienvaultProvider(cfg, customColly)
		require.NotNil(t, provider)

		avProvider := provider.(*alienvaultProvider)
		avProvider.rateLimit = 10 * time.Millisecond

		_, err := avProvider.Fetch()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("empty response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Empty body
		}))
		defer server.Close()

		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled:   boolPtr(true),
					SourceURL: server.URL,
					APIKey:    "test-api-key",
				},
			},
		}

		customColly := colly.NewCollector()
		customColly.AllowedDomains = []string{}

		provider := NewAlienvaultProvider(cfg, customColly)
		require.NotNil(t, provider)

		avProvider := provider.(*alienvaultProvider)
		avProvider.rateLimit = 10 * time.Millisecond

		_, err := avProvider.Fetch()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty response")
	})

	t.Run("pagination merges multiple pages", func(t *testing.T) {
		// Create server first to capture base URL
		var serverURL string
		pageCount := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pageCount++
			var response OTXResponse
			switch pageCount {
			case 1:
				response = OTXResponse{
					Count: 2,
					Next:  serverURL + "/api/v1/pulses/subscribed?page=2",
					Results: []OTXPulse{
						{ID: "pulse-1", Name: "Pulse Page 1", Indicators: []OTXIndicator{
							{Type: "IPv4", Indicator: "1.1.1.1"},
							{Type: "domain", Indicator: "page1.example.com"},
						}},
					},
				}
			case 2:
				response = OTXResponse{
					Count: 2,
					Next:  serverURL + "/api/v1/pulses/subscribed?page=3",
					Results: []OTXPulse{
						{ID: "pulse-2", Name: "Pulse Page 2", Indicators: []OTXIndicator{
							{Type: "IPv4", Indicator: "2.2.2.2"},
							{Type: "domain", Indicator: "page2.example.com"},
						}},
					},
				}
			case 3:
				response = OTXResponse{
					Count: 1,
					Next:  "", // No more pages
					Results: []OTXPulse{
						{ID: "pulse-3", Name: "Pulse Page 3", Indicators: []OTXIndicator{
							{Type: "IPv4", Indicator: "3.3.3.3"},
						}},
					},
				}
			default:
				t.Fatalf("Unexpected page request: %d", pageCount)
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		// Capture server URL after creation
		serverURL = server.URL

		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled:   boolPtr(true),
					SourceURL: server.URL,
					APIKey:    "test-api-key",
				},
			},
		}

		customColly := colly.NewCollector()
		customColly.AllowedDomains = []string{}

		provider := NewAlienvaultProvider(cfg, customColly)
		require.NotNil(t, provider)

		avProvider := provider.(*alienvaultProvider)
		avProvider.rateLimit = 10 * time.Millisecond

		reader, err := avProvider.Fetch()
		require.NoError(t, err)
		require.NotNil(t, reader)

		data, err := io.ReadAll(reader)
		require.NoError(t, err)

		// Parse the merged response
		var merged OTXResponse
		err = json.Unmarshal(data, &merged)
		require.NoError(t, err)

		// Should have 3 pulses from 3 pages
		assert.Equal(t, 3, merged.Count)
		assert.Empty(t, merged.Next) // Next should be empty after merge

		// Verify all pulses are present
		assert.Len(t, merged.Results, 3)
		assert.Equal(t, "pulse-1", merged.Results[0].ID)
		assert.Equal(t, "pulse-2", merged.Results[1].ID)
		assert.Equal(t, "pulse-3", merged.Results[2].ID)

		// Verify all indicators are present (3 pulses × 2 indicators = 6, last pulse has 1)
		totalIndicators := 0
		for _, pulse := range merged.Results {
			totalIndicators += len(pulse.Indicators)
		}
		assert.Equal(t, 5, totalIndicators) // 2 + 2 + 1
	})

	t.Run("pagination stops on empty next", func(t *testing.T) {
		pageCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pageCount++
			if pageCount > 1 {
				t.Fatalf("Should only request page 1, got page %d", pageCount)
			}
			response := OTXResponse{
				Count: 1,
				Next:  "", // Empty next
				Results: []OTXPulse{
					{ID: "single-pulse", Name: "Single Page", Indicators: []OTXIndicator{
						{Type: "IPv4", Indicator: "9.9.9.9"},
					}},
				},
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := &config.Config{
			Providers: map[string]*config.ProviderOptions{
				"alienvault": {
					Enabled:   boolPtr(true),
					SourceURL: server.URL,
					APIKey:    "test-api-key",
				},
			},
		}

		customColly := colly.NewCollector()
		customColly.AllowedDomains = []string{}

		provider := NewAlienvaultProvider(cfg, customColly)
		require.NotNil(t, provider)

		avProvider := provider.(*alienvaultProvider)
		avProvider.rateLimit = 10 * time.Millisecond

		reader, err := avProvider.Fetch()
		require.NoError(t, err)
		require.NotNil(t, reader)

		data, err := io.ReadAll(reader)
		require.NoError(t, err)

		var merged OTXResponse
		err = json.Unmarshal(data, &merged)
		require.NoError(t, err)
		assert.Equal(t, 1, merged.Count)
		assert.Len(t, merged.Results, 1)
	})
}

// Helper functions

func boolPtr(b bool) *bool {
	return &b
}

func findEntryByIndicator(entries []*entries.Entry, indicator string) *entries.Entry {
	for _, entry := range entries {
		if entry.Host == indicator || entry.SourceURL == indicator {
			return entry
		}
	}
	return nil
}

// mockEntryCollector implements entry_collector.Collector for testing
type mockEntryCollector struct {
	entries *[]*entries.Entry
}

func (m *mockEntryCollector) Submit(entry *entries.Entry) {
	if m.entries != nil {
		*m.entries = append(*m.entries, entry)
	}
}

func (m *mockEntryCollector) SubmitBatch(entries []*entries.Entry) {
	if m.entries != nil {
		*m.entries = append(*m.entries, entries...)
	}
}

func (m *mockEntryCollector) Close() {}

func (m *mockEntryCollector) Wait() {}

func (m *mockEntryCollector) GetProcessedCount(source string) int {
	return 0
}

func (m *mockEntryCollector) RemoveStaleEntriesAndSyncBloom(ctx context.Context, providerName, processID string) error {
	return nil
}

func (m *mockEntryCollector) StartProviderProcessing(providerName, processID string) {}

func (m *mockEntryCollector) FinishProviderProcessing(providerName, processID string) (count int, duration time.Duration, ok bool) {
	return 0, 0, false
}

