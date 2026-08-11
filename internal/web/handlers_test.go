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
		{name: "root", path: "/", body: "account.info\n"},
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
