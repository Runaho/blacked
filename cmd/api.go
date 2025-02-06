package cmd

import (
	"blacked/internal/config"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

var WebServer *cli.Command = &cli.Command{
	Name:    "serve",
	Aliases: []string{"s"},
	Usage:   "Start web api server",
	Action:  serve,
}

func serve(c *cli.Context) (err error) {
	if err = config.InitConfig(); err != nil {
		log.Error().Err(err).Msg("Failed to initialize config")
		return err
	}

	//config := config.GetConfig()

	//var app *web.Application

	//if app, err = web.NewApplication(&config.Server); err != nil {
	//	log.Error().Err(err).Msg("Failed to create web application")
	//	return err
	//}

	//server := graceful.WithDefaults(app.Echo.Server)
	//log.Info().Msgf("Starting server on %s", server.Addr)

	//if err = graceful.Graceful(server.ListenAndServe, server.Shutdown); err != nil {
	//	log.Error().Err(err).Msg("Failed to start server")
	//	return err
	//}

	log.Info().Msg("Server stopped gracefully")

	return nil
}
