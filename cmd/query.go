package cmd

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/features/entries/services"
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

// QueryCommand queries your blacklist entries by URL.
var QueryCommand = &cli.Command{
	Name:  "query",
	Usage: "Query blacklist entries by URL",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "url",
			Aliases:  []string{"u"},
			Usage:    "URL to query (supports full URL, host, domain, path).",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "type",
			Aliases: []string{"t"},
			Usage:   "Type of URL query: [full, host, domain, path, mixed].",
			Value:   "mixed",
		},
		&cli.BoolFlag{
			Name:    "json",
			Aliases: []string{"j"},
			Usage:   "Output results in JSON format.",
			Value:   false,
		},
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Enable verbose logging. By default shows minimal result; with verbose you can see all hits.",
			Value:   false,
		},
	},
	Action: queryBlacklist,
}

// queryBlacklist is the action backing the “query” command.
func queryBlacklist(c *cli.Context) error {
	queryService, err := services.NewQueryService()
	if err != nil {
		return fmt.Errorf("failed to create query service: %w", err)
	}

	urlToQuery, queryType, err := getQueryParameters(c)
	if err != nil {
		return err
	}

	hits, err := queryService.Query(context.Background(), urlToQuery, queryType)
	if err != nil {
		return fmt.Errorf("failed to query blacklist entries: %w", err)
	}

	queryResponse := entries.NewQueryResponse(urlToQuery, hits, *queryType, c.Bool("verbose"))

	return printQueryResponse(queryResponse, c.Bool("json"))
}

// getQueryParameters extracts the required flags from the CLI context.
func getQueryParameters(c *cli.Context) (string, *enums.QueryType, error) {
	urlToQuery := c.String("url")
	queryTypeStr := c.String("type")

	if urlToQuery == "" {
		return "", nil, fmt.Errorf("URL is required")
	}

	qt, err := enums.QueryTypeString(queryTypeStr)
	if err != nil {
		return "", nil, fmt.Errorf("invalid query type: %w", err)
	}

	return urlToQuery, &qt, nil
}

func printQueryResponse(response *entries.QueryResponse, asJSON bool) error {
	if asJSON {
		jsonData, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(jsonData))
		return nil
	}

	log.Info().
		Str("URL", response.URL).
		Int("Total Hits", response.Count).
		Str("Query Type", response.QueryType.String()).
		Int("Shown Hits", len(response.Hits)).
		Msg("Query response")

	return nil
}
