package v2

import (
	"blacked/features/bloom"
	"blacked/features/web/handlers/response"
	"blacked/internal/db"
	"blacked/internal/query"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// QueryHandler wraps the new HTTP-agnostic QueryService for v2 API endpoints.
type QueryHandler struct {
	svc *query.QueryService
}

// NewQueryHandler constructs a QueryHandler with live dependencies.
func NewQueryHandler() (*QueryHandler, error) {
	// Build BloomChecker adapter — default 1000 items per set
	mgr := bloom.NewBloomManager(1000)
	checker := NewBloomAdapter(mgr)

	// Build EntryRepository
	database, err := db.GetDB()
	if err != nil {
		return nil, err
	}
	repo := db.NewEntryRepository(database)

	// Scorer stub (real scoring in Phase 5)
	scorer := query.NewScorer(nil)

	svc := query.NewQueryService(checker, repo, scorer)
	return &QueryHandler{svc: svc}, nil
}

// NewQueryHandlerWithDeps allows injecting dependencies for testing.
func NewQueryHandlerWithDeps(svc *query.QueryService) *QueryHandler {
	return &QueryHandler{svc: svc}
}

// Check handles GET /api/v1/check?url= — fast bloom-only check (~0.4ms).
func (h *QueryHandler) Check(c echo.Context) error {
	urlStr := c.QueryParam("url")
	if urlStr == "" {
		return response.BadRequest(c, "url parameter is required")
	}

	result, err := h.svc.Likely(c.Request().Context(), urlStr)
	if err != nil {
		log.Error().Err(err).Str("url", urlStr).Msg("v2 check failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Bloom check failed", err.Error())
	}

	return c.JSON(http.StatusOK, result)
}

// Hit handles GET /api/v1/hit?url= — full check (bloom + DB + score ~5-15ms).
func (h *QueryHandler) Hit(c echo.Context) error {
	urlStr := c.QueryParam("url")
	if urlStr == "" {
		return response.BadRequest(c, "url parameter is required")
	}

	result, err := h.svc.Hit(c.Request().Context(), urlStr)
	if err != nil {
		log.Error().Err(err).Str("url", urlStr).Msg("v2 hit failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Hit check failed", err.Error())
	}

	return c.JSON(http.StatusOK, result)
}

// Bulk handles POST /api/v1/bulk — bulk URL lookup.
type bulkInput struct {
	URLs []string `json:"urls" validate:"required,min=1"`
}

func (h *QueryHandler) Bulk(c echo.Context) error {
	var input bulkInput
	if err := c.Bind(&input); err != nil {
		return response.BadRequest(c, "Invalid request body: "+err.Error())
	}
	if err := c.Validate(&input); err != nil {
		return response.BadRequest(c, "Validation error: "+err.Error())
	}

	results, err := h.svc.Bulk(c.Request().Context(), input.URLs)
	if err != nil {
		log.Error().Err(err).Msg("v2 bulk failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Bulk check failed", err.Error())
	}

	return c.JSON(http.StatusOK, results)
}
