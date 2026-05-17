package sources

import (
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
)

// NewOpenPhishSource creates the OpenPhish feed source.
func NewOpenPhishSource(settings *config.CollectorConfig, collyClient *colly.Collector) *Source {
	const (
		providerID = "openphish"
		sourceID   = "openphish-feed"
		sourceName = "OpenPhish Feed"
		sourceURL  = "https://openphish.com/feed.txt"
		category   = "phishing"
	)

	s := &Source{
		ID:         sourceID,
		ProviderID: providerID,
		Name:       sourceName,
		SourceURL:  sourceURL,
		SourceType: SourceTypeFlat,
		Category:   category,
		Enabled:    true,
		BloomTypes: []BloomType{BloomHost, BloomHostPath, BloomPath, BloomFile},
	}

	s.Fetcher = NewCollyFetcher(collyClient)
	s.Parser = NewFlatListParser(settings.ParserWorkers, settings.ParserBatchSize, category)

	return s
}
