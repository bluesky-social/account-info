package web

import (
	"context"
	"net/http"
	"net/http/httptest"
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

			routes(accounts).ServeHTTP(response, request)

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

	routes(&fakeAccountLookup{}).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(
		t,
		"text/html; charset=utf-8",
		response.Header().Get("Content-Type"),
	)
	require.Equal(t, "public, max-age=3600", response.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))

	body := response.Body.String()
	require.Contains(t, body, "<title>account.info — AT Protocol profile lookup</title>")
	require.Contains(t, body, "https://account.info/calabro.io")
	require.Contains(t, body, "https://account.info/avatar/calabro.io")
	require.Contains(t, body, "https://github.com/bluesky-social/account-info")
	require.Contains(t, body, "https://atproto.com/")
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

	routes(&fakeAccountLookup{}).ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}
