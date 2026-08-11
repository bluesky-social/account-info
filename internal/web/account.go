package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/bluesky-social/account-info/internal/profile"
)

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

		collections := request.URL.Query()["collection"]
		account, err := accounts.Lookup(
			request.Context(),
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}
