package web

import (
	"blacked/features/web/handlers/benchmark"
	"blacked/features/web/handlers/health"
	"blacked/features/web/handlers/provider"
	"blacked/features/web/handlers/query"
	v2 "blacked/features/web/handlers/v2"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
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

	// V2 API routes (check, hit, bulk)
	v2Handler, err := v2.NewQueryHandler()
	if err != nil {
		log.Warn().Err(err).Msg("V2 query handler init failed — skipping v2 routes")
	} else {
		if err := v2.MapV2Routes(e, v2Handler); err != nil {
			return err
		}
	}

	return nil
}

func (app *Application) MapHome() {
	e := app.Echo

	e.GET("/", func(c echo.Context) error {
		return c.String(200, "Welcome to BLACKED Service")
	})
}
