package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
	Avatar(context.Context, string) (profile.Avatar, error)
}

type errorResponse struct {
	Error       string   `json:"error"`
	Message     string   `json:"message"`
	Collections []string `json:"collections,omitempty"`
}

type accountResponse struct {
	DID           string                           `json:"did"`
	Handle        string                           `json:"handle,omitempty"`
	PDS           string                           `json:"pds"`
	Authoritative string                           `json:"authoritative,omitempty"`
	DisplayName   string                           `json:"displayName,omitempty"`
	Description   string                           `json:"description,omitempty"`
	Avatar        string                           `json:"avatar,omitempty"`
	Profiles      map[string]profileRecordResponse `json:"profiles"`
}

type profileRecordResponse struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid,omitempty"`
	Value json.RawMessage `json:"value"`
}

func handleAccount(accounts accountLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Vary", "Accept, User-Agent")
		representation := negotiateAccountRepresentation(
			request.Header.Values("Accept"),
			request.UserAgent(),
		)
		if representation == representationNotAcceptable {
			writeError(
				w,
				http.StatusNotAcceptable,
				"not_acceptable",
				"available representations are text/html, application/json, and image/*",
				nil,
			)
			return
		}
		if representation == representationImage {
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.Redirect(
				w,
				request,
				"/avatar/"+url.PathEscape(request.PathValue("identifier")),
				http.StatusTemporaryRedirect,
			)
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
			if representation == representationHTML {
				failure := classifyLookupFailure(err, accounts.Collections())
				logLookupFailure(failure, err)
				writeIndexHTML(w, failure.status, indexPage{
					Identifier:  request.PathValue("identifier"),
					LookupError: failure.htmlMessage,
				})
				return
			}
			writeLookupError(w, err, accounts.Collections())
			return
		}

		if len(account.Profiles) == 0 {
			if representation == representationHTML {
				writeIndexHTML(w, http.StatusNotFound, indexPage{
					Identifier: request.PathValue("identifier"),
					LookupError: "No supported profile could be found for " +
						"that account.",
				})
				return
			}
			writeError(
				w,
				http.StatusNotFound,
				"profile_not_found",
				"no matching profile record was found",
				nil,
			)
			return
		}
		if representation == representationHTML {
			writeAccountHTML(w, &account)
			return
		}
		writeAccountJSON(w, http.StatusOK, &account)
	}
}

type lookupFailure struct {
	status      int
	code        string
	message     string
	htmlMessage string
	collections []string
	logLevel    slog.Level
	logMessage  string
}

func handleAvatar(accounts accountLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ctx, cancel := context.WithTimeout(request.Context(), lookupTimeout)
		defer cancel()

		avatar, err := accounts.Avatar(ctx, request.PathValue("identifier"))
		if err != nil {
			writeLookupError(w, err, accounts.Collections())
			return
		}

		extension := ".bin"
		switch avatar.ContentType {
		case "image/jpeg":
			extension = ".jpg"
		case "image/png":
			extension = ".png"
		}
		w.Header().Set("Content-Type", avatar.ContentType)
		w.Header().Set("Content-Disposition", "inline; filename=avatar"+extension)
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("ETag", strconv.Quote(avatar.CID))
		http.ServeContent(
			w,
			request,
			"avatar"+extension,
			time.Time{},
			bytes.NewReader(avatar.Content),
		)
	}
}

func writeLookupError(
	w http.ResponseWriter,
	err error,
	collections []string,
) {
	failure := classifyLookupFailure(err, collections)
	logLookupFailure(failure, err)
	writeError(
		w,
		failure.status,
		failure.code,
		failure.message,
		failure.collections,
	)
}

func classifyLookupFailure(err error, collections []string) lookupFailure {
	switch {
	case errors.Is(err, profile.ErrInvalidIdentifier):
		return lookupFailure{
			status:      http.StatusBadRequest,
			code:        "invalid_identifier",
			message:     err.Error(),
			htmlMessage: "Enter a valid handle or DID.",
		}
	case errors.Is(err, profile.ErrIdentityNotFound):
		return lookupFailure{
			status:      http.StatusNotFound,
			code:        "account_not_found",
			message:     err.Error(),
			htmlMessage: "That account could not be found.",
		}
	case errors.Is(err, profile.ErrNoPDS):
		return lookupFailure{
			status:      http.StatusBadGateway,
			code:        "pds_not_found",
			message:     err.Error(),
			htmlMessage: "That account does not specify a data server.",
		}
	case errors.Is(err, profile.ErrUnsupportedCollection):
		return lookupFailure{
			status:      http.StatusBadRequest,
			code:        "unsupported_collection",
			message:     err.Error(),
			htmlMessage: "That profile type is not supported.",
			collections: collections,
		}
	case errors.Is(err, profile.ErrProfileNotFound):
		return lookupFailure{
			status:      http.StatusNotFound,
			code:        "profile_not_found",
			message:     err.Error(),
			htmlMessage: "No supported profile could be found for that account.",
		}
	case errors.Is(err, profile.ErrAvatarNotFound):
		return lookupFailure{
			status:      http.StatusNotFound,
			code:        "avatar_not_found",
			message:     err.Error(),
			htmlMessage: "That account does not have an avatar.",
		}
	case errors.Is(err, profile.ErrMultipleProfiles):
		return lookupFailure{
			status:      http.StatusConflict,
			code:        "multiple_profiles",
			message:     err.Error(),
			htmlMessage: "A default profile could not be selected for that account.",
		}
	case errors.Is(err, context.DeadlineExceeded):
		return lookupFailure{
			status:      http.StatusGatewayTimeout,
			code:        "upstream_timeout",
			message:     "profile lookup timed out",
			htmlMessage: "The lookup timed out. Please try again.",
			logLevel:    slog.LevelWarn,
			logMessage:  "profile lookup timed out",
		}
	default:
		return lookupFailure{
			status:  http.StatusBadGateway,
			code:    "upstream_error",
			message: "failed to retrieve profile information",
			htmlMessage: "We could not retrieve that account right now. " +
				"Please try again.",
			logLevel:   slog.LevelError,
			logMessage: "profile lookup failed",
		}
	}
}

func logLookupFailure(failure lookupFailure, err error) {
	if failure.logMessage == "" {
		return
	}
	slog.Log(
		context.Background(),
		failure.logLevel,
		failure.logMessage,
		"error",
		err,
	)
}

func writeError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
	collections []string,
) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, errorResponse{
		Error:       code,
		Message:     message,
		Collections: collections,
	})
}

func writeAccountJSON(w http.ResponseWriter, status int, account *profile.Account) {
	response, err := newAccountResponse(account)
	if err != nil {
		slog.Error("build account response", "error", err)
		writeError(
			w,
			http.StatusInternalServerError,
			"internal_error",
			"failed to encode response",
			nil,
		)
		return
	}
	writeJSON(w, status, response)
}

func newAccountResponse(account *profile.Account) (accountResponse, error) {
	profiles := make(map[string]profileRecordResponse, len(account.Profiles))
	for _, record := range account.Profiles {
		if record.Collection == "" {
			return accountResponse{}, fmt.Errorf("profile collection is empty")
		}
		if _, exists := profiles[record.Collection]; exists {
			return accountResponse{}, fmt.Errorf(
				"duplicate profile collection: %s",
				record.Collection,
			)
		}
		profiles[record.Collection] = profileRecordResponse{
			URI:   record.URI,
			CID:   record.CID,
			Value: record.Value,
		}
	}

	return accountResponse{
		DID:           account.DID,
		Handle:        account.Handle,
		PDS:           account.PDS,
		Authoritative: account.Authoritative,
		DisplayName:   account.DisplayName,
		Description:   account.Description,
		Avatar:        account.Avatar,
		Profiles:      profiles,
	}, nil
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
