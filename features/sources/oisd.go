package sources

import (
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
)

// NewOISDBigSource creates the OISD Big blocklist source.
func NewOISDBigSource(settings *config.CollectorConfig, collyClient *colly.Collector) *Source {
	const (
		providerID   = "oisd"
		sourceID     = "oisd-big"
		sourceName   = "OISD Big"
		sourceURL    = "https://big.oisd.nl/domainswild"
		cronSchedule = "15 4 * * *"
		category     = "blocklist"
	)

	s := &Source{
		ID:           sourceID,
		ProviderID:   providerID,
		Name:         sourceName,
		SourceURL:    sourceURL,
		SourceType:   SourceTypeFlat,
		Category:     category,
		Enabled:      true,
		BloomTypes:   []BloomType{BloomHost, BloomDomain},
	}

	s.Fetcher = NewCollyFetcher(collyClient)
	s.Parser = NewFlatListParser(settings.ParserWorkers, settings.ParserBatchSize)

	return s
}

// NewOISDNSFWSource creates the OISD NSFW blocklist source.
func NewOISDNSFWSource(settings *config.CollectorConfig, collyClient *colly.Collector) *Source {
	const (
		providerID   = "oisd"
		sourceID     = "oisd-nsfw"
		sourceName   = "OISD NSFW"
		sourceURL    = "https://nsfw.oisd.nl/domainswild"
		cronSchedule = "22 6 * * *"
		category     = "nsfw"
	)

	s := &Source{
		ID:           sourceID,
		ProviderID:   providerID,
		Name:         sourceName,
		SourceURL:    sourceURL,
		SourceType:   SourceTypeFlat,
		Category:     category,
		Enabled:      true,
		BloomTypes:   []BloomType{BloomHost, BloomDomain},
	}

	s.Fetcher = NewCollyFetcher(collyClient)
	s.Parser = NewFlatListParser(settings.ParserWorkers, settings.ParserBatchSize)

	return s
}
