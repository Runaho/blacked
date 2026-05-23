package alienvault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func TestAuthErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"alienvault": {
				Enabled: boolPtr(true),
				SourceURL: server.URL,
				APIKey: "wrong-key",
			},
		},
	}

	customColly := colly.NewCollector()
	customColly.AllowedDomains = []string{}

	provider := NewAlienvaultProvider(cfg, customColly)
	require.NotNil(t, provider)

	avProvider := provider.(*alienvaultProvider)
	avProvider.rateLimit = 10 * time.Millisecond

	start := time.Now()
	_, err := avProvider.Fetch()
	elapsed := time.Since(start)
	
	t.Logf("Elapsed: %v", elapsed)
	t.Logf("Error: %v", err)
	
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401", "error should contain 401, got: %v", err)
}