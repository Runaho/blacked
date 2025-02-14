package web

import (
	"blacked/features/web/handlers/health"
	"blacked/features/web/handlers/problem"
	"blacked/features/web/handlers/provider"
	"blacked/features/web/handlers/query"

	"github.com/labstack/echo/v4"
)

func (app *Application) ConfigureRoutes() error {
	e := app.Echo

	app.MapHome()
	if err := query.MapQueryRoutes(e, app.services.EntryQueryService); err != nil {
		return err
	}

	if err := provider.MapProviderRoutes(e); err != nil {
		return err
	}

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
