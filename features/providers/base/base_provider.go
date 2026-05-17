package base

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"blacked/features/entries/repository"
	"blacked/features/entry_collector"
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var (
	// Fetch errors
	ErrFetchingSource = errors.New("error fetching source data")
	ErrVisitingURL    = errors.New("error visiting URL")
	ErrEmptyResponse  = errors.New("empty response from source")

	// Parse errors
	ErrParsingData   = errors.New("error parsing source data")
	ErrInvalidFormat = errors.New("invalid data format from source")

	// Repository errors
	ErrBatchSaving      = errors.New("error saving batch entries")
	ErrRepositoryNotSet = errors.New("repository not set")

	// Process errors
	ErrProcessIDNotSet = errors.New("process ID not set")
)

type Provider interface {
	GetName() string
	Source() string
	Fetch() (io.Reader, error)
	Parse(data io.Reader) error
	SetProcessID(id uuid.UUID)
	SetRepository(repository repository.BlacklistRepository)
	GetCronSchedule() string
	SetCronSchedule(cron string) *BaseProvider
	Register() *BaseProvider
	GetProcessID() uuid.UUID
	SetCollyClient(collyClient *colly.Collector)
}

type BaseProvider struct {
	Name          string
	SourceURL     string
	Category      string
	ProcessID     *uuid.UUID
	CollyClient   *colly.Collector
	CronSchedule  string
	RateLimit     time.Duration
	Repository    repository.BlacklistRepository
	ParseFunction func(io.Reader, entry_collector.Collector) error
}

// NewBaseProvider creates a new BaseProvider
func NewBaseProvider(name, sourceURL, category string, collyClient *colly.Collector, parseFunc func(io.Reader, entry_collector.Collector) error) *BaseProvider {
	p := &BaseProvider{
		Name:        name,
		SourceURL:   sourceURL,
		Category:    category,
		CollyClient: collyClient,
		ParseFunction: parseFunc,
	}

	return p
}

func (b *BaseProvider) Register() *BaseProvider {
	RegisterProvider(b)
	return b
}

// GetName returns the provider name
func (b *BaseProvider) GetName() string {
	return b.Name
}

// Source returns the source URL
func (b *BaseProvider) Source() string {
	return b.SourceURL
}

// Category returns the default category for this provider
func (b *BaseProvider) GetCategory() string {
	return b.Category
}

// SetRepository sets the repository
func (b *BaseProvider) SetRepository(repository repository.BlacklistRepository) {
	b.Repository = repository
}

// SetCollyClient sets the colly client
func (b *BaseProvider) SetCollyClient(collyClient *colly.Collector) {
	b.CollyClient = collyClient
}

// GetProcessID returns the process ID
func (b *BaseProvider) GetProcessID() uuid.UUID {
	if b.ProcessID == nil {
		newProcessID := uuid.New()
		b.ProcessID = &newProcessID
	}
	return *b.ProcessID
}

// SetProcessID sets the process ID
func (b *BaseProvider) SetProcessID(id uuid.UUID) {
	b.ProcessID = &id
}

// GetCRONScedule returns the CRON schedule
func (b *BaseProvider) GetCronSchedule() string {
	return GetProviderSchedule(b.Name)
}

// SetCRONScedule sets the CRON schedule
func (b *BaseProvider) SetCronSchedule(cron string) *BaseProvider {
	b.CronSchedule = cron
	return b
}

// Fetch retrieves data from source URL
func (b *BaseProvider) Fetch() (io.Reader, error) {
	var responseBody []byte
	var fetchErr error

	c := b.CollyClient.Clone()
	c.OnResponse(func(r *colly.Response) {
		responseBody = r.Body
		log.Info().
			Str("source", b.SourceURL).
			Int("bytes", len(responseBody)).
			Msg("Fetched data from source")
	})

	c.OnError(func(r *colly.Response, err error) {
		fetchErr = ErrFetchingSource
		log.Err(err).
			Str("url", r.Request.URL.String()).
			Int("status_code", r.StatusCode).
			Msg("Colly error when fetching data")
	})

	log.Info().Msgf("Fetching %s", b.SourceURL)
	if err := c.Visit(b.SourceURL); err != nil {
		log.Err(err).Str("url", b.SourceURL).Msg("Failed to visit URL")
		return nil, ErrVisitingURL
	}

	c.Wait()

	if fetchErr != nil {
		return nil, fetchErr
	}

	if len(responseBody) == 0 {
		log.Error().Str("url", b.SourceURL).Msg("Empty response from source")
		return nil, ErrEmptyResponse
	}

	return bytes.NewReader(responseBody), nil
}

// Parse processes the fetched data
func (b *BaseProvider) Parse(data io.Reader) error {
	if b.Repository == nil {
		log.Error().Str("provider", b.Name).Msg("Repository not set")
		return ErrRepositoryNotSet
	}

	if b.ParseFunction == nil {
		log.Error().Str("provider", b.Name).Msg("Parse function not set")
		return ErrParsingData
	}

	collector := entry_collector.GetPondCollector()
	if collector == nil {
		log.Error().Str("provider", b.Name).Msg("Entry collector not set")
		return ErrParsingData
	}

	err := b.ParseFunction(data, collector)
	if err != nil {
		log.Err(err).Str("provider", b.Name).Msg("Error parsing data")
		return ErrParsingData
	}

	return nil
}

// BuildCollyClientForProvider clones the global colly client and applies per-provider overrides.
func BuildCollyClientForProvider(client *colly.Collector, opts *config.ProviderOptions) *colly.Collector {
	if client == nil {
		return nil
	}
	if opts == nil {
		return client.Clone()
	}

	c := client.Clone()

	if opts.UserAgent != "" {
		c.UserAgent = opts.UserAgent
	}
	if opts.Timeout != nil {
		c.SetRequestTimeout(*opts.Timeout)
	}
	if opts.MaxRedirects > 0 {
		// colly doesn't expose a way to change MaxDepth after creation;
		// we rely on the clone inheriting the original; warn if mismatch.
		log.Debug().
			Str("provider", opts.SourceURL).
			Int("max_redirects", opts.MaxRedirects).
			Msg("max_redirects override requested but colly doesn't support post-creation change; using cloned defaults")
	}

	return c
}

// ResolveURL replaces the {api_key} placeholder if an API key is configured.
func ResolveURL(sourceURL, apiKey string) string {
	if apiKey == "" {
		return sourceURL
	}
	return strings.ReplaceAll(sourceURL, "{api_key}", apiKey)
}

// HTTPFetcher provides a colly-independent fetcher that uses net/http directly.
// Useful when colly's overhead isn't needed (plain text feeds).
type HTTPFetcher struct {
	client    *http.Client
	userAgent string
}

// NewHTTPFetcher creates an HTTPFetcher with optional overrides.
func NewHTTPFetcher(timeout time.Duration, userAgent string, maxRedirects int) *HTTPFetcher {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	if maxRedirects <= 0 {
		maxRedirects = 5
	}

	return &HTTPFetcher{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		userAgent: userAgent,
	}
}

// Fetch performs a GET request and returns the response body.
func (f *HTTPFetcher) Fetch(u string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)

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

// IsProviderURL checks if a string looks like a valid HTTP/HTTPS URL.
func IsProviderURL(s string) bool {
	if s == "" || s == "{api_key}" {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
