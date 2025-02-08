package cmd

import (
	"blacked/features/entries/enums"
	"blacked/features/entries/repository"
	"blacked/internal/db"
	"context"
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

	log.Info().Any("Hits", hits).Msg("Query results")

	log.Info().
		Str("Query Type", queryTypeString).
		Str("URL", urlToQuery).
		Int("Hits Count", len(hits)).
		Msg("Blacklist query completed successfully.")

	return nil
}
