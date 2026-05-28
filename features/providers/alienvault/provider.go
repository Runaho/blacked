package alienvault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"blacked/internal/utils"

	"github.com/gocolly/colly/v2"
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
	Next    string     `json:"next"`
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

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
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
		rateLimit:    1 * time.Second, // 1 request per second (confirmed via load testing)
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

// FetchWithContext delegates to Fetch for context-aware timeout handling.
// Required because process.go calls FetchWithContext(), not Fetch().
// Without this override, BaseProvider.FetchWithContext() would use CollyClient.Clone()
// which inherits browser User-Agent and causes 403 Forbidden from OTX API.
func (p *alienvaultProvider) FetchWithContext(ctx context.Context) (io.Reader, error) {
	return p.Fetch()
}

// Fetch implements OTX API fetching with proper authentication, rate limiting,
// and automatic pagination through all subscribed pulses.
func (p *alienvaultProvider) Fetch() (io.Reader, error) {
	currentURL := p.SourceURL
	var allResults []OTXPulse

	pageCount := 0
	totalIndicators := 0

	for {
		// Apply rate limiting between requests
		if pageCount > 0 {
			time.Sleep(p.rateLimit)
		}

		// Retry loop for transient server errors
		maxRetries := 3
		var body []byte
		var fetchErr error
		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {
				sleepDuration := time.Duration(attempt*attempt) * time.Second
				log.Warn().
					Int("attempt", attempt+1).
					Int("max_retries", maxRetries).
					Dur("sleep", sleepDuration).
					Msg("Retrying after transient error")
				time.Sleep(sleepDuration)
			}

			// Always use fresh collector for OTX API — clone inherits browser UA from parent,
			// which OTX rejects with 403. Fresh collector with explicit headers works reliably.
			c := colly.NewCollector()
			c.MaxBodySize = 10 * 1024 * 1024
			c.AllowedDomains = []string{} // disable domain filter for API server

			// Set OTX API key header (API endpoint requires non-browser User-Agent)
			c.OnRequest(func(r *colly.Request) {
				r.Headers.Set("X-OTX-API-KEY", p.apiKey)
				r.Headers.Set("Accept", "application/json")
				r.Headers.Set("User-Agent", "blacked/1.0")
			})

			body = nil
			fetchErr = nil
			statusCode := 0
			c.OnResponse(func(r *colly.Response) {
				body = r.Body
				statusCode = r.StatusCode
				log.Info().
					Str("source", currentURL).
					Int("bytes", len(body)).
					Int("page", pageCount+1).
					Int("attempt", attempt+1).
					Int("status", statusCode).
					Msg("Fetched data from AlienVault OTX")
			})
			c.OnError(func(r *colly.Response, err error) {
				fetchErr = fmt.Errorf("colly error for %s (status %d): %w", currentURL, r.StatusCode, err)
				statusCode = r.StatusCode
				log.Err(err).Str("url", currentURL).Int("code", r.StatusCode).
					Int("attempt", attempt+1).
					Msg("Colly error when fetching data from AlienVault OTX")
			})

			log.Info().Msgf("Fetching page %d (attempt %d/%d): %s", pageCount+1, attempt+1, maxRetries, currentURL)
			if err := c.Visit(currentURL); err != nil {
				log.Err(err).Msgf("Visit error page %d", pageCount+1)
				// Visit error may contain status code (e.g. "Unauthorized" for 401)
				fetchErr = err
				// If the error message contains auth codes or "Unauthorized", return immediately
				errStr := err.Error()
				if strings.Contains(errStr, "401") || strings.Contains(errStr, "403") ||
					strings.Contains(errStr, "Unauthorized") || strings.Contains(errStr, "Forbidden") {
					authErr := fmt.Errorf("authentication failed for %s (status %d)", currentURL, statusCode)
					log.Error().Err(authErr).Msg("Visit returned auth error")
					return nil, authErr
				}
				continue
			}
			c.Wait()

			// Auth errors (401/403) — fail immediately without retry
			if statusCode == 401 || statusCode == 403 {
				authErr := fmt.Errorf("authentication failed for %s (status %d)", currentURL, statusCode)
				log.Error().Err(authErr).Msg("Authentication error — failing immediately")
				return nil, authErr
			}

			if fetchErr == nil && len(body) > 0 {
				// Verify it's valid JSON
				bodyMap := make(map[string]interface{})
				if err := json.Unmarshal(body, &bodyMap); err == nil {
					// Valid JSON — success
					break
				}
				// Invalid JSON — retry
				log.Warn().
					Int("attempt", attempt+1).
					Msg("Invalid JSON response — retrying")
				continue
			}

			// Check if it's a retryable error
			if fetchErr != nil {
				errStr := fetchErr.Error()
				isAuthError := strings.Contains(errStr, "401") || strings.Contains(errStr, "403") || statusCode == 401 || statusCode == 403
				if isAuthError {
					log.Error().Err(fetchErr).Msg("Authentication error — failing immediately")
					return nil, fetchErr
				}

				isRetryable := strings.Contains(errStr, "504") ||
					strings.Contains(errStr, "429") ||
					strings.Contains(errStr, "500") ||
					strings.Contains(errStr, "502") ||
					strings.Contains(errStr, "503")
				if !isRetryable || attempt == maxRetries-1 {
					// Retry exhausted or non-retryable error — return partial data
					log.Error().
						Err(fetchErr).
						Int("pages_fetched", pageCount).
						Int("total_results", len(allResults)).
						Int("attempt", attempt+1).
						Msg("Retry exhausted — returning partial data")
					finalResponse := OTXResponse{
						Count:   len(allResults),
						Next:    "",
						Results: allResults,
					}
					finalJSON, err := json.Marshal(finalResponse)
					if err != nil {
						return nil, fmt.Errorf("marshal partial response: %w", err)
					}
					return bytes.NewReader(finalJSON), nil
				}
			}
		}

		if len(body) == 0 || fetchErr != nil {
			if fetchErr != nil {
				return nil, fetchErr
			}
			return nil, fmt.Errorf("empty response from %s", currentURL)
		}

		// Parse response to check for next page
		var response OTXResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("unmarshal alienvault json: %w", err)
		}

		// Accumulate results from this page
		pageIndicators := 0
		for _, pulse := range response.Results {
			pageIndicators += len(pulse.Indicators)
		}
		allResults = append(allResults, response.Results...)
		totalIndicators += pageIndicators

		// Check for next page
		pageCount++
		nextURL := response.Next
		if nextURL == "" || nextURL == currentURL {
			log.Info().
				Int("pages", pageCount).
				Int("total_pulses", len(allResults)).
				Int("total_indicators", totalIndicators).
				Msg("Pagination complete — no more pages")
			break
		}

		currentURL = nextURL
		log.Info().Msgf("Moving to next page: %s", currentURL)
	}

	// Build final response with all results merged
	finalResponse := OTXResponse{
		Count:   len(allResults),
		Next:    "",
		Results: allResults,
	}
	finalJSON, err := json.Marshal(finalResponse)
	if err != nil {
		return nil, fmt.Errorf("marshal merged response: %w", err)
	}

	log.Info().
		Int("pages_fetched", pageCount).
		Int("total_results", len(allResults)).
		Int("total_indicators", totalIndicators).
		Msg("Built merged OTX response")

	return bytes.NewReader(finalJSON), nil
}

// FetchPages implements base.MultiPageProvider.
// Each page is saved to disk immediately after fetch (per-page persistence),
// then parsed and yielded as entries. Memory usage is bounded by single page size.
// On crash, ResumePageNumber detects the highest saved page and resumes from next.
// The process.go handler drives this via the MultiPageProvider interface.
func (p *alienvaultProvider) FetchPages(ctx context.Context) (<-chan base.PageParseResult, error) {
	cfg := config.GetConfig()
	storePath := cfg.Collector.StorePath
	collector := entry_collector.GetPondCollector()

	// Determine resume point — scan directory for highest existing page
	startPage, err := utils.ResumePageNumber(storePath, providerName)
	if err != nil {
		startPage = 1
		log.Warn().Err(err).Msg("ResumePageNumber failed, starting from page 1")
	}

	// Detect if this is a fresh run or resume
	freshRun := startPage == 1
	if !freshRun {
		log.Info().Int("resume_page", startPage).Msg("Resuming multi-page fetch from previous run")
	}

	currentURL := p.SourceURL
	resultChan := make(chan base.PageParseResult, 1)

	go func() {
		defer close(resultChan)

		pageCount := 0
		totalIndicators := 0

		// If resuming, read pages from disk and parse them, then continue from network.
		// Skip rate limit for disk reads. Use stored next_url to resume correctly.
		if !freshRun {
			meta, err := utils.GetPageMetadata(storePath, providerName)
			if err != nil || meta == nil || len(meta.Pages) < startPage-1 {
				log.Warn().
					Err(err).
					Int("start_page", startPage).
					Int("meta_pages", func() int { if meta == nil { return 0 }; return len(meta.Pages) }()).
					Msg("Cannot read meta or not enough pages — falling back to network fetch from start")
				startPage = 1
				currentURL = p.SourceURL
			} else {
				// Reconstruct URL chain from stored next_page_urls.
				// Read pages 1..startPage-1 from disk, parse and submit entries.
				for i := 1; i < startPage; i++ {
					pageInfo := meta.Pages[i-1]
					pagePath := storePath + "/" + providerName + "/" + pageInfo.File
					data, err := os.ReadFile(pagePath)
					if err != nil {
						log.Warn().Err(err).Int("page", i).Msg("Failed to read page from disk — skipping")
						continue
					}

					var response OTXResponse
					if err := json.Unmarshal(data, &response); err != nil {
						log.Warn().Err(err).Int("page", i).Msg("Failed to unmarshal page from disk — skipping")
						continue
					}

					pageIndicators := 0
					processID := ""
					if p.ProcessID != nil {
						processID = p.ProcessID.String()
					}
					for _, pulse := range response.Results {
						for _, indicator := range pulse.Indicators {
							entry, err := indicatorToEntry(&indicator, providerName, processID)
							if err != nil || entry == nil {
								continue
							}
							collector.Submit(entry)
							pageIndicators++
						}
					}
					pageCount++
					totalIndicators += pageIndicators

					log.Info().
						Int("page", i).
						Int("indicators", pageIndicators).
						Str("next_url", pageInfo.NextPageURL).
						Msg("Processed page from disk")
				}

				// Set currentURL for the next network fetch.
				// Use stored next_page_url from the last disk page if available,
				// otherwise fall back to SourceURL (network will detect if more pages exist).
				lastPageInfo := meta.Pages[startPage-2]
				if lastPageInfo.NextPageURL != "" {
					currentURL = lastPageInfo.NextPageURL
					log.Info().Str("resume_url", currentURL).Msg("Resuming from stored next_page_url")
				} else {
					// Disk page has no next_url — it may be the last page of a previous run,
					// or the previous run was interrupted before the next_url was stored.
					// Resume from SourceURL and let the API decide if there are more pages.
					currentURL = p.SourceURL
					log.Info().Msg("No stored next_page_url — resuming from SourceURL")
				}
			}
		}

		for {
			// Check context cancellation
			select {
			case <-ctx.Done():
				log.Warn().Int("pages_fetched", pageCount).Msg("context cancelled during multi-page fetch")
				return
			default:
			}

			// Apply rate limiting between network requests only (pageCount already
			// accounts for disk pages when resuming).
			if pageCount > 0 {
				time.Sleep(p.rateLimit)
			}

			pageCount++

			// Retry loop for transient server errors
			maxRetries := 3
			var body []byte
			var fetchErr error
			var statusCode int

			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					sleepDuration := time.Duration(attempt*attempt) * time.Second
					log.Warn().
						Int("attempt", attempt+1).
						Int("max_retries", maxRetries).
						Dur("sleep", sleepDuration).
						Msg("Retrying after transient error")
					time.Sleep(sleepDuration)
				}

				c := colly.NewCollector()
				c.MaxBodySize = 10 * 1024 * 1024
				c.AllowedDomains = []string{}

				c.OnRequest(func(r *colly.Request) {
					r.Headers.Set("X-OTX-API-KEY", p.apiKey)
					r.Headers.Set("Accept", "application/json")
					r.Headers.Set("User-Agent", "blacked/1.0")
				})

				body = nil
				fetchErr = nil
				statusCode = 0

				c.OnResponse(func(r *colly.Response) {
					body = r.Body
					statusCode = r.StatusCode
					log.Info().
						Str("source", currentURL).
						Int("bytes", len(body)).
						Int("page", pageCount).
						Int("attempt", attempt+1).
						Int("status", statusCode).
						Msg("Fetched page from AlienVault OTX")
				})

				c.OnError(func(r *colly.Response, err error) {
					fetchErr = fmt.Errorf("colly error for %s (status %d): %w", currentURL, r.StatusCode, err)
					statusCode = r.StatusCode
					log.Err(err).Str("url", currentURL).Int("code", r.StatusCode).
						Int("attempt", attempt+1).
						Msg("Colly error when fetching page from AlienVault OTX")
				})

				log.Info().Msgf("Fetching page %d (attempt %d/%d): %s", pageCount, attempt+1, maxRetries, currentURL)
				if err := c.Visit(currentURL); err != nil {
					log.Err(err).Msgf("Visit error page %d", pageCount)
					fetchErr = err
					errStr := err.Error()
					if strings.Contains(errStr, "401") || strings.Contains(errStr, "403") ||
						strings.Contains(errStr, "Unauthorized") || strings.Contains(errStr, "Forbidden") {
						authErr := fmt.Errorf("authentication failed for %s (status %d)", currentURL, statusCode)
						log.Error().Err(authErr).Msg("Authentication error — failing immediately")
						resultChan <- base.PageParseResult{PageNumber: pageCount, HasNextPage: false}
						return
					}
					continue
				}
				c.Wait()

				if statusCode == 401 || statusCode == 403 {
					authErr := fmt.Errorf("authentication failed for %s (status %d)", currentURL, statusCode)
					log.Error().Err(authErr).Msg("Authentication error — failing immediately")
					resultChan <- base.PageParseResult{PageNumber: pageCount, HasNextPage: false}
					return
				}

				if fetchErr == nil && len(body) > 0 {
					bodyMap := make(map[string]interface{})
					if err := json.Unmarshal(body, &bodyMap); err == nil {
						break
					}
					log.Warn().Int("attempt", attempt+1).Msg("Invalid JSON response — retrying")
					continue
				}

				if fetchErr != nil {
					errStr := fetchErr.Error()
					isAuthError := strings.Contains(errStr, "401") || strings.Contains(errStr, "403") || statusCode == 401 || statusCode == 403
					if isAuthError {
						log.Error().Err(fetchErr).Msg("Authentication error — failing immediately")
						resultChan <- base.PageParseResult{PageNumber: pageCount, HasNextPage: false}
						return
					}

					isRetryable := strings.Contains(errStr, "504") ||
						strings.Contains(errStr, "429") ||
						strings.Contains(errStr, "500") ||
						strings.Contains(errStr, "502") ||
						strings.Contains(errStr, "503")
					if !isRetryable || attempt == maxRetries-1 {
						log.Error().
							Err(fetchErr).
							Int("pages_fetched", pageCount).
							Int("attempt", attempt+1).
							Msg("Retry exhausted — ending fetch")
						resultChan <- base.PageParseResult{PageNumber: pageCount, HasNextPage: false}
						return
					}
				}
			}

			if len(body) == 0 || fetchErr != nil {
				log.Error().Err(fetchErr).Msgf("Empty response or error for page %d", pageCount)
				resultChan <- base.PageParseResult{PageNumber: pageCount, HasNextPage: false}
				return
			}

			// Parse response to extract next_url and indicators
			var response OTXResponse
			if err := json.Unmarshal(body, &response); err != nil {
				log.Err(err).Msgf("Failed to unmarshal page %d", pageCount)
				resultChan <- base.PageParseResult{PageNumber: pageCount, HasNextPage: false}
				return
			}

			// Save page to disk immediately (per-page persistence), including next_page_url
			fetchedAt := time.Now()
			nextPageURL := response.Next
			if _, err := utils.SavePageData(storePath, providerName, pageCount, body, 0, fetchedAt, nextPageURL); err != nil {
				log.Warn().Err(err).Int("page", pageCount).Msg("Failed to save page to disk — continuing anyway")
			}

			// Parse this page's indicators and submit to collector
			pageIndicators := 0
			processID := ""
			if p.ProcessID != nil {
				processID = p.ProcessID.String()
			}

			for _, pulse := range response.Results {
				for _, indicator := range pulse.Indicators {
					entry, err := indicatorToEntry(&indicator, providerName, processID)
					if err != nil || entry == nil {
						continue
					}
					collector.Submit(entry)
					pageIndicators++
				}
			}

			totalIndicators += pageIndicators

			// Determine if there is a next page.
			// response.Next == "" means no more pages. But if currentURL != p.SourceURL
			// (i.e., we resumed from a stored next_url), empty response.Next means
			// the previous fetch was interrupted mid-page — we should NOT treat this as done.
			hasNext := response.Next != "" && response.Next != currentURL
			nextURL := response.Next

			if !hasNext {
				// If we are on a resumed URL (not the first page) and the response has no next_url,
				// this page was already fetched — the previous run stored it with next_url="".
				// It is not a new page, so we are done.
				if pageCount >= startPage && currentURL != p.SourceURL {
					log.Info().
						Int("pages_fetched", pageCount).
						Int("total_indicators", totalIndicators).
						Int("bytes", len(body)).
						Msg("Multi-page fetch complete")
					resultChan <- base.PageParseResult{
						PageNumber:    pageCount,
						Indicators:    pageIndicators,
						Bytes:         int64(len(body)),
						FetchedAt:     fetchedAt,
						HasNextPage:   false,
						NextPageURL:   "",
						Entries:       nil,
					}
					return
				}
				// Fresh run or first page: no more pages from API
				log.Info().
					Int("pages_fetched", pageCount).
					Int("total_indicators", totalIndicators).
					Int("bytes", len(body)).
					Msg("Multi-page fetch complete")
				resultChan <- base.PageParseResult{
					PageNumber:    pageCount,
					Indicators:    pageIndicators,
					Bytes:         int64(len(body)),
					FetchedAt:     fetchedAt,
					HasNextPage:   false,
					NextPageURL:   "",
					Entries:       nil,
				}
				return
			}

			// Signal this page is done, provider will fetch next
			resultChan <- base.PageParseResult{
				PageNumber:    pageCount,
				Indicators:    pageIndicators,
				Bytes:         int64(len(body)),
				FetchedAt:     fetchedAt,
				HasNextPage:   true,
				NextPageURL:   nextURL,
				Entries:       nil,
			}

			currentURL = nextURL
			log.Info().Msgf("Moving to next page: %s", currentURL)
		}
	}()

	return resultChan, nil
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
		// Handle IP addresses directly - no URL parsing needed
		host := strings.TrimSpace(indicator.Indicator)
		if host == "" {
			log.Debug().Msg("empty IP indicator — skipping")
			return nil, nil
		}

		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory("malicious_ip").
			WithIP(host)

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
