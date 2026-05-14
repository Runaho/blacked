package v2

import (
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// MapV2Routes registers the v2 API routes on the given Echo group.
// Group at /api/v1 is expected — routes are:
//   GET  /api/v1/check?url=   → QueryHandler.Check (bloom only)
//   GET  /api/v1/hit?url=     → QueryHandler.Hit   (bloom + DB + score)
//   POST /api/v1/bulk          → QueryHandler.Bulk  (batch check)
func MapV2Routes(e *echo.Echo, handler *QueryHandler) error {
	g := e.Group("/api/v1")

	g.GET("/check", handler.Check)
	g.GET("/hit", handler.Hit)
	g.POST("/bulk", handler.Bulk)

	log.Info().
		Str("check", "GET /api/v1/check?url=").
		Str("hit", "GET /api/v1/hit?url=").
		Str("bulk", "POST /api/v1/bulk").
		Msg("V2 API routes mapped successfully.")

	return nil
}
