package colly

import (
	"blacked/internal/config"
	"fmt"
	"sync"

	"github.com/gocolly/colly/v2"
)

var (
	client *colly.Collector
	once   sync.Once
	err    error // Package level error variable
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
			err = fmt.Errorf("failed to create colly collector") // Set error if collector creation fails
			return
		}
		fmt.Println("Colly client initialized.") // Optional: Log initialization
	})
	return client, err // Return client and potential error from initialization
}

func GetCollyClient() (*colly.Collector, error) {
	if client == nil {
		return nil, fmt.Errorf("colly client is not initialized")
	}
	return client, nil
}
