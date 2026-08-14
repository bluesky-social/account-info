// Package web provides the account.info HTTP server.
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/account-info/internal/profile"
)

// Serve starts the HTTP server and blocks until it stops or ctx is canceled.
func Serve(
	ctx context.Context,
	address string,
	cacheTTL time.Duration,
	cacheErrorTTL time.Duration,
	cacheMaxEntries int,
) error {
	accounts, err := profile.NewDefaultService(profile.CacheConfig{
		TTL:        cacheTTL,
		ErrorTTL:   cacheErrorTTL,
		MaxEntries: cacheMaxEntries,
	})
	if err != nil {
		return fmt.Errorf("configure account cache: %w", err)
	}

	server := &http.Server{
		Addr:              address,
		Handler:           routes(accounts),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "address", address)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen: %w", err)

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		slog.Info("shutting down HTTP server")
		if err := server.Shutdown(shutdownCtx); err != nil {
			// Shutdown returns on context expiry without closing active
			// connections; force-close so Serve never leaks handlers.
			return errors.Join(
				fmt.Errorf("shut down: %w", err),
				server.Close(),
			)
		}

		return nil
	}
}
