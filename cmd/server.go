package cmd

import (
	"blacked/features/web"
	"blacked/internal/config"

	"github.com/ory/graceful"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

// WebServer is the CLI command that starts the web API server.
var WebServer = &cli.Command{
	Name:    "serve",
	Aliases: []string{"s"},
	Usage:   "Start web API server",
	Action:  serve,
}

func serve(c *cli.Context) (err error) {
	if err := config.InitConfig(); err != nil {
		log.Error().Err(err).Msg("Failed to load config")
		return err
	}
	cfg := config.GetConfig()

	app, err := web.NewApplication(&cfg.Server)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create web application")
		return err
	}

	server := graceful.WithDefaults(app.Echo.Server)
	log.Info().Msgf("Starting server on %s", server.Addr)

	if err = graceful.Graceful(server.ListenAndServe, server.Shutdown); err != nil {
		log.Error().Err(err).Msg("Failed to start server")
		return err
	}

	log.Info().Msg("Server stopped gracefully.")
	return nil
}
