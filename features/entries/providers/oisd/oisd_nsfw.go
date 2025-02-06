package oisd

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

type OISDNSFW struct {
	settings    *config.CollectorConfig
	processID   *uuid.UUID
	collyClient *colly.Collector
	repository  repository.BlacklistRepository
}

func NewOISDNSFWProvider(
	settings *config.CollectorConfig,
	collyClient *colly.Collector,
	repository repository.BlacklistRepository,
) *OISDNSFW {
	return &OISDNSFW{
		settings:    settings,
		collyClient: collyClient,
		repository:  repository,
	}
}

func (o *OISDNSFW) Name() string {
	return "OISD_NSFW"
}

func (o *OISDNSFW) Source() string {
	return "https://nsfw.oisd.nl/domainswild2"
}

func (o *OISDNSFW) Fetch() (io.Reader, error) {
	var responseBody []byte
	var fetchErr error

	c := o.collyClient.Clone()
	c.OnResponse(func(r *colly.Response) {
		responseBody = r.Body
		log.Info().Msgf("Fetched %d bytes from %s", len(responseBody), o.Source())
	})

	c.OnError(func(r *colly.Response, err error) {
		fetchErr = fmt.Errorf("colly error for URL %s, status code: %d, error: %w", r.Request.URL, r.StatusCode, err)
		log.Error().Err(err).Msgf("colly error fetching %s, status code: %d", r.Request.URL, r.StatusCode)
	})

	log.Info().Msgf("Fetching %s", o.Source())
	if err := c.Visit(o.Source()); err != nil {
		return nil, fmt.Errorf("error visiting URL %s: %w", o.Source(), err)
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

func (o *OISDNSFW) Parse(data io.Reader) error {
	ctx := context.Background()
	startsAt := time.Now()
	processID := o.GetProcessID()
	scanner := bufio.NewScanner(data)
	entryBatch := make([]entries.Entry, 0, o.settings.BatchSize)

	entriesProcessed := 0

	mc, err := collector.GetMetricsCollector()

	if mc != nil && err == nil {
		mc.SetSyncRunning(o.Name())
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
		_entry.Source = o.Name()
		_entry.SourceURL = o.Source()
		_entry.CreatedAt = time.Now()
		_entry.UpdatedAt = time.Now()
		_entry.Category = "nsfw"

		err := _entry.SetURL(line)

		entry := *_entry

		if err != nil {
			log.Error().Err(err).Msgf("error setting URL: %s", line)
			mc.IncrementImportErrors(o.Name())
			continue
		}

		entryBatch = append(entryBatch, entry)

		if len(entryBatch) >= o.settings.BatchSize {
			if err := o.repository.BatchSaveEntries(ctx, entryBatch); err != nil {
				if mc != nil {
					since := time.Since(scanningAt)
					mc.SetSyncFailed(o.Name(), err, since)
				}
				return fmt.Errorf("error batch saving entries: %w", err)
			}
			entriesProcessed += len(entryBatch)
			if mc != nil {
				mc.IncrementInsertedCount(o.Name(), len(entryBatch))
			}
			entryBatch = entryBatch[:0]
		}
	}

	// Save remaining batch
	if len(entryBatch) > 0 {
		if err := o.repository.BatchSaveEntries(ctx, entryBatch); err != nil {
			if mc != nil {
				mc.SetSyncFailed(o.Name(), err, time.Since(startsAt))
			}
			return fmt.Errorf("error batch saving final entries: %w", err)
		}
		entriesProcessed += len(entryBatch)
		if mc != nil {
			mc.IncrementInsertedCount(o.Name(), len(entryBatch))
		}
	}

	if err := scanner.Err(); err != nil {
		if mc != nil {
			mc.SetSyncFailed(o.Name(), err, time.Since(startsAt))
		}
		return fmt.Errorf("scanner error: %w", err)
	}

	if mc != nil {
		mc.SetSyncSuccess(o.Name(), time.Since(startsAt))
		mc.SetTotalProcessed(o.Name(), entriesProcessed)
	}

	duration := time.Since(startsAt)
	log.Info().Msgf("OISD Big Provider: Processed and batch-saved %d entries in %v (with metrics updated, processID: %s)\n", entriesProcessed, duration, processID.String())
	return nil
}

func (o *OISDNSFW) GetProcessID() uuid.UUID {
	if o.processID == nil {
		newProcessID := uuid.New()
		o.processID = &newProcessID
	}

	return *o.processID
}

func (o *OISDNSFW) SetProcessID(id uuid.UUID) {
	o.processID = &id
}
