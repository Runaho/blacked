package health

import (
	"blacked/internal/config"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// MapHealth sets up a simple healthcheck endpoint if enabled in config.
func MapHealth(e *echo.Echo, cfg config.ServerConfig) {
	if !cfg.HealthCheck {
		log.Info().Msg("Health check disabled")
		return
	}
	g := e.Group("/health")
	g.GET("/status", StatusCheck)
	log.Info().Msg("Health check enabled at /health/status")
}

// StatusCheck returns a simple JSON indicating “ok” status.
func StatusCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status": "ok",
	})
}
