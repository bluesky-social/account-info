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
	lookupCalls int
	avatarCalls int
}

type decodedAccountResponse struct {
	DID           string                            `json:"did"`
	Handle        string                            `json:"handle"`
	PDS           string                            `json:"pds"`
	Authoritative string                            `json:"authoritative"`
	DisplayName   string                            `json:"displayName"`
	Description   string                            `json:"description"`
	Avatar        string                            `json:"avatar"`
	Profiles      map[string]decodedProfileResponse `json:"profiles"`
}

type decodedProfileResponse struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

func (f *fakeAccountLookup) Collections() []string {
	return f.collections
}

func (f *fakeAccountLookup) Lookup(
	_ context.Context,
	identifier string,
	collections []string,
) (profile.Account, error) {
	f.lookupCalls++
	f.identifier = identifier
	f.selected = collections
	return f.account, f.err
}

func (f *fakeAccountLookup) Avatar(
	_ context.Context,
	identifier string,
) (profile.Avatar, error) {
	f.avatarCalls++
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
			response := requestAccountWithHeaders(
				t,
				lookup,
				"/alice.example",
				test.accept,
				"curl/8.14.1",
			)

			require.Equal(t, test.status, response.Code)
			require.Equal(t, "Accept, User-Agent", response.Header().Get("Vary"))
			if test.status == http.StatusTemporaryRedirect {
				require.Equal(t, "/avatar/alice.example", response.Header().Get("Location"))
				require.Equal(t, "public, max-age=300", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestAccountHandlerRendersBrowserLandingPage(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.DisplayName = "Alice Example"
	lookup.account.Description = "Builder of reliable systems."
	lookup.account.Avatar = "https://pds.example/xrpc/com.atproto.sync.getBlob?secret=upstream"
	lookup.account.AvatarContentType = "image/jpeg"
	lookup.account.Profiles[0].CID = "bafydefault"
	lookup.account.Profiles[0].App = &profile.AppLink{
		Name: "Bluesky",
		Icon: "bluesky",
		URL:  "https://bsky.app/profile/alice.example",
	}
	lookup.account.Profiles[1].CID = "bafyother"
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"text/html,application/xhtml+xml,application/json;q=0.8",
		"Mozilla/5.0 Firefox/141.0",
	)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
	require.Equal(t, "Accept, User-Agent", response.Header().Get("Vary"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Contains(t, response.Header().Get("Content-Security-Policy"), "img-src 'self'")

	body := response.Body.String()
	require.Contains(t, body, "<title>Alice Example (@alice.example) — account.info</title>")
	require.Contains(t, body, `<meta property="og:type" content="profile">`)
	require.Contains(t, body, `<meta property="og:site_name" content="account.info">`)
	require.Contains(t, body, `<meta property="og:title" content="Alice Example (@alice.example)">`)
	require.Contains(t, body, `<meta property="og:description" content="Builder of reliable systems.">`)
	require.Contains(t, body, `<meta property="og:url" content="https://account.info/alice.example">`)
	require.Contains(t, body, `<meta property="og:image" content="https://account.info/avatar/alice.example/profile.jpg">`)
	require.Contains(t, body, `<meta property="og:image:secure_url" content="https://account.info/avatar/alice.example/profile.jpg">`)
	require.Contains(t, body, `<meta property="og:image:type" content="image/jpeg">`)
	require.Contains(t, body, `<meta property="og:image:alt" content="Alice Example avatar">`)
	require.Contains(t, body, `<meta name="twitter:card" content="summary">`)
	require.Contains(t, body, `<meta name="twitter:image" content="https://account.info/avatar/alice.example/profile.jpg">`)
	require.Contains(t, body, `<link rel="image_src" href="https://account.info/avatar/alice.example/profile.jpg">`)
	require.Contains(t, body, "Alice Example")
	require.Contains(t, body, "Builder of reliable systems.")
	require.Contains(t, body, "did:plc:alice")
	require.Contains(t, body, "app.bsky.actor.profile")
	require.Contains(t, body, "org.example.profile")
	require.Contains(t, body, "bafydefault")
	require.Contains(t, body, "bafyother")
	require.Contains(t, body, `src="/avatar/alice.example/profile.jpg"`)
	require.Contains(t, body, `href="https://bsky.app/profile/alice.example"`)
	require.Contains(t, body, `src="/assets/apps/bluesky.svg"`)
	require.Contains(t, body, `aria-label="Open @alice.example on Bluesky"`)
	require.Contains(t, body, `target="_blank"`)
	require.NotContains(t, body, "secret=upstream")
}

func TestAccountHandlerRendersHTMLForLinkPreviewCrawler(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.Avatar = "https://pds.example/xrpc/com.atproto.sync.getBlob"
	lookup.account.AvatarContentType = "image/png"
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"*/*",
		"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)",
	)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
	body := response.Body.String()
	require.Contains(t, body, `<meta property="og:title"`)
	require.Contains(t, body, `<meta property="og:image" content="https://account.info/avatar/alice.example/profile.png">`)
	require.Contains(t, body, `<meta property="og:image:type" content="image/png">`)
	require.Contains(t, body, `<link rel="image_src" href="https://account.info/avatar/alice.example/profile.png">`)
}

func TestAccountHandlerRendersWebPAvatar(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.Avatar = "https://pds.example/xrpc/com.atproto.sync.getBlob"
	lookup.account.AvatarContentType = "image/webp"
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"text/html",
		"Mozilla/5.0 Firefox/141.0",
	)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
	body := response.Body.String()
	require.Contains(t, body, `src="/avatar/alice.example/profile.webp"`)
	require.Contains(
		t,
		body,
		`<meta property="og:image:type" content="image/webp">`,
	)
}

func TestAccountHandlerRendersAllProfilesWithoutAuthority(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"text/html",
		"curl/8.14.1",
	)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "No authoritative profile is available")
	require.Contains(t, response.Body.String(), "app.bsky.actor.profile")
	require.Contains(t, response.Body.String(), "org.example.profile")
}

func TestAccountHandlerRendersLookupErrorsOnLookupPageForBrowser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		identifier string
		err        error
		status     int
		message    string
		technical  string
	}{
		{
			name:       "identity not found",
			identifier: "missing.example",
			err:        fmt.Errorf("resolve identity: %w", profile.ErrIdentityNotFound),
			status:     http.StatusNotFound,
			message:    "That account could not be found.",
		},
		{
			name:       "invalid identifier",
			identifier: "asdf",
			err: fmt.Errorf(
				`%w: invalid Handle "asdf": must have at least two labels`,
				profile.ErrInvalidIdentifier,
			),
			status:    http.StatusBadRequest,
			message:   "Enter a valid handle or DID.",
			technical: "must have at least two labels",
		},
		{
			name:       "identity has no PDS",
			identifier: "alice.example",
			err:        fmt.Errorf("resolve identity: %w", profile.ErrNoPDS),
			status:     http.StatusBadGateway,
			message:    "That account does not specify a data server.",
		},
		{
			name:       "lookup timeout",
			identifier: "slow.example",
			err:        fmt.Errorf("resolve identity: %w", context.DeadlineExceeded),
			status:     http.StatusGatewayTimeout,
			message:    "The lookup timed out. Please try again.",
		},
		{
			name:       "upstream failure",
			identifier: "joe.com",
			err:        errors.New("dial joe.com: connection reset by peer"),
			status:     http.StatusBadGateway,
			message:    "We could not retrieve that account right now. Please try again.",
			technical:  "connection reset by peer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response := requestAccountWithHeaders(
				t,
				&fakeAccountLookup{err: test.err},
				"/"+test.identifier,
				"text/html,application/xhtml+xml,application/json;q=0.8",
				"Mozilla/5.0 Firefox/141.0",
			)

			require.Equal(t, test.status, response.Code)
			require.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Equal(t, "Accept, User-Agent", response.Header().Get("Vary"))

			body := response.Body.String()
			require.Contains(t, body, "<title>account.info — AT Protocol profile lookup</title>")
			require.Contains(t, body, `value="`+test.identifier+`"`)
			require.Contains(t, body, `aria-invalid="true" aria-describedby="lookup-error"`)
			require.Contains(t, body, `<p id="lookup-error" class="lookup-error" role="alert">`)
			require.Contains(t, body, test.message)
			if test.technical != "" {
				require.NotContains(t, body, test.technical)
			}
			require.NotContains(t, body, `"error":`)
		})
	}
}

func TestAccountHandlerRendersMissingProfileOnLookupPageForBrowser(t *testing.T) {
	t.Parallel()

	response := requestAccountWithHeaders(
		t,
		&fakeAccountLookup{account: testAccount(0)},
		"/alice.example",
		"text/html",
		"Mozilla/5.0 Firefox/141.0",
	)

	require.Equal(t, http.StatusNotFound, response.Code)
	require.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Contains(t, response.Body.String(), `value="alice.example"`)
	require.Contains(
		t,
		response.Body.String(),
		"No supported profile could be found for that account.",
	)
}

func TestAccountHandlerExplicitJSONPreservesPayloadForBrowser(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.Profiles[0].App = &profile.AppLink{
		Name: "Bluesky",
		Icon: "bluesky",
		URL:  "https://bsky.app/profile/alice.example",
	}
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"application/json",
		"Mozilla/5.0 Firefox/141.0",
	)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json; charset=utf-8", response.Header().Get("Content-Type"))
	var body decodedAccountResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Profiles, 2)
	require.NotContains(t, response.Body.String(), "bsky.app")
}

func TestAccountHandlerRejectsUnacceptableRepresentationBeforeLookup(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"application/xml",
		"Mozilla/5.0 Firefox/141.0",
	)

	require.Equal(t, http.StatusNotAcceptable, response.Code)
	require.Empty(t, lookup.identifier)
}

func TestAccountLandingPageEscapesUpstreamContent(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.DisplayName = `<script>alert("display name")</script>`
	lookup.account.Description = `<img src=x onerror=alert("description")>`
	lookup.account.Profiles[0].Value = json.RawMessage(
		`{"payload":"</script><script>alert('record')</script>"}`,
	)
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"text/html",
		"Mozilla/5.0 Firefox/141.0",
	)

	require.Equal(t, http.StatusOK, response.Code)
	body := response.Body.String()
	require.NotContains(t, body, `<script>alert("display name")</script>`)
	require.NotContains(t, body, `<img src=x onerror=alert("description")>`)
	require.NotContains(t, body, `<script>alert('record')</script>`)
	require.Contains(t, body, "&lt;script&gt;")
}

func TestAccountLandingPageRejectsDuplicateProfileCollections(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.Profiles[1].Collection = "app.bsky.actor.profile"
	response := requestAccountWithHeaders(
		t,
		lookup,
		"/alice.example",
		"text/html",
		"Mozilla/5.0 Firefox/141.0",
	)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	var body errorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "internal_error", body.Error)
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

func TestAvatarHandlerServesExtensionBearingPreviewURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		path        string
	}{
		{name: "JPEG", contentType: "image/jpeg", path: "/avatar/alice.example/profile.jpg"},
		{name: "WebP", contentType: "image/webp", path: "/avatar/alice.example/profile.webp"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			content := []byte("verified image bytes")
			lookup := &fakeAccountLookup{avatar: profile.Avatar{
				Content:     content,
				ContentType: test.contentType,
				CID:         "bafkreicid",
			}}
			response := requestAccountWithAccept(t, lookup, test.path, "image/*")

			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "alice.example", lookup.avatarID)
			require.Equal(t, content, response.Body.Bytes())
			require.Equal(t, test.contentType, response.Header().Get("Content-Type"))
		})
	}
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
	routes(lookup, nil).ServeHTTP(response, request)

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
			routes(&fakeAccountLookup{avatar: avatar}, nil).ServeHTTP(response, request)

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

func TestAccountHandlerReturnsAuthoritativeProfileAndAllRecords(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.DisplayName = "Alice"
	lookup.account.Description = "Builder"
	lookup.account.Avatar = "https://pds.example/xrpc/com.atproto.sync.getBlob"
	response := requestAccount(t, lookup, "/alice.example")

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "alice.example", lookup.identifier)
	var body decodedAccountResponse
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
	require.Equal(
		t,
		map[string]decodedProfileResponse{
			"app.bsky.actor.profile": decodedProfile(&lookup.account.Profiles[0]),
			"org.example.profile":    decodedProfile(&lookup.account.Profiles[1]),
		},
		body.Profiles,
	)
	var raw struct {
		Profiles map[string]map[string]json.RawMessage `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &raw))
	require.NotContains(t, raw.Profiles["app.bsky.actor.profile"], "collection")
}

func TestAccountHandlerReturnsAllProfilesWithoutAuthority(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	response := requestAccount(t, lookup, "/alice.example")

	require.Equal(t, http.StatusOK, response.Code)
	var body decodedAccountResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Empty(t, body.Authoritative)
	require.Equal(
		t,
		map[string]decodedProfileResponse{
			"app.bsky.actor.profile": decodedProfile(&lookup.account.Profiles[0]),
			"org.example.profile":    decodedProfile(&lookup.account.Profiles[1]),
		},
		body.Profiles,
	)
}

func TestAccountHandlerIgnoresRemovedAllParameter(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	response := requestAccount(t, lookup, "/alice.example?all=sometimes")

	require.Equal(t, http.StatusOK, response.Code)
	var body decodedAccountResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
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
	var body decodedAccountResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Len(t, body.Profiles, 1)
	require.Contains(t, body.Profiles, "app.bsky.actor.profile")
}

func TestAccountHandlerRejectsDuplicateProfileCollections(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(2)}
	lookup.account.Authoritative = "app.bsky.actor.profile"
	lookup.account.Profiles[1].Collection = "app.bsky.actor.profile"
	response := requestAccount(t, lookup, "/alice.example")

	require.Equal(t, http.StatusInternalServerError, response.Code)
	var body errorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, "internal_error", body.Error)
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
			name:   "no profile",
			target: "/alice.example",
			lookup: &fakeAccountLookup{account: testAccount(0)},
			status: http.StatusNotFound,
			code:   "profile_not_found",
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
	return requestAccountWithAccept(t, lookup, target, "application/json")
}

func requestAccountWithAccept(
	t *testing.T,
	lookup accountLookup,
	target string,
	accept string,
) *httptest.ResponseRecorder {
	return requestAccountWithHeaders(t, lookup, target, accept, "")
}

func requestAccountWithHeaders(
	t *testing.T,
	lookup accountLookup,
	target string,
	accept string,
	userAgent string,
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
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	response := httptest.NewRecorder()
	routes(lookup, nil).ServeHTTP(response, request)
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

func decodedProfile(record *profile.Record) decodedProfileResponse {
	return decodedProfileResponse{
		URI:   record.URI,
		CID:   record.CID,
		Value: record.Value,
	}
}
