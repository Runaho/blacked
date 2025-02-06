// collector/export.go
package collector

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// in the Prometheus text exposition format for standard http.Server.
func (mc *MetricsCollector) ExposeMetricsHTTPHandler() http.Handler {
	return promhttp.Handler()
}

func (mc *MetricsCollector) ExposeMetricsHTTPHandlerFunc(c echo.Context) error {
	return c.String(http.StatusOK, "Metrics Exposed")
}

func (mc *MetricsCollector) ExposeWebMetrics(e *echo.Echo) {
	e.GET("/metrics", mc.ExposeMetricsHTTPHandlerFunc)
	e.GET("/metrics/prometheus", echo.WrapHandler(mc.ExposeMetricsHTTPHandler()))
}
