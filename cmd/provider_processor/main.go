package provider_processor

import (
	"github.com/urfave/cli/v2"
)

// ProcessCommand processes blacklist entries from specified providers.
    var ProcessCommand = &cli.Command{
    	Name:  "process",
    	Usage: "Process blacklist entries from providers",
    	Flags: []cli.Flag{
    		&cli.StringSliceFlag{
    			Name:    "provider",
    			Aliases: []string{"p"},
    			Usage:   "Specify providers to process (comma-separated). If omitted, process all providers.",
    		},
    		&cli.StringSliceFlag{
    			Name:    "remove-provider",
    			Aliases: []string{"r"},
    			Usage:   "Specify providers to remove (comma-separated)",
    		},
    	},
    	Action: processAction,
    }

    func processAction(c *cli.Context) error {
    	return Process(c.StringSlice("provider"), c.StringSlice("remove-provider"))
    }
