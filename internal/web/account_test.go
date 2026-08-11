package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/account-info/internal/profile"
	"github.com/stretchr/testify/require"
)

type fakeAccountLookup struct {
	account     profile.Account
	err         error
	collections []string
	identifier  string
	selected    []string
}

func (f *fakeAccountLookup) Collections() []string {
	return f.collections
}

func (f *fakeAccountLookup) Lookup(
	_ context.Context,
	identifier string,
	collections []string,
) (profile.Account, error) {
	f.identifier = identifier
	f.selected = collections
	return f.account, f.err
}

func TestAccountHandlerReturnsAuthoritativeProfile(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.DisplayName = "Alice"
	lookup.account.Description = "Builder"
	lookup.account.Avatar = "https://pds.example/xrpc/com.atproto.sync.getBlob"
	response := requestAccount(t, lookup, "/alice.example")

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "alice.example", lookup.identifier)
	var body profile.Account
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, lookup.account.DID, body.DID)
	require.Equal(t, "app.bsky.actor.profile", body.Authoritative)
	require.Equal(t, "Alice", body.DisplayName)
	require.Equal(t, "Builder", body.Description)
	require.Equal(
		t,
		"https://pds.example/xrpc/com.atproto.sync.getBlob",
		body.Avatar,
	)
	require.Len(t, body.Profiles, 1)
	require.Equal(t, "app.bsky.actor.profile", body.Profiles[0].Collection)
}

func TestAccountHandlerReturnsAllProfiles(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	response := requestAccount(t, lookup, "/alice.example?all=true")

	require.Equal(t, http.StatusOK, response.Code)
	var body profile.Account
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "app.bsky.actor.profile", body.Authoritative)
	require.Len(t, body.Profiles, 2)
}

func TestAccountHandlerFiltersCollections(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	response := requestAccount(
		t,
		lookup,
		"/alice.example?collection=app.bsky.actor.profile",
	)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(
		t,
		[]string{"app.bsky.actor.profile"},
		lookup.selected,
	)
	var body profile.Account
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Profiles, 1)
}

func TestAccountHandlerErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		lookup *fakeAccountLookup
		status int
		code   string
	}{
		{
			name:   "invalid all",
			target: "/alice.example?all=sometimes",
			lookup: &fakeAccountLookup{},
			status: http.StatusBadRequest,
			code:   "invalid_query",
		},
		{
			name:   "no profile",
			target: "/alice.example",
			lookup: &fakeAccountLookup{account: testAccount(0)},
			status: http.StatusNotFound,
			code:   "profile_not_found",
		},
		{
			name:   "multiple profiles",
			target: "/alice.example",
			lookup: &fakeAccountLookup{account: testAccount(2)},
			status: http.StatusConflict,
			code:   "multiple_profiles",
		},
		{
			name:   "invalid identifier",
			target: "/not-a-valid-identifier",
			lookup: &fakeAccountLookup{err: profile.ErrInvalidIdentifier},
			status: http.StatusBadRequest,
			code:   "invalid_identifier",
		},
		{
			name:   "identity not found",
			target: "/alice.example",
			lookup: &fakeAccountLookup{err: profile.ErrIdentityNotFound},
			status: http.StatusNotFound,
			code:   "account_not_found",
		},
		{
			name:   "unsupported collection",
			target: "/alice.example?collection=bad.example.profile",
			lookup: &fakeAccountLookup{
				err:         profile.ErrUnsupportedCollection,
				collections: []string{"app.bsky.actor.profile"},
			},
			status: http.StatusBadRequest,
			code:   "unsupported_collection",
		},
		{
			name:   "lookup timeout",
			target: "/alice.example",
			lookup: &fakeAccountLookup{
				err: fmt.Errorf("get profile: %w", context.DeadlineExceeded),
			},
			status: http.StatusGatewayTimeout,
			code:   "upstream_timeout",
		},
		{
			name:   "upstream error",
			target: "/alice.example",
			lookup: &fakeAccountLookup{err: errors.New("network failure")},
			status: http.StatusBadGateway,
			code:   "upstream_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response := requestAccount(t, test.lookup, test.target)
			require.Equal(t, test.status, response.Code)
			var body errorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, test.code, body.Error)
		})
	}
}

func TestWriteJSONEncodeFailureReturnsInternalError(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.Profiles[0].Value = json.RawMessage(`{invalid`)
	response := requestAccount(t, lookup, "/alice.example")

	require.Equal(t, http.StatusInternalServerError, response.Code)
	var body errorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "internal_error", body.Error)
}

func requestAccount(
	t *testing.T,
	lookup accountLookup,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		target,
		http.NoBody,
	)
	response := httptest.NewRecorder()
	routes(lookup).ServeHTTP(response, request)
	return response
}

func testAccount(profileCount int) profile.Account {
	account := profile.Account{
		DID:      "did:plc:alice",
		Handle:   "alice.example",
		PDS:      "https://pds.example",
		Profiles: make([]profile.Record, 0, profileCount),
	}
	collections := []string{
		"app.bsky.actor.profile",
		"org.example.profile",
	}
	for index := range profileCount {
		account.Profiles = append(account.Profiles, profile.Record{
			Collection: collections[index],
			URI:        "at://did:plc:alice/" + collections[index] + "/self",
			Value:      json.RawMessage(`{"name":"Alice"}`),
		})
	}
	return account
}
