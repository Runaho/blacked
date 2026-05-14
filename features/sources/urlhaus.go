package sources

import (
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
)

// NewURLHausSource creates the URLHaus abuse.ch text source.
func NewURLHausSource(settings *config.CollectorConfig, collyClient *colly.Collector) *Source {
	const (
		providerID = "abuse-ch"
		sourceID   = "urlhaus-online"
		sourceName = "URLHaus Online"
		sourceURL  = "https://urlhaus.abuse.ch/downloads/text/"
		category   = "malware"
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
	s.Parser = NewFlatListParser(category, settings.ParserWorkers, settings.ParserBatchSize)

	return s
}
