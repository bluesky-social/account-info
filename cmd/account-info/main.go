// Command account-info serves AT Protocol account information.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bluesky-social/account-info/internal/web"
	"github.com/urfave/cli/v3"
)

const exitFailure = 1

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)

	cmd := &cli.Command{
		Name:  "account-info",
		Usage: "serve AT Protocol account information",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "address",
				Aliases: []string{"a"},
				Value:   ":8080",
				Usage:   "HTTP server listen address",
				Sources: cli.EnvVars("ACCOUNT_INFO_ADDRESS"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return web.Serve(ctx, cmd.String("address"))
		},
	}

	err := cmd.Run(ctx, os.Args)
	stop()
	if err != nil {
		slog.Error("account-info stopped", "error", err)
		os.Exit(exitFailure)
	}
}
