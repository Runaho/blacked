package cmd

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/features/entries/repository"
	"blacked/internal/db"
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

// QueryCommand queries blacklist entries based on URL criteria.
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
			Usage:   "Type of URL query: [full, host, domain, path, mixed]",
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
			Usage:   "Enable verbose logging. Shows all of the found entries. default only show first with count.",
			Value:   false,
		},
	},
	Action: queryBlacklist,
}

func queryBlacklist(c *cli.Context) error {
	dbConn, err := db.GetDB()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer dbConn.Close()

	repo := repository.NewDuckDBRepository(dbConn)
	urlToQuery := c.String("url")
	queryTypeString := c.String("type")

	var queryType *enums.QueryType
	if queryTypeString != "" {
		qt, err := enums.QueryTypeString(queryTypeString)
		if err != nil {
			return fmt.Errorf("invalid query type: %w", err)
		}
		queryType = &qt
	}

	log.Info().Msgf("Querying blacklist entries by URL: %s (type: %v)", urlToQuery, queryType)

	hits, err := repo.QueryLinkByType(context.Background(), urlToQuery, queryType)

	if err != nil {
		return fmt.Errorf("failed to query blacklist entries: %w", err)
	}

	verbose := c.Bool("verbose")

	queryResponse := entries.NewQueryResponse(urlToQuery, hits, *queryType, verbose)

	if c.Bool("json") {
		jsonData, err := json.MarshalIndent(queryResponse, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		log.Print(string(jsonData))
		return nil
	} else {
		log.Info().
			Bool("Verbose", verbose).
			Str("URL", queryResponse.URL).
			Int("Total Hits", queryResponse.Count).
			Str("Query Type", queryResponse.QueryType.String()).
			Interface("Hits", queryResponse.Hits).
			Msg("Query response")
	}
	return nil
}
