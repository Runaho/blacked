package blacked

import (
	"blacked/cmd"
	"blacked/internal/config"
	"blacked/internal/logger"
	"os"
	"path/filepath"
	"strconv"
	"time"

	stdlog "log"

	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := config.InitConfig(); err != nil {
		stdlog.Fatalf("error loading config: %v", err)
	}

	logger.InitializeLogger()

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
	}

	app.Suggest = true
	return app
}
