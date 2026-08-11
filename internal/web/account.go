package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/bluesky-social/account-info/internal/profile"
)

// lookupTimeout bounds one account lookup end to end. It must stay below
// the server's WriteTimeout so the handler can still write an error response.
const lookupTimeout = 25 * time.Second

type accountLookup interface {
	Collections() []string
	Lookup(context.Context, string, []string) (profile.Account, error)
}

type errorResponse struct {
	Error       string   `json:"error"`
	Message     string   `json:"message"`
	Collections []string `json:"collections,omitempty"`
}

func handleAccount(accounts accountLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		all, err := parseAll(request)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_query", err.Error(), nil)
			return
		}

		// Bound the whole lookup (identity resolution + record fetches,
		// including the XRPC client's internal retries) below the server's
		// 30s WriteTimeout so a slow PDS can't stack retries past it.
		ctx, cancel := context.WithTimeout(request.Context(), lookupTimeout)
		defer cancel()

		collections := request.URL.Query()["collection"]
		account, err := accounts.Lookup(
			ctx,
			request.PathValue("identifier"),
			collections,
		)
		if err != nil {
			writeLookupError(w, err, accounts.Collections())
			return
		}

		if len(account.Profiles) == 0 {
			writeError(
				w,
				http.StatusNotFound,
				"profile_not_found",
				"no matching profile record was found",
				nil,
			)
			return
		}
		if all || len(collections) > 0 {
			writeJSON(w, http.StatusOK, account)
			return
		}
		if account.Authoritative != "" {
			for _, record := range account.Profiles {
				if record.Collection == account.Authoritative {
					account.Profiles = []profile.Record{record}
					writeJSON(w, http.StatusOK, account)
					return
				}
			}
		}
		if len(account.Profiles) == 1 {
			writeJSON(w, http.StatusOK, account)
			return
		}

		available := make([]string, 0, len(account.Profiles))
		for _, record := range account.Profiles {
			available = append(available, record.Collection)
		}
		writeError(
			w,
			http.StatusConflict,
			"multiple_profiles",
			"multiple profiles are available without an authoritative profile",
			available,
		)
	}
}

func parseAll(request *http.Request) (bool, error) {
	raw := request.URL.Query().Get("all")
	if raw == "" {
		return false, nil
	}
	all, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("all must be a boolean")
	}
	return all, nil
}

func writeLookupError(
	w http.ResponseWriter,
	err error,
	collections []string,
) {
	switch {
	case errors.Is(err, profile.ErrInvalidIdentifier):
		writeError(
			w,
			http.StatusBadRequest,
			"invalid_identifier",
			err.Error(),
			nil,
		)
	case errors.Is(err, profile.ErrIdentityNotFound):
		writeError(w, http.StatusNotFound, "account_not_found", err.Error(), nil)
	case errors.Is(err, profile.ErrNoPDS):
		writeError(w, http.StatusBadGateway, "pds_not_found", err.Error(), nil)
	case errors.Is(err, profile.ErrUnsupportedCollection):
		writeError(
			w,
			http.StatusBadRequest,
			"unsupported_collection",
			err.Error(),
			collections,
		)
	case errors.Is(err, context.DeadlineExceeded):
		slog.Warn("profile lookup timed out", "error", err)
		writeError(
			w,
			http.StatusGatewayTimeout,
			"upstream_timeout",
			"profile lookup timed out",
			nil,
		)
	default:
		slog.Error("profile lookup failed", "error", err)
		writeError(
			w,
			http.StatusBadGateway,
			"upstream_error",
			"failed to retrieve profile information",
			nil,
		)
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
	collections []string,
) {
	writeJSON(w, status, errorResponse{
		Error:       code,
		Message:     message,
		Collections: collections,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	// Marshal before committing the status so an encode failure (e.g.
	// invalid upstream json.RawMessage in a Record) yields a 500 instead
	// of the requested status with a truncated body.
	payload, err := json.Marshal(value)
	if err != nil {
		slog.Error("encode JSON response", "error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(
			[]byte(`{"error":"internal_error","message":"failed to encode response"}` + "\n"),
		)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(append(payload, '\n')); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}
