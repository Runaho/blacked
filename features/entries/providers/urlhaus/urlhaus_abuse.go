package urlhaus

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/collector"
	"blacked/internal/config"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type URLHausProvider struct {
	settings    *config.CollectorConfig
	processID   *uuid.UUID
	collyClient *colly.Collector
	repository  repository.BlacklistRepository
}

func NewURLHausProvider(
	settings *config.CollectorConfig,
	collyClient *colly.Collector,
	repository repository.BlacklistRepository,
) *URLHausProvider {
	return &URLHausProvider{
		settings:    settings,
		collyClient: collyClient,
		repository:  repository,
	}
}

func (u *URLHausProvider) Name() string {
	return "URLHAUS"
}

func (u *URLHausProvider) Source() string {
	return "https://urlhaus.abuse.ch/downloads/text/"
}

func (u *URLHausProvider) Fetch() (io.Reader, error) {
	var responseBody []byte
	var fetchErr error

	c := u.collyClient.Clone()
	c.OnResponse(func(r *colly.Response) {
		responseBody = r.Body
		log.Info().Msgf("Fetched %d bytes from %s", len(responseBody), u.Source())
	})

	c.OnError(func(r *colly.Response, err error) {
		fetchErr = fmt.Errorf("colly error for URL %s, status code: %d, error: %w", r.Request.URL, r.StatusCode, err)
		log.Error().Err(err).Msgf("colly error fetching %s, status code: %d", r.Request.URL, r.StatusCode)
	})

	log.Info().Msgf("Fetching %s", u.Source())
	if err := c.Visit(u.Source()); err != nil {
		return nil, fmt.Errorf("error visiting URL %s: %w", u.Source(), err)
	}

	// If you’re using c.Async = true, don’t forget to wait:
	c.Wait()

	// If there was an error from OnError, return it
	if fetchErr != nil {
		return nil, fetchErr
	}

	// Return the downloaded data as an io.Reader
	return bytes.NewReader(responseBody), nil
}

func (u *URLHausProvider) Parse(data io.Reader) error {
	ctx := context.Background()
	startsAt := time.Now()
	processID := u.GetProcessID()
	scanner := bufio.NewScanner(data)
	entryBatch := make([]entries.Entry, 0, u.settings.BatchSize)

	entriesProcessed := 0

	mc, err := collector.GetMetricsCollector()

	if mc != nil && err == nil {
		mc.SetSyncRunning(u.Name())
	}

	for scanner.Scan() {
		scanningAt := time.Now()
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		_entry := new(entries.Entry)
		_entry.ID = uuid.New().String()
		_entry.ProcessID = processID.String()
		_entry.Source = u.Name()
		_entry.SourceURL = u.Source()
		_entry.CreatedAt = time.Now()
		_entry.UpdatedAt = time.Now()

		err := _entry.SetURL(line)

		entry := *_entry

		if err != nil {
			log.Error().Err(err).Msgf("error setting URL: %s", line)
			mc.IncrementImportErrors(u.Name())
			continue
		}

		entryBatch = append(entryBatch, entry)

		if len(entryBatch) >= u.settings.BatchSize {
			if err := u.repository.BatchSaveEntries(ctx, entryBatch); err != nil {
				if mc != nil {
					since := time.Since(scanningAt)
					mc.SetSyncFailed(u.Name(), err, since)
				}
				return fmt.Errorf("error batch saving entries: %w", err)
			}
			entriesProcessed += len(entryBatch)
			if mc != nil {
				mc.IncrementInsertedCount(u.Name(), len(entryBatch))
			}
			entryBatch = entryBatch[:0]
		}
	}

	// Save remaining batch
	if len(entryBatch) > 0 {
		if err := u.repository.BatchSaveEntries(ctx, entryBatch); err != nil {
			if mc != nil {
				mc.SetSyncFailed(u.Name(), err, time.Since(startsAt))
			}
			return fmt.Errorf("error batch saving final entries: %w", err)
		}
		entriesProcessed += len(entryBatch)
		if mc != nil {
			mc.IncrementInsertedCount(u.Name(), len(entryBatch))
		}
	}

	if err := scanner.Err(); err != nil {
		if mc != nil {
			mc.SetSyncFailed(u.Name(), err, time.Since(startsAt))
		}
		return fmt.Errorf("scanner error: %w", err)
	}

	if mc != nil {
		mc.SetSyncSuccess(u.Name(), time.Since(startsAt))
		mc.SetTotalProcessed(u.Name(), entriesProcessed)
	}

	duration := time.Since(startsAt)
	log.Info().Msgf("URLHaus Provider: Processed and batch-saved %d entries in %v (with metrics updated, processID: %s)\n", entriesProcessed, duration, processID.String())
	return nil
}

func (u *URLHausProvider) GetProcessID() uuid.UUID {
	if u.processID == nil {
		newProcessID := uuid.New()
		u.processID = &newProcessID
	}

	return *u.processID
}

func (u *URLHausProvider) SetProcessID(id uuid.UUID) {
	u.processID = &id
}
