package cmd

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/features/entries/services"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

// CLI error variables
var (
	ErrCreateQueryService = errors.New("failed to create query service")
	ErrQueryBlacklist     = errors.New("failed to query blacklist entries")
	ErrMissingURL         = errors.New("URL is required")
	ErrInvalidQueryType   = errors.New("invalid query type")
	ErrMarshalJSON        = errors.New("failed to marshal JSON")
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
		log.Error().Err(err).Msg("Failed to create query service")
		return ErrCreateQueryService
	}

	urlToQuery, queryType, err := getQueryParameters(c)
	if err != nil {
		return err
	}

	hits, err := queryService.Query(context.Background(), urlToQuery, queryType)
	if err != nil {
		log.Err(err).Str("url", urlToQuery).Str("query_type", queryType.String()).Msg("Failed to query blacklist entries")
		return ErrQueryBlacklist
	}

	queryResponse := entries.NewQueryResponse(urlToQuery, hits, *queryType, c.Bool("verbose"))

	return printQueryResponse(queryResponse, c.Bool("json"))
}

// getQueryParameters extracts the required flags from the CLI context.
func getQueryParameters(c *cli.Context) (string, *enums.QueryType, error) {
	urlToQuery := c.String("url")
	queryTypeStr := c.String("type")

	if urlToQuery == "" {
		log.Error().Msg("URL parameter is required")
		return "", nil, ErrMissingURL
	}

	qt, err := enums.QueryTypeString(queryTypeStr)
	if err != nil {
		log.Error().Err(err).Str("query_type", queryTypeStr).Msg("Invalid query type")
		return "", nil, ErrInvalidQueryType
	}

	return urlToQuery, &qt, nil
}

func printQueryResponse(response *entries.QueryResponse, asJSON bool) error {
	if asJSON {
		jsonData, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal JSON")
			return ErrMarshalJSON
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
