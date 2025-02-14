package provider

import (
	"blacked/features/providers/services"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

func MapProviderRoutes(e *echo.Echo, svc *services.ProviderProcessService) error {
	handler := NewProviderHandler(svc)

	g := e.Group("/provider")
	g.POST("/process", handler.ProcessProviders)
	g.GET("/process/status/:processID", handler.GetProcessStatus)
	g.GET("/processes", handler.ListProcesses) // Add list processes endpoint

	log.Info().
		Str("new processing", "/provider/process").
		Str("get process status", "/provider/process/status/:processID").
		Str("list processes", "/provider/processes").
		Msg("Provider routes mapped successfully.")

	return nil
}
