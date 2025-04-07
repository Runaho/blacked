package colly

import (
	"blacked/internal/config"
	"errors"
	"sync"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

var (
	client *colly.Collector
	once   sync.Once
	err    error

	ErrCollyNotInitialized = errors.New("colly client is not initialized")
)

// InitCollyClient returns a singleton instance of colly.Collector, initialized only once.
func InitCollyClient() (*colly.Collector, error) {
	once.Do(func() {
		cfg := config.GetConfig()
		collyConfig := cfg.Colly
		client = colly.NewCollector(
			colly.MaxDepth(collyConfig.MaxRedirects),
			colly.MaxBodySize(collyConfig.MaxSize),
			colly.IgnoreRobotsTxt(),
			colly.AllowURLRevisit(),
			colly.Async(true),
			colly.UserAgent(collyConfig.UserAgent),
			colly.TraceHTTP(),
		)
		client.SetRequestTimeout(collyConfig.TimeOut)

		if client == nil {
			err = errors.New("failed to create colly collector")
			log.Error().Msg("Failed to initialize colly client.")
			return
		}
		log.Debug().Msg("Colly client initialized.")
	})
	return client, err // Return client and potential error from initialization
}

func GetCollyClient() (*colly.Collector, error) {
	if client == nil {
		return nil, ErrCollyNotInitialized
	}
	return client, nil
}
