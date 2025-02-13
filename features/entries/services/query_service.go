package services

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/features/entries/repository"
	"blacked/internal/db"
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// QueryService handles queries against the blacklist entries.
type QueryService struct {
	repo repository.BlacklistRepository
}

// NewQueryService creates a new QueryService instance.  It should handle potential errors during database connection initialization more robustly in a production environment.
func NewQueryService() (*QueryService, error) {
	dbConn, err := db.GetDB()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the database: %w", err)
	}
	return &QueryService{repo: repository.NewDuckDBRepository(dbConn)}, nil
}

// Query performs a query based on the provided URL and query type.  It handles various query types and returns the results.
func (s *QueryService) Query(ctx context.Context, url string, queryType *enums.QueryType) ([]entries.Hit, error) {
	log.Info().Msgf("Querying blacklist entries by URL: %s (type: %v)", url, queryType)
	startTime := time.Now()
	hits, err := s.repo.QueryLinkByType(ctx, url, queryType)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query blacklist entries")
		return nil, fmt.Errorf("failed to query blacklist entries: %w", err)
	}
	log.Debug().Dur("duration", time.Since(startTime)).Msgf("Query completed, %d hits found", len(hits))
	return hits, nil
}
