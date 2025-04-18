package web

import (
	"blacked/features/web/handlers/benchmark"
	"blacked/features/web/handlers/health"
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

	if err := provider.MapProviderRoutes(e, app.services.ProviderProcessService); err != nil {
		return err
	}

	health.MapHealth(e, *app.config)

	benchmark.MapBenchmarkRoutes(e, app.services.EntryQueryService)
	return nil
}

func (app *Application) MapHome() {
	e := app.Echo

	e.GET("/", func(c echo.Context) error {
		return c.String(200, "Welcome to BLACKED Service")
	})
}
