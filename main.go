package main

import (
	"blacked/cmd"
	"blacked/features/providers"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/logger"
	"os"
	"path/filepath"
	"strconv"
	"time"

	stdlog "log"

	"github.com/fatih/color"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := app().Run(os.Args); err != nil {
		stdlog.Fatalf("error running the app: %v", err)
	}
}

func app() *cli.App {
	helpName := color.YellowString(filepath.Base(os.Args[0]))
	year := strconv.Itoa(time.Now().UTC().Year())

	app := &cli.App{
		Usage:       "Backend Service",
		HelpName:    helpName,
		Version:     "v0.0.1",
		Compiled:    time.Now().UTC(),
		Copyright:   "© " + year + " RUNAHO",
		Description: "This application aims to check links in the blacklist.",
		Commands:    cmd.Commands,
		Before:      before,
	}

	app.Suggest = true
	return app
}

func before(c *cli.Context) error {
	logger.InitializeLogger()

	log.Trace().Msg("Initializing configuration")
	if err := config.InitConfig(); err != nil {
		log.Error().Err(err).Stack().Msg("Failed to load config")
		return err
	}
	log.Debug().Msg("Configuration loaded")

	log.Trace().Msg("Initializing database connection")

	_, err := db.GetDB()
	if err != nil {
		log.Error().Err(err).Stack().Msg("Failed to connect to database")
		return err
	}
	log.Debug().Msg("Database connection established")

	log.Trace().Msg("Initializing providers")
	_, err = providers.InitProviders()
	if err != nil {
		log.Error().Err(err).Stack().Msg("Failed to initialize providers")
		return err
	}
	log.Debug().Msg("Providers initialized")

	return nil
}
