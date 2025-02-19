package logger

import (
	"blacked/internal/config"
	stdlog "log"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	zerologger zerolog.Logger
)

type logWrapper struct {
	zerolog.Logger
}

func (l logWrapper) Write(p []byte) (n int, err error) {
	n = len(p)
	if n > 0 && p[n-1] == '\n' {
		p = p[:n-1]
	}
	l.Info().Msg(string(p))
	return
}

func InitializeLogger() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if config.IsDevMode() {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	if isatty.IsTerminal(os.Stdout.Fd()) {
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: zerolog.TimeFormatUnix}
		zerologger = zerolog.New(output)
	} else {
		zerologger = zerolog.New(os.Stdout)
	}

	zerologger = zerologger.With().Timestamp().Caller().Logger()

	log.Logger = zerologger

	stdlog.SetFlags(0)
	stdlog.SetOutput(logWrapper{zerologger})
}
