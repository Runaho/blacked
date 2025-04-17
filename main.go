package main

import (
	"blacked/cmd"
	"blacked/features/cache"
	"blacked/features/entry_collector"
	"blacked/features/providers"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/logger"
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	stdlog "log"

	"github.com/fatih/color"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	// Set up a context with a timeout for graceful shutdown
	defer cancel()

	// Resource cleanup on exit
	defer cleanup()

	// Pass context to app
	if err := app(ctx).Run(os.Args); err != nil {
		stdlog.Fatalf("error running the app: %v", err)
	}
}

func app(ctx context.Context) *cli.App {
	helpName := color.YellowString(filepath.Base(os.Args[0]))
	year := strconv.Itoa(time.Now().UTC().Year())

	app := &cli.App{
		Usage:       "Backend Service",
		HelpName:    helpName,
		Version:     "v0.1.0",
		Compiled:    time.Now().UTC(),
		Copyright:   "© " + year + " RUNAHO",
		Description: "This application aims to check links in the blacklist.",
		Commands:    cmd.Commands,
		Before:      before(ctx),
		Suggest:     true,
	}

	return app
}

// before returns a cli.BeforeFunc that closes over the context
func before(ctx context.Context) cli.BeforeFunc {
	return func(c *cli.Context) error {
		logger.InitializeLogger()

		log.Trace().Msg("Initializing configuration")
		if err := config.InitConfig(); err != nil {
			log.Error().Err(err).Stack().Msg("Failed to load config")
			return err
		}
		log.Debug().Msg("Configuration loaded")

		log.Trace().Msg("Initializing database connection")
		dbConn, err := db.GetDB()
		if err != nil {
			log.Error().Err(err).Stack().Msg("Failed to connect to database")
			return err
		}
		log.Debug().Msg("Database connection established")

		log.Trace().Msg("Initializing Cache Provider")
		if err := cache.InitializeCache(ctx); err != nil {
			log.Error().Err(err).Stack().Msg("Failed to initialize Cache Provider")
			return err
		}
		log.Debug().Msg("Cache Provider Initialized")

		log.Debug().Msg("Initializing Pond Collector")
		entry_collector.InitPondCollector(ctx, dbConn)
		log.Debug().Msg("Pond Collector Initialized")

		log.Trace().Msg("Initializing providers")
		_, err = providers.InitProviders()
		if err != nil {
			log.Error().Err(err).Stack().Msg("Failed to initialize providers")
			return err
		}
		log.Debug().Msg("Providers initialized")

		return nil
	}
}

// cleanup closes all resources in the correct order
func cleanup() {
	log.Info().Msg("Shutting down: closing resources...")

	// Close pond collector
	if pond := entry_collector.GetPondCollector(); pond != nil {
		pond.Close()
		log.Debug().Msg("Pond collector closed")
	}

	// Close cache
	cache.CloseCache()
	log.Debug().Msg("Cache closed")

	// Close DB
	db.Close()
	log.Debug().Msg("Database closed")
}
