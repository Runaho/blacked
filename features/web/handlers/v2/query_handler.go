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

// NewQueryHandler constructs a QueryHandler with the shared BloomManager.
func NewQueryHandler(mgr *bloom.BloomManager) (*QueryHandler, error) {
	checker := NewBloomAdapter(mgr)

	database, err := db.GetDB()
	if err != nil {
		return nil, err
	}
	repo := db.NewEntryRepository(database)

	scorer := query.NewScorer(nil)

	svc := query.NewQueryService(checker, repo, scorer)
	return &QueryHandler{svc: svc}, nil
}

// NewQueryHandlerWithDeps allows injecting dependencies for testing.
func NewQueryHandlerWithDeps(svc *query.QueryService) *QueryHandler {
	return &QueryHandler{svc: svc}
}

// Check handles GET /api/v1/check?url= — fast bloom-only check (~0.4ms).
// Returns 204 No Content if no match, 200 with body if matched.
func (h *QueryHandler) Check(c echo.Context) error {
	urlStr := c.QueryParam("url")
	if urlStr == "" {
		return c.NoContent(http.StatusNoContent)
	}

	result, err := h.svc.Likely(c.Request().Context(), urlStr)
	if err != nil {
		log.Error().Err(err).Str("url", urlStr).Msg("v2 check failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Bloom check failed", err.Error())
	}

	if !result.Likely {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, result)
}

// Hit handles GET /api/v1/hit?url= — full check (bloom + DB + score ~5-15ms).
// Returns 204 No Content if no match, 200 with body if matched.
func (h *QueryHandler) Hit(c echo.Context) error {
	urlStr := c.QueryParam("url")
	if urlStr == "" {
		return c.NoContent(http.StatusNoContent)
	}

	result, err := h.svc.Hit(c.Request().Context(), urlStr)
	if err != nil {
		log.Error().Err(err).Str("url", urlStr).Msg("v2 hit failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Hit check failed", err.Error())
	}

	if !result.Blocked {
		return c.NoContent(http.StatusNoContent)
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
