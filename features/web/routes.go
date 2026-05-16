package web

import (
	"blacked/features/entry_collector"
	"blacked/features/web/handlers/health"
	"blacked/features/web/handlers/provider"
	v2 "blacked/features/web/handlers/v2"
	"blacked/internal/config"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

func (app *Application) ConfigureRoutes() error {
	e := app.Echo

	app.MapHome()

	if err := provider.MapProviderRoutes(e, app.services.ProviderProcessService); err != nil {
		return err
	}

	health.MapHealth(e, *app.config)

	// V2 API routes — inject the singleton BloomManager from PondCollector
	collector := entry_collector.GetPondCollector()
	if collector == nil {
		log.Warn().Msg("PondCollector not ready — v2 routes skipped")
	} else {
		bloomMgr := collector.GetBloomManager()
		trustConfig := config.LoadScoringConfig()
		v2Handler, err := v2.NewQueryHandler(bloomMgr, trustConfig)
		if err != nil {
			log.Warn().Err(err).Msg("V2 query handler init failed — skipping v2 routes")
		} else {
			if err := v2.MapV2Routes(e, v2Handler); err != nil {
				return err
			}
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
