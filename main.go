package main

import (
	"blacked/cmd"
	"blacked/features/entries/providers"
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
	stdlog.Print("Initializing application configuration")
	if err := config.InitConfig(); err != nil {
		stdlog.Fatalf("error loading config: %v", err)
		return err
	}

	logger.InitializeLogger()

	log.Info().Msg("Initializing database connection")
	dbConn, err := db.GetDB()
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to database")
		return err
	}

	log.Info().Msg("Initializing providers")
	_, err = providers.NewProviders(dbConn)
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize providers")
		return err
	}

	return nil
}
