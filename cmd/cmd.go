package cmd

import (
	"log"
	"os"
	App "plus/app"
	_ "plus/pkg"

	"github.com/urfave/cli"
)

func Execute(name, usage, version, commit string) {
	app := cli.NewApp()
	app.Name = name
	app.Usage = usage
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "config, c",
			Value: "config.yaml",
			Usage: "Configuration file path",
		},
		&cli.StringFlag{
			Name:  "listen, l",
			Value: ":8080",
			Usage: "Listen address",
		},
		&cli.StringFlag{
			Name:  "storage-path, s",
			Value: "./storage",
			Usage: "Storage directory path",
		},
		cli.StringFlag{
			Name:  "log",
			Usage: "set the log file to write plus logs to (default is '/dev/stderr')",
		},
		cli.StringFlag{
			Name:  "log-level",
			Value: "debug",
			Usage: "set  the log level ('DEBUG/debug', 'INFO/info', 'WARN/warn', 'ERROR/error', 'FATAL/fatal')",
		},
	}
	app.Action = App.Run

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
