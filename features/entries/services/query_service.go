package services

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/features/entries/repository"
	"blacked/internal/db"
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
)

// Query service error variables
var (
	ErrDatabaseConnection = errors.New("failed to connect to the database")
	ErrQueryBlacklist     = errors.New("failed to query blacklist entries")
)

// QueryService handles queries against the blacklist entries.
type QueryService struct {
	repo repository.BlacklistRepository
}

// NewQueryService creates a new QueryService instance.  It should handle potential errors during database connection initialization more robustly in a production environment.
func NewQueryService() (*QueryService, error) {
	dbConn, err := db.GetDB()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get database connection")
		return nil, ErrDatabaseConnection
	}

	return &QueryService{repo: repository.NewSQLiteRepository(dbConn)}, nil
}

// Query performs a query based on the provided URL and query type.  It handles various query types and returns the results.
func (s *QueryService) Query(ctx context.Context, url string, queryType *enums.QueryType) ([]entries.Hit, error) {
	log.Info().Msgf("Querying blacklist entries by URL: %s (type: %v)", url, queryType)
	startTime := time.Now()
	hits, err := s.repo.QueryLinkByType(ctx, url, queryType)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query blacklist entries")
		return nil, ErrQueryBlacklist
	}
	log.Debug().Dur("duration", time.Since(startTime)).Msgf("Query completed, %d hits found", len(hits))
	return hits, nil
}

func (s *QueryService) GetEntryByID(ctx context.Context, id string) (*entries.Entry, error) {
	// Call your repository to get the entry by ID
	entry, err := s.repo.GetEntryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return entry, nil
}
