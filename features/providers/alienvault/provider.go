package alienvault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const providerName = "alienvault"

// --- OTX API Response Types ---

type OTXPulse struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Created     string      `json:"created,omitempty"`
	Modified    string      `json:"modified,omitempty"`
	Indicators  []OTXIndicator `json:"indicators,omitempty"`
}

type OTXIndicator struct {
	Type       string `json:"type"`
	Indicator  string `json:"indicator"`
	Title      string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Created    string `json:"created,omitempty"`
}

type OTXResponse struct {
	Count   int        `json:"count"`
	Results []OTXPulse `json:"results"`
}

// alienvaultProvider wraps BaseProvider with OTX-specific logic
type alienvaultProvider struct {
	*base.BaseProvider
	apiKey    string
	rateLimit time.Duration
}

// --- Public constructor ---

func NewAlienvaultProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	opts, ok := cfg.Providers[providerName]
	if !ok || opts == nil {
		opts = &config.ProviderOptions{}
	}
	if opts.Enabled != nil && !*opts.Enabled {
		log.Info().Str("provider", providerName).Msg("provider disabled — skipping")
		return nil
	}

	apiURL := opts.SourceURL
	if apiURL == "" {
		apiURL = "https://otx.alienvault.com/api/v1/pulses/subscribed"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "0 */6 * * *" // Every 6 hours (respecting rate limit)
	}
	category := opts.Category
	if category == "" {
		category = "threat_intel"
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)
	if client != nil {
		client.MaxBodySize = 10 * 1024 * 1024 // 10 MB
	}

	processID := uuid.New().String()
	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		raw, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("read alienvault data: %w", err)
		}
		return parseAlienvaultResponse(raw, collector, providerName, processID)
	}

	bp := base.NewBaseProvider(providerName, apiURL, category, client, parseFunc)
	bp.SetCronSchedule(cron)

	p := &alienvaultProvider{
		BaseProvider: bp,
		apiKey:       opts.APIKey,
		rateLimit:     10 * time.Second, // 1 request per 10 seconds
	}
	p.Register()
	return p
}

// Register wraps the base Register so the registry stores alienvaultProvider
// (with overridden Fetch), not the embedded BaseProvider.
func (p *alienvaultProvider) Register() *base.BaseProvider {
	base.RegisterProvider(p)
	return p.BaseProvider
}

// Fetch implements OTX API fetching with proper authentication and rate limiting
func (p *alienvaultProvider) Fetch() (io.Reader, error) {
	targetURL := p.SourceURL

	// Apply rate limiting
	time.Sleep(p.rateLimit)

	c := p.CollyClient.Clone()
	if c == nil {
		c = colly.NewCollector()
	}
	c.MaxBodySize = 10 * 1024 * 1024

	// Set OTX API key header
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("X-OTX-API-KEY", p.apiKey)
		r.Headers.Set("Accept", "application/json")
	})

	var body []byte
	var fetchErr error
	c.OnResponse(func(r *colly.Response) {
		body = r.Body
		log.Info().Str("source", targetURL).Int("bytes", len(body)).
			Msg("Fetched data from AlienVault OTX")
	})
	c.OnError(func(r *colly.Response, err error) {
		fetchErr = fmt.Errorf("colly error for %s (status %d): %w", targetURL, r.StatusCode, err)
		log.Err(err).Str("url", targetURL).Int("code", r.StatusCode).
			Msg("Colly error when fetching data from AlienVault OTX")
	})

	log.Info().Msgf("Fetching %s", targetURL)
	if err := c.Visit(targetURL); err != nil {
		return nil, fmt.Errorf("visit %s: %w", targetURL, err)
	}
	c.Wait()

	if fetchErr != nil {
		return nil, fetchErr
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response from %s", targetURL)
	}
	return bytes.NewReader(body), nil
}

// --- Response parsing ---

func parseAlienvaultResponse(data []byte, collector entry_collector.Collector, source, processID string) error {
	var response OTXResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("unmarshal alienvault json: %w", err)
	}

	var totalEntries, skippedCount int
	for _, pulse := range response.Results {
		for _, indicator := range pulse.Indicators {
			entry, err := indicatorToEntry(&indicator, source, processID)
			if err != nil {
				skippedCount++
				continue
			}
			if entry != nil {
				collector.Submit(entry)
				totalEntries++
			} else {
				skippedCount++
			}
		}
	}

	log.Info().
		Int("entries", totalEntries).
		Int("skipped", skippedCount).
		Msg("alienvault parse complete")

	return nil
}

// --- Indicator → Entry mapping ---

func indicatorToEntry(indicator *OTXIndicator, source, processID string) (*entries.Entry, error) {
	// Map OTX indicator types to appropriate entry types
	switch indicator.Type {
	case "IPv4", "IPv6":
		// Handle IP addresses
		host := strings.TrimSpace(indicator.Indicator)
		if host == "" {
			log.Debug().Msg("empty IP indicator — skipping")
			return nil, nil
		}
		
		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory("malicious_ip")
		
		// Set as URL with // prefix for IP handling
		if err := entry.SetURL("//" + host); err != nil {
			// If SetURL fails (e.g., for IPv6 format issues), create entry manually
			entry.Host = host
			entry.Domain = host
			entry.SubDomains = nil
			entry.Path = ""
			entry.RawQuery = ""
			return entry, nil
		}
		// Override domain extraction for IPs
		entry.Domain = host
		entry.SubDomains = nil
		return entry, nil

	case "domain":
		if indicator.Indicator == "" || indicator.Indicator == "." {
			log.Debug().Msg("empty domain indicator — skipping")
			return nil, nil
		}
		
		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory("malicious_domain")
		
		if err := entry.SetURL(indicator.Indicator); err != nil {
			return nil, nil
		}
		return entry, nil

	case "hostname":
		if indicator.Indicator == "" {
			log.Debug().Msg("empty hostname indicator — skipping")
			return nil, nil
		}
		
		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory("malicious_hostname")
		
		if err := entry.SetURL("//" + indicator.Indicator); err != nil {
			return nil, nil
		}
		return entry, nil

	case "URL":
		if indicator.Indicator == "" {
			log.Debug().Msg("empty URL indicator — skipping")
			return nil, nil
		}
		
		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory("malicious_url")
		
		if err := entry.SetURL(indicator.Indicator); err != nil {
			return nil, nil
		}
		return entry, nil

	default:
		// Skip other indicator types (hashes, etc.)
		log.Debug().Str("type", indicator.Type).Str("indicator", indicator.Indicator).
			Msg("unsupported indicator type — skipping")
		return nil, nil
	}
}

// HTTPFetcherWithAuth provides an alternative fetcher for AlienVault OTX
// that uses net/http directly with proper authentication
type HTTPFetcherWithAuth struct {
	client    *http.Client
	userAgent string
	apiKey    string
}

func NewHTTPFetcherWithAuth(apiKey string) *HTTPFetcherWithAuth {
	return &HTTPFetcherWithAuth{
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		apiKey:    apiKey,
	}
}

func (f *HTTPFetcherWithAuth) Fetch(u string) (io.ReadCloser, error) {
	time.Sleep(10 * time.Second) // Rate limiting

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("X-OTX-API-KEY", f.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return resp.Body, nil
}