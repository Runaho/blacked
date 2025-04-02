package providers

import (
	"blacked/features/entries/repository"
	"blacked/features/providers/base"
	"blacked/features/providers/oisd"
	"blacked/features/providers/openphish"
	"blacked/features/providers/phishtank"
	"blacked/features/providers/urlhaus"
	"blacked/internal/collector"
	"blacked/internal/colly"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/utils"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type Providers []base.Provider

func NewProviders() (Providers, error) {
	cfg := config.GetConfig()

	cc, err := colly.InitCollyClient()
	if err != nil {
		log.Error().Err(err).Msg("error initializing colly client")
		return nil, err
	}

	var providers = Providers{
		oisd.NewOISDBigProvider(&cfg.Collector, cc),
		oisd.NewOISDNSFWProvider(&cfg.Collector, cc),
		urlhaus.NewURLHausProvider(&cfg.Collector, cc),
		openphish.NewOpenPhishFeedProvider(&cfg.Collector, cc),
		phishtank.NewPhishTankProvider(&cfg.Collector, cc),
	}

	// Example: Collect their source URLs for logging or metrics
	srcs := providers.Sources()
	log.Trace().Msgf("initialized provider sources: %v", srcs)

	// Also gather their domains for the Colly AllowedDomains
	sourceDomains, err := providers.SourceDomains()
	if err != nil {
		log.Error().Err(err).Msg("error getting source domains")
		return providers, err
	}
	cc.AllowedDomains = sourceDomains
	log.Trace().Msgf("initialized provider source domains: %v", sourceDomains)

	// Initialize Prometheus metrics with the source names.
	collector.NewMetricsCollector(srcs)

	return providers, nil
}

// Process is where we actually perform any DB‐writing logic. We open a short‐lived
// read‐write connection, create a repository that uses it, then iteratively fetch
// and parse data from each provider. Now processes providers concurrently.
func (p Providers) Process() error {
	rwDB, err := db.GetDB()
	if err != nil {
		return fmt.Errorf("failed to open read-write database: %w", err)
	}

	repo := repository.NewSQLiteRepository(rwDB)

	var wg sync.WaitGroup
	errChan := make(chan error, len(p)) // Buffered channel to collect errors

	for _, provider := range p {
		wg.Add(1)
		go p.processProvider(provider, repo, &wg, errChan) // Launch goroutine for each provider
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
func (p Providers) processProvider(provider base.Provider, repo repository.BlacklistRepository, wg *sync.WaitGroup, errChan chan error) {
	defer wg.Done()

	name := provider.GetName()
	source := provider.Source()
	processID := uuid.New()
	startedAt := time.Now()
	strProcessID := processID.String()

	// Create a logger with context for this provider
	providerLogger := log.With().
		Str("process_id", strProcessID).
		Str("source", source).
		Str("provider", name).
		Logger()

	providerLogger.Info().Time("starts", startedAt).Msg("start processing data")

	// Set the process ID on the provider
	provider.SetProcessID(processID)

	// Fetch the data
	reader, meta, err := utils.GetResponseReader(source, provider.Fetch, name, strProcessID)
	if err != nil {
		providerLogger.Error().Err(err).Msg("error fetching data")
		errChan <- fmt.Errorf("%s provider: error fetching data: %w", name, err)
		return
	}

	// Handle metadata if present
	if meta != nil {
		strProcessID = meta.ProcessID
		providerLogger.Info().
			Str("new_process_id", strProcessID).
			TimeDiff("duration", time.Now(), startedAt).
			Msg("found metadata, changing process ID")
		provider.SetProcessID(uuid.MustParse(strProcessID))
	}

	// Set the repository and parse data
	provider.SetRepository(repo)
	if err := provider.Parse(reader); err != nil {
		providerLogger.Error().Err(err).Msg("error parsing data")
		errChan <- fmt.Errorf("%s provider: error parsing data: %w", name, err)
		return
	}

	// Cleanup if needed
	cfg := config.GetConfig()
	if cfg.APP.Environtment != "development" {
		utils.RemoveStoredResponse(name)
	}

	providerLogger.Info().
		TimeDiff("duration", time.Now(), startedAt).
		Msg("finished processing data")
}

func (p Providers) NamesAndSources() map[string]string {
	result := make(map[string]string)
	for _, provider := range p {
		result[provider.GetName()] = provider.Source()
	}
	return result
}

func (p Providers) GetNames() []string {
	var result []string
	for _, provider := range p {
		result = append(result, provider.GetName())
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
