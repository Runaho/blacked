package query

import (
	"blacked/features/entries/services"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

func MapQueryRoutes(e *echo.Echo, svc *services.QueryService) error {
	handler := NewSearchHandler(svc)

	g := e.Group("/entry")
	g.POST("/search", handler.Search)
	g.GET("/:id", handler.QueryByID)
	g.GET("", handler.QueryByURL)

	log.Info().
		Str("search an entry (POST)", "/search").
		Str("get by id", "/entry/:id").
		Str("get by url", "get /entry `param,query,form,json,xml key is 'url'").
		Msg("Entry routes mapped successfully.")

	return nil
}
