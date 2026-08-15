package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBasicRoutes(t *testing.T) {
	t.Parallel()

	accounts := &fakeAccountLookup{}

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "health", path: "/healthz", body: "ok\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				test.path,
				http.NoBody,
			)
			response := httptest.NewRecorder()

			routes(accounts, nil).ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, test.body, response.Body.String())
			require.Equal(
				t,
				"text/plain; charset=utf-8",
				response.Header().Get("Content-Type"),
			)
		})
	}
}

func TestRootExplainsService(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/",
		http.NoBody,
	)
	response := httptest.NewRecorder()

	routes(&fakeAccountLookup{}, nil).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(
		t,
		"text/html; charset=utf-8",
		response.Header().Get("Content-Type"),
	)
	require.Equal(t, "public, max-age=3600", response.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Contains(
		t,
		response.Header().Get("Content-Security-Policy"),
		"form-action 'self'",
	)

	body := response.Body.String()
	require.Contains(t, body, "<style>")
	require.Contains(t, body, ".profile-card")
	require.Contains(t, body, "<title>account.info — AT Protocol profile lookup</title>")
	require.Contains(t, body, "https://account.info/calabro.io")
	require.Contains(t, body, "https://account.info/avatar/calabro.io")
	require.Contains(t, body, "https://github.com/bluesky-social/account-info")
	require.Contains(t, body, "https://atproto.com/")
	require.Contains(t, body, `<form class="lookup" action="/lookup" method="get">`)
	require.Contains(t, body, `name="identifier"`)
	require.Contains(t, body, `placeholder="alice.example or did:plc:..."`)
	require.Contains(t, body, `autocomplete="off"`)
	require.Contains(t, body, `data-1p-ignore`)
	require.Contains(t, body, `>Look up</button>`)
}

func TestLookupRedirectsToExactIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		identifier string
		location   string
	}{
		{name: "handle", identifier: "alice.example", location: "/alice.example"},
		{name: "DID", identifier: "did:plc:alice", location: "/did:plc:alice"},
		{
			name:       "reserved characters remain in one path segment",
			identifier: "//example.com",
			location:   "/%2F%2Fexample.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lookup := &fakeAccountLookup{}
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				"/lookup?"+url.Values{"identifier": {test.identifier}}.Encode(),
				http.NoBody,
			)
			response := httptest.NewRecorder()

			routes(lookup, nil).ServeHTTP(response, request)

			require.Equal(t, http.StatusSeeOther, response.Code)
			require.Equal(t, test.location, response.Header().Get("Location"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Empty(t, lookup.identifier)
		})
	}
}

func TestLookupRejectsEmptyIdentifier(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/lookup?identifier=",
		http.NoBody,
	)
	response := httptest.NewRecorder()

	routes(&fakeAccountLookup{}, nil).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestUnmatchedPathReturnsNotFound(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/unknown/nested/path",
		http.NoBody,
	)
	response := httptest.NewRecorder()

	routes(&fakeAccountLookup{}, nil).ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestAppIcon(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/assets/apps/bluesky.svg",
		http.NoBody,
	)
	response := httptest.NewRecorder()

	routes(&fakeAccountLookup{}, nil).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "image/svg+xml", response.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Contains(t, response.Body.String(), "<svg")
}

func TestUnknownAppIconReturnsNotFound(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/assets/apps/%2e%2e.svg",
		http.NoBody,
	)
	response := httptest.NewRecorder()

	routes(&fakeAccountLookup{}, nil).ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}
