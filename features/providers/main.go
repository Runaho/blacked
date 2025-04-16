package providers

import (
	"blacked/features/providers/base"
	"blacked/features/providers/oisd"
	"blacked/features/providers/openphish"
	"blacked/features/providers/phishtank"
	"blacked/features/providers/urlhaus"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"

	"blacked/internal/collector"
	collyClient "blacked/internal/colly"
	"blacked/internal/config"

	"net/url"
)

func getProviders(cfg *config.Config, cc *colly.Collector) Providers {
	oisd.NewOISDBigProvider(&cfg.Collector, cc)
	oisd.NewOISDNSFWProvider(&cfg.Collector, cc)
	urlhaus.NewURLHausProvider(&cfg.Collector, cc)
	openphish.NewOpenPhishFeedProvider(&cfg.Collector, cc)
	phishtank.NewPhishTankProvider(&cfg.Collector, cc)

	providers := Providers(base.GetRegisteredProviders())
	return providers
}

type Providers []base.Provider

func NewProviders() (Providers, error) {
	cfg := config.GetConfig()

	cc, err := collyClient.InitCollyClient()
	if err != nil {
		log.Error().Err(err).Msg("error initializing colly client")
		return nil, err
	}

	providers := getProviders(cfg, cc)

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
