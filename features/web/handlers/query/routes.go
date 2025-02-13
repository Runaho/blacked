package query

import (
	"blacked/features/entries/services"

	"github.com/labstack/echo/v4"
)

// MapQueryRoutes registers the routes for querying.
func MapQueryRoutes(e *echo.Echo, svc *services.QueryService) error {
	handler := NewQueryHandler(svc)

	g := e.Group("/query")
	g.POST("", handler.Query)

	return nil
}
