package benchmark

import (
	"blacked/features/entries/services"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

func MapBenchmarkRoutes(e *echo.Echo, svc *services.QueryService) error {
	handler := NewBenchmarkHandler(svc)

	g := e.Group("/benchmark")
	g.POST("/query", handler.BenchmarkURL)
	g.POST("/compare", handler.CompareAllMethods)

	log.Info().
		Str("benchmark a URL's", "/benchmark/query").
		Str("compare all methods", "/benchmark/compare").
		Msg("Benchmark routes mapped successfully.")

	return nil
}
