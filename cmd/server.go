package cmd

import (
	"context"
	"net/http"

	"blacked/features/cache"
	"blacked/features/entry_collector"
	"blacked/features/providers"
	"blacked/features/web"
	"blacked/internal/config"
	"blacked/internal/runner"

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

	log.Trace().Msg("Initializing Cache Provider")
	if err = cache.InitializeCache(c.Context); err != nil {
		log.Error().Err(err).Msg("Failed to initialize Cache Provider")
		return err
	}

	entryCollector := entry_collector.GetPondCollector()
	if ok := entryCollector.ScheduleCacheSync(true); !ok {
		log.Error().Msg("Failed to schedule cache sync")
		return err
	}
	log.Debug().Msg("Badger cache initialized")

	// Initialize providers — if this fails, we should NOT start the server
	if _, err := providers.InitProviders(); err != nil {
		log.Error().Err(err).Msg("Provider initialization failed — aborting server start")
		return err
	}
	log.Debug().Msg("Providers initialized")

	// Run startup decision engine asynchronously (non-blocking).
	// Server starts serving requests while startup providers run in background.
	// Startup process is observable via /provider/process/status/:processID
	runner.RunStartupProvidersAsync(c.Context, *app.GetProviders())

	if _, err := runner.InitializeRunner(*app.GetProviders()); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize runner")
	}

	defer runner.ShutdownRunner(c.Context)

	// Graceful shutdown with context timeout using native http.Server.Shutdown()
	// This properly drains in-flight requests before closing connections
	server := app.Echo.Server
	shutdownTimeout := cfg.Server.ShutdownTimeout
	shutdownCtx, cancel := context.WithTimeout(c.Context, shutdownTimeout)
	defer cancel()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Server error")
		}
		cancel() // Cancel shutdown context when server stops
	}()

	log.Info().Msgf("Starting server on %s", server.Addr)

	// Wait for shutdown signal
	<-c.Context.Done()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Graceful shutdown failed, forcing close")
		return server.Close()
	}

	log.Info().Msg("Server stopped gracefully.")
	return nil
}
