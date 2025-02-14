package providers

import (
	"blacked/features/entries/repository"
	"blacked/features/providers/oisd"
	"blacked/features/providers/openphish"
	"blacked/features/providers/urlhaus"
	"blacked/internal/collector"
	"blacked/internal/colly"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/utils"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type Provider interface {
	Name() string
	Source() string
	Fetch() (io.Reader, error)
	Parse(data io.Reader) error
	SetProcessID(id uuid.UUID)
	SetRepository(repository repository.BlacklistRepository)
}

type Providers []Provider

func NewProviders() (*Providers, error) {
	cfg := config.GetConfig()

	cc, err := colly.InitCollyClient()
	if err != nil {
		log.Error().Err(err).Msg("error initializing colly client")
		return nil, err
	}

	providers := &Providers{
		oisd.NewOISDBigProvider(&cfg.Collector, cc),
		oisd.NewOISDNSFWProvider(&cfg.Collector, cc),
		openphish.NewOpenPhishFeedProvider(&cfg.Collector, cc),
		urlhaus.NewURLHausProvider(&cfg.Collector, cc),
	}

	// Example: Collect their source URLs for logging or metrics
	srcs := providers.Sources()
	log.Info().Msgf("initialized provider sources: %v", srcs)

	// Also gather their domains for the Colly AllowedDomains
	sourceDomains, err := providers.SourceDomains()
	if err != nil {
		log.Error().Err(err).Msg("error getting source domains")
		return providers, err
	}
	cc.AllowedDomains = sourceDomains
	log.Info().Msgf("initialized provider source domains: %v", sourceDomains)

	// Initialize Prometheus metrics with the source names.
	collector.NewMetricsCollector(srcs)

	return providers, nil
}

// Process is where we actually perform any DB‐writing logic. We open a short‐lived
// read‐write connection, create a repository that uses it, then iteratively fetch
// and parse data from each provider. Now processes providers concurrently.
func (p Providers) Process() error {
	rwDB, err := db.GetReadWriteDB()
	if err != nil {
		return fmt.Errorf("failed to open read-write database: %w", err)
	}
	defer rwDB.Close()

	repo := repository.NewSQLiteRepository(rwDB)
	ctx := context.Background()
	var wg sync.WaitGroup
	errChan := make(chan error, len(p)) // Buffered channel to collect errors

	for _, provider := range p {
		wg.Add(1)
		go p.processProvider(ctx, provider, repo, &wg, errChan) // Launch goroutine for each provider
	}

	wg.Wait()      // Wait for all provider processing to complete
	close(errChan) // Close error channel after all goroutines finish

	var aggregatedError error
	for err := range errChan { // Collect errors from channel
		if err != nil {
			aggregatedError = errors.Join(aggregatedError, err) // Use errors.Join to combine errors
		}
	}

	if aggregatedError != nil {
		return fmt.Errorf("errors during provider processing: %w", aggregatedError) // Return aggregated error if any
	}

	fmt.Println("Blacklist entries processed successfully.")
	return nil
}

// processProvider handles the processing logic for a single provider.
func (p Providers) processProvider(ctx context.Context, provider Provider, repo repository.BlacklistRepository, wg *sync.WaitGroup, errChan chan error) {
	defer wg.Done()

	processID := uuid.New()
	startedAt := time.Now()

	source := provider.Source()
	name := provider.Name()
	strProcessID := processID.String()

	log.Info().
		Str("process_id", strProcessID).
		Str("source", source).
		Str("name", name).
		Time("starts", startedAt).
		Msg("start processing data")

	provider.SetProcessID(processID)

	reader, meta, err := utils.GetResponseReader(source, provider.Fetch, name, strProcessID)
	if err != nil {
		log.Error().
			Err(err).
			Str("process_id", strProcessID).
			Str("source", source).
			Str("name", name).
			Msg("error fetching data")
		errChan <- fmt.Errorf("error fetching data from %s: %w", provider.Source(), err)
		return
	}

	if meta != nil {
		log.Info().
			Str("process_id", strProcessID).
			Str("source", source).
			Str("name", name).
			TimeDiff("duration", time.Now(), startedAt).
			Msg("Found meta data for the process; changing process ID")
		strProcessID = meta.ProcessID
		provider.SetProcessID(uuid.MustParse(strProcessID))
	}

	provider.SetRepository(repo)

	if err := provider.Parse(reader); err != nil {
		log.Error().
			Err(err).
			Str("process_id", strProcessID).
			Str("source", source).
			Str("name", name).
			Msg("error parsing data")
		errChan <- fmt.Errorf("error parsing data from %s: %w", provider.Name(), err)
		return
	}

	cfg := config.GetConfig()
	if cfg.APP.Environtment != "development" {
		utils.RemoveStoredResponse(name)
	}

	log.Info().
		Str("process_id", strProcessID).
		Str("source", source).
		Str("name", name).
		TimeDiff("duration", time.Now(), startedAt).
		Msg("finished processing data")
}

func (p Providers) NamesAndSources() map[string]string {
	result := make(map[string]string)
	for _, provider := range p {
		result[provider.Name()] = provider.Source()
	}
	return result
}

func (p Providers) Names() []string {
	var result []string
	for _, provider := range p {
		result = append(result, provider.Name())
	}
	return result
}

func (p Providers) Sources() []string {
	var result []string
	for _, provider := range p {
		result = append(result, provider.Source())
	}
	return result
}

func (p Providers) SourceDomains() (result []string, e error) {
	for _, provider := range p {
		uri, err := url.Parse(provider.Source())
		if err != nil {
			log.Error().Err(err).Msg("error parsing source url")
			return result, err
		}
		result = append(result, uri.Host)
	}

	// Add an extra domain for possible GitHub raw usage:
	uri, e := url.Parse("https://raw.githubusercontent.com")
	if e == nil {
		result = append(result, uri.Host)
	} else {
		log.Error().Err(e).Msg("error parsing fallback url")
	}
	return result, nil
}
