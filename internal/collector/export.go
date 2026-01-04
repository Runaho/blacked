// collector/export.go
package collector

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// in the Prometheus text exposition format for standard http.Server.
func (mc *MetricsCollector) ExposeMetricsHTTPHandler() http.Handler {
	return promhttp.Handler()
}

func (mc *MetricsCollector) ExposeWebMetrics(e *echo.Echo) {
	// Expose Prometheus metrics at /metrics (standard endpoint)
	e.GET("/metrics", echo.WrapHandler(mc.ExposeMetricsHTTPHandler()))

	log.Info().
		Str("path", "/metrics").
		Msg("Metrics exposed successfully.")
}
