package query

import (
	"blacked/features/entries/services"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

func MapQueryRoutes(e *echo.Echo, svc *services.QueryService) error {
	handler := NewQueryHandler(svc)

	g := e.Group("/query")
	g.POST("/entry", handler.Query)

	log.Info().Msg("Query routes mapped successfully. at /query/entry")

	return nil
}
