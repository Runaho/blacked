package sources

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

// Fetcher abstracts how data is retrieved from a Source URL.
type Fetcher interface {
	Fetch(url string) (io.Reader, error)
}

// CollyFetcher uses the Colly web scraper to fetch data.
type CollyFetcher struct {
	collyClient *colly.Collector
}

// NewCollyFetcher creates a new CollyFetcher wrapping an existing collector.
func NewCollyFetcher(client *colly.Collector) *CollyFetcher {
	return &CollyFetcher{collyClient: client}
}

// Fetch retrieves data from the given URL using Colly.
func (f *CollyFetcher) Fetch(url string) (io.Reader, error) {
	var responseBody []byte
	var fetchErr error

	c := f.collyClient.Clone()

	c.OnResponse(func(r *colly.Response) {
		responseBody = r.Body
		log.Info().
			Str("source", url).
			Int("bytes", len(responseBody)).
			Msg("Fetched data from source")
	})

	c.OnError(func(r *colly.Response, err error) {
		fetchErr = fmt.Errorf("colly error fetching %s: status_code=%d err=%w", url, r.StatusCode, err)
		log.Err(err).
			Str("url", url).
			Int("status_code", r.StatusCode).
			Msg("Error fetching source")
	})

	log.Info().Str("url", url).Msg("Fetching source")

	if err := c.Visit(url); err != nil {
		log.Err(err).Str("url", url).Msg("Failed to visit URL")
		return nil, fmt.Errorf("visit error: %w", err)
	}

	c.Wait()

	if fetchErr != nil {
		return nil, fetchErr
	}
	if len(responseBody) == 0 {
		log.Warn().Str("url", url).Msg("Empty response from source")
		return nil, fmt.Errorf("empty response from %s", url)
	}

	return bytes.NewReader(responseBody), nil
}

// HTTPFetcher uses the standard HTTP client to fetch data.
// This is simpler and avoids Colly's lifecycle for APIs.
type HTTPFetcher struct {
	client *HTTPClient
}

// HTTPClient wraps http.Client.
type HTTPClient struct {
	Timeout time.Duration
}

// NewHTTPFetcher creates a new HTTPFetcher with default timeout.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{
		client: &HTTPClient{Timeout: 30 * time.Second},
	}
}

// Fetch retrieves data from the given URL using standard HTTP GET.
func (f *HTTPFetcher) Fetch(url string) (io.Reader, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Blacked/1.0")

	client := &http.Client{Timeout: f.client.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Err(err).Str("url", url).Msg("HTTP fetch failed")
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warn().Str("url", url).Int("status", resp.StatusCode).Msg("HTTP non-success status")
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	log.Info().Str("url", url).Int("bytes", len(body)).Msg("HTTP fetched")
	return bytes.NewReader(body), nil
}
