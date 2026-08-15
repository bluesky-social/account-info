// Command account-info serves AT Protocol account information.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/account-info/internal/web"
	"github.com/urfave/cli/v3"
)

const exitFailure = 1

const (
	defaultCacheTTL        = 5 * time.Minute
	defaultCacheErrorTTL   = 30 * time.Second
	defaultCacheMaxEntries = 1_000_000
	defaultLookupRateLimit = 3
)

type serveFunc func(
	context.Context,
	string,
	time.Duration,
	time.Duration,
	int,
	int,
	[]string,
) error

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)

	cmd := newCommand(web.Serve)

	err := cmd.Run(ctx, os.Args)
	stop()
	if err != nil {
		slog.Error("account-info stopped", "error", err)
		os.Exit(exitFailure)
	}
}

func newCommand(serve serveFunc) *cli.Command {
	return &cli.Command{
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
			&cli.DurationFlag{
				Name:    "cache-ttl",
				Value:   defaultCacheTTL,
				Usage:   "TTL for successful account lookups",
				Sources: cli.EnvVars("ACCOUNT_INFO_CACHE_TTL"),
			},
			&cli.DurationFlag{
				Name:    "cache-error-ttl",
				Value:   defaultCacheErrorTTL,
				Usage:   "TTL for failed account lookups",
				Sources: cli.EnvVars("ACCOUNT_INFO_CACHE_ERROR_TTL"),
			},
			&cli.IntFlag{
				Name:    "cache-max-entries",
				Value:   defaultCacheMaxEntries,
				Usage:   "maximum number of cached account lookups",
				Sources: cli.EnvVars("ACCOUNT_INFO_CACHE_MAX_ENTRIES"),
			},
			&cli.IntFlag{
				Name:    "lookup-rate-limit",
				Value:   defaultLookupRateLimit,
				Usage:   "lookup requests allowed per second per source IP (0 disables)",
				Sources: cli.EnvVars("ACCOUNT_INFO_LOOKUP_RATE_LIMIT"),
			},
			&cli.StringSliceFlag{
				Name:  "trusted-proxy-cidr",
				Usage: "CIDR of a direct proxy that appends the client IP to X-Forwarded-For (repeatable)",
				Sources: cli.EnvVars(
					"ACCOUNT_INFO_TRUSTED_PROXY_CIDRS",
				),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return serve(
				ctx,
				cmd.String("address"),
				cmd.Duration("cache-ttl"),
				cmd.Duration("cache-error-ttl"),
				cmd.Int("cache-max-entries"),
				cmd.Int("lookup-rate-limit"),
				cmd.StringSlice("trusted-proxy-cidr"),
			)
		},
	}
}
