package web

import (
	"blacked/features/web/handlers/query"
	"blacked/features/web/router/health"
	"blacked/features/web/router/problem"

	"github.com/labstack/echo/v4"
)

func (app *Application) ConfigureRoutes() error {
	e := app.Echo

	problem.MapRoutes(e)
	health.MapHealth(e, *app.config)

	return nil
}

func (app *Application) MapHome() {
	e := app.Echo

	e.GET("/", func(c echo.Context) error {
		return c.String(200, "Welcome to BLACKED Service")
	})
}

func (app *Application) MapQueryRoutes() error {
	e := app.Echo

	// Pass in the service from app.services
	err := query.MapQueryRoutes(e, app.services.EntryQueryService)
	return err
}
