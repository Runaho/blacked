package base

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/collector"
	"blacked/internal/config"
	"bytes"
	"context"
	"errors"
	"io"
	"time"

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
	Settings      *config.CollectorConfig
	ProcessID     *uuid.UUID
	CollyClient   *colly.Collector
	CronSchedule  string
	RateLimit     time.Duration
	Repository    repository.BlacklistRepository
	ParseFunction func(io.Reader) ([]entries.Entry, error)
}

// NewBaseProvider creates a new BaseProvider
func NewBaseProvider(name, sourceURL string, settings *config.CollectorConfig, collyClient *colly.Collector, parseFunc func(io.Reader) ([]entries.Entry, error)) *BaseProvider {
	p := &BaseProvider{
		Name:          name,
		SourceURL:     sourceURL,
		Settings:      settings,
		CollyClient:   collyClient,
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

	ctx := context.Background()
	startsAt := time.Now()
	processID := b.GetProcessID()

	entries, err := b.ParseFunction(data)
	if err != nil {
		log.Err(err).Str("provider", b.Name).Msg("Error parsing data")
		return ErrParsingData
	}

	totalProcessed := 0
	entriesLen := len(entries)
	for i := 0; i < entriesLen; i += b.Settings.BatchSize {
		end := min(i+b.Settings.BatchSize, entriesLen)

		batch := entries[i:end]
		if err := b.Repository.BatchSaveEntries(ctx, batch); err != nil {
			log.Err(err).
				Str("provider", b.Name).
				Int("batch_start", i).
				Int("batch_end", end).
				Int("batch_size", len(batch)).
				Msg("Failed to save batch entries")

			return ErrBatchSaving
		}

		totalProcessed += len(batch)

		mc, _ := collector.GetMetricsCollector()
		if mc != nil {
			mc.IncrementSavedCount(b.Name, len(batch))
		}
	}

	mc, _ := collector.GetMetricsCollector()
	if mc != nil {
		mc.SetTotalProcessed(b.Name, totalProcessed)
	}

	duration := time.Since(startsAt)
	log.Info().
		Str("provider", b.Name).
		Int("entries", totalProcessed).
		Dur("duration", duration).
		Str("processID", processID.String()).
		Msg("Provider processing completed")

	return nil
}
