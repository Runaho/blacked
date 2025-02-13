package cmd

import (
	"blacked/cmd/provider_processor"

	"github.com/urfave/cli/v2"
)

var Commands = []*cli.Command{
	provider_processor.ProcessCommand,
	QueryCommand,
	WebServer,
}
