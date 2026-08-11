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
func Serve(ctx context.Context, address string) error {
	server := &http.Server{
		Addr:              address,
		Handler:           routes(profile.NewDefaultService()),
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
			return fmt.Errorf("shut down: %w", err)
		}

		return nil
	}
}
