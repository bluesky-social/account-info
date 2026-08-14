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
	avatar      profile.Avatar
	err         error
	avatarErr   error
	collections []string
	identifier  string
	avatarID    string
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

func (f *fakeAccountLookup) Avatar(
	_ context.Context,
	identifier string,
) (profile.Avatar, error) {
	f.avatarID = identifier
	return f.avatar, f.avatarErr
}

func TestAccountHandlerRedirectsPreferredImageRepresentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		accept string
		status int
	}{
		{name: "image wildcard", accept: "image/*", status: http.StatusTemporaryRedirect},
		{
			name:   "browser image request",
			accept: "image/avif,image/webp,image/*,*/*;q=0.8",
			status: http.StatusTemporaryRedirect,
		},
		{name: "curl default", accept: "*/*", status: http.StatusOK},
		{name: "JSON preferred", accept: "image/*;q=0.5, application/json;q=0.9", status: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookup := &fakeAccountLookup{account: testAccount(1)}
			lookup.account.Authoritative = "app.bsky.actor.profile"
			response := requestAccountWithAccept(
				t,
				lookup,
				"/alice.example",
				test.accept,
			)

			require.Equal(t, test.status, response.Code)
			require.Equal(t, "Accept", response.Header().Get("Vary"))
			if test.status == http.StatusTemporaryRedirect {
				require.Equal(t, "/avatar/alice.example", response.Header().Get("Location"))
				require.Equal(t, "public, max-age=300", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestAvatarHandlerServesCacheableImage(t *testing.T) {
	t.Parallel()

	content := []byte("verified image bytes")
	lookup := &fakeAccountLookup{avatar: profile.Avatar{
		Content:     content,
		ContentType: "image/png",
		CID:         "bafkreicid",
	}}
	response := requestAccountWithAccept(
		t,
		lookup,
		"/avatar/alice.example",
		"application/json",
	)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "alice.example", lookup.avatarID)
	require.Equal(t, content, response.Body.Bytes())
	require.Equal(t, "image/png", response.Header().Get("Content-Type"))
	require.Equal(t, `"bafkreicid"`, response.Header().Get("ETag"))
	require.Equal(t, "inline; filename=avatar.png", response.Header().Get("Content-Disposition"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "*", response.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(
		t,
		"public, max-age=300",
		response.Header().Get("Cache-Control"),
	)
	require.Empty(t, response.Header().Get("Vary"))
}

func TestAvatarHandlerHonorsConditionalRequest(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{avatar: profile.Avatar{
		Content:     []byte("verified image bytes"),
		ContentType: "image/jpeg",
		CID:         "bafkreicid",
	}}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/avatar/alice.example",
		http.NoBody,
	)
	request.Header.Set("If-None-Match", `"bafkreicid"`)
	response := httptest.NewRecorder()
	routes(lookup).ServeHTTP(response, request)

	require.Equal(t, http.StatusNotModified, response.Code)
	require.Empty(t, response.Body.Bytes())
}

func TestAvatarHandlerSupportsHeadAndRanges(t *testing.T) {
	t.Parallel()

	avatar := profile.Avatar{
		Content:     []byte("verified image bytes"),
		ContentType: "image/jpeg",
		CID:         "bafkreicid",
	}
	tests := []struct {
		name        string
		method      string
		rangeHeader string
		status      int
		body        string
	}{
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{
			name:        "range",
			method:      http.MethodGet,
			rangeHeader: "bytes=9-13",
			status:      http.StatusPartialContent,
			body:        "image",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(
				context.Background(),
				test.method,
				"/avatar/alice.example",
				http.NoBody,
			)
			if test.rangeHeader != "" {
				request.Header.Set("Range", test.rangeHeader)
			}
			response := httptest.NewRecorder()
			routes(&fakeAccountLookup{avatar: avatar}).ServeHTTP(response, request)

			require.Equal(t, test.status, response.Code)
			require.Equal(t, test.body, response.Body.String())
			require.Equal(t, "bytes", response.Header().Get("Accept-Ranges"))
		})
	}
}

func TestAvatarHandlerErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "profile missing", err: profile.ErrProfileNotFound, status: http.StatusNotFound, code: "profile_not_found"},
		{name: "avatar missing", err: profile.ErrAvatarNotFound, status: http.StatusNotFound, code: "avatar_not_found"},
		{name: "ambiguous profile", err: profile.ErrMultipleProfiles, status: http.StatusConflict, code: "multiple_profiles"},
		{name: "upstream failure", err: errors.New("blob failed"), status: http.StatusBadGateway, code: "upstream_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := requestAccount(
				t,
				&fakeAccountLookup{avatarErr: test.err},
				"/avatar/alice.example",
			)
			require.Equal(t, test.status, response.Code)
			var body errorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, test.code, body.Error)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
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
	return requestAccountWithAccept(t, lookup, target, "")
}

func requestAccountWithAccept(
	t *testing.T,
	lookup accountLookup,
	target string,
	accept string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		target,
		http.NoBody,
	)
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
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
