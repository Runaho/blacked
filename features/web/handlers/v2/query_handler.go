package v2

import (
	"blacked/features/bloom"
	"blacked/features/web/handlers/response"
	"blacked/internal/collector"
	"blacked/internal/db"
	"blacked/internal/query"
	"net"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// QueryHandler wraps the new HTTP-agnostic QueryService for v2 API endpoints.
type QueryHandler struct {
	svc *query.QueryService
}

// NewQueryHandler constructs a QueryHandler with the shared BloomManager.
// trustConfig is an optional provider→trust_score map (loaded from scoring.toml).
// Pass nil to use default trust scores (0.5 per source).
func NewQueryHandler(mgr *bloom.BloomManager, trustConfig map[string]float64) (*QueryHandler, error) {
	checker := NewBloomAdapter(mgr)

	database, err := db.GetDB()
	if err != nil {
		return nil, err
	}
	repo := db.NewEntryRepository(database)

	scorer := query.NewScorer(trustConfig)

	svc := query.NewQueryService(checker, repo, scorer)
	return &QueryHandler{svc: svc}, nil
}

// NewQueryHandlerWithDeps allows injecting dependencies for testing.
func NewQueryHandlerWithDeps(svc *query.QueryService) *QueryHandler {
	return &QueryHandler{svc: svc}
}

// Check handles GET /api/v1/check?url= or GET /api/v1/check?ip= — fast bloom-only check (~0.4ms).
// ?ip= accepts bare IP or IP:port. Port is preserved in bloom key.
// Schemes like http://, ftp://, file:// are rejected via ?ip= (use ?url= for those → full_url bloom).
func (h *QueryHandler) Check(c echo.Context) error {
	ipStr := c.QueryParam("ip")
	urlStr := c.QueryParam("url")

	// ?ip= parameter — bare IP or IP:port, no scheme allowed
	if ipStr != "" {
		if strings.Contains(ipStr, "://") {
			return c.NoContent(http.StatusNoContent) // reject schemes via ?ip=
		}
		// Validate IP (with optional port)
		trimmed := strings.TrimSpace(ipStr)
		if h, _, err := net.SplitHostPort(trimmed); err != nil {
			// No port — check if pure IP
			if net.ParseIP(trimmed) == nil {
				return c.NoContent(http.StatusNoContent) // not a valid IP
			}
			urlStr = trimmed // "1.2.3.4" → IP bloom key
		} else {
			// Has port — verify host part is valid IP, keep port in bloom key
			if net.ParseIP(h) == nil {
				return c.NoContent(http.StatusNoContent) // not a valid IP
			}
			urlStr = trimmed // "1.2.3.4:8080" → IP bloom key (port preserved)
		}
	} else if urlStr == "" {
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
	// Record business metric
	if mc, err := collector.GetMetricsCollector(); err == nil {
		mc.RecordCheck()
	}
	return c.JSON(http.StatusOK, result)
}

// Hit handles GET /api/v1/hit?url= or GET /api/v1/hit?ip= — full check (bloom + DB + score ~5-15ms).
// ?ip= accepts bare IP or IP:port. Port is preserved in bloom key.
// Schemes like http://, ftp://, file:// are rejected via ?ip= (use ?url= for those → full_url bloom).
func (h *QueryHandler) Hit(c echo.Context) error {
	ipStr := c.QueryParam("ip")
	urlStr := c.QueryParam("url")

	// ?ip= parameter — bare IP or IP:port, no scheme allowed
	if ipStr != "" {
		if strings.Contains(ipStr, "://") {
			return c.NoContent(http.StatusNoContent) // reject schemes via ?ip=
		}
		// Validate IP (with optional port)
		trimmed := strings.TrimSpace(ipStr)
		if h, _, err := net.SplitHostPort(trimmed); err != nil {
			// No port — check if pure IP
			if net.ParseIP(trimmed) == nil {
				return c.NoContent(http.StatusNoContent) // not a valid IP
			}
			urlStr = trimmed // "1.2.3.4" → IP bloom key
		} else {
			// Has port — verify host part is valid IP, keep port in bloom key
			if net.ParseIP(h) == nil {
				return c.NoContent(http.StatusNoContent) // not a valid IP
			}
			urlStr = trimmed // "1.2.3.4:8080" → IP bloom key (port preserved)
		}
	} else if urlStr == "" {
		return c.NoContent(http.StatusNoContent)
	}

	result, err := h.svc.Hit(c.Request().Context(), urlStr)
	if err != nil {
		log.Error().Err(err).Str("url", urlStr).Msg("v2 hit failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Hit check failed", err.Error())
	}

	if !result.Blocked {
		// Record business metric — allowed (not blocked)
		if mc, err := collector.GetMetricsCollector(); err == nil {
			mc.RecordHit(false)
		}
		return c.NoContent(http.StatusNoContent)
	}
	// Record business metric — blocked
	if mc, err := collector.GetMetricsCollector(); err == nil {
		mc.RecordHit(true)
	}
	return c.JSON(http.StatusOK, result)
}

// bulkInput is the request body for bulk endpoints.
type bulkInput struct {
	URLs []string `json:"urls" validate:"required,min=1"`
}

// BulkCheck handles POST /api/v1/bulk-check — bloom-only batch check (~0.4ms per URL).
func (h *QueryHandler) BulkCheck(c echo.Context) error {
	var input bulkInput
	if err := c.Bind(&input); err != nil {
		return response.BadRequest(c, "Invalid request body: "+err.Error())
	}
	if err := c.Validate(&input); err != nil {
		return response.BadRequest(c, "Validation error: "+err.Error())
	}

	results, err := h.svc.BulkCheck(c.Request().Context(), input.URLs)
	if err != nil {
		log.Error().Err(err).Msg("v2 bulk-check failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Bulk check failed", err.Error())
	}
	// Record business metric
	if mc, err := collector.GetMetricsCollector(); err == nil {
		mc.RecordBulkCheck()
	}
	return c.JSON(http.StatusOK, results)
}

// BulkHit handles POST /api/v1/bulk-hit — full batch check (bloom + DB + score).
func (h *QueryHandler) BulkHit(c echo.Context) error {
	var input bulkInput
	if err := c.Bind(&input); err != nil {
		return response.BadRequest(c, "Invalid request body: "+err.Error())
	}
	if err := c.Validate(&input); err != nil {
		return response.BadRequest(c, "Validation error: "+err.Error())
	}

	results, err := h.svc.BulkHit(c.Request().Context(), input.URLs)
	if err != nil {
		log.Error().Err(err).Msg("v2 bulk-hit failed")
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Bulk hit failed", err.Error())
	}
	// Record business metric — check if any URL was blocked
	anyBlocked := false
	for _, r := range results {
		if r.Blocked {
			anyBlocked = true
			break
		}
	}
	if mc, err := collector.GetMetricsCollector(); err == nil {
		mc.RecordBulkHit(anyBlocked)
	}
	return c.JSON(http.StatusOK, results)
}
