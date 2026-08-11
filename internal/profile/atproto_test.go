package profile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordReaderNotFoundMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "protocol RecordNotFound maps to ErrRecordNotFound",
			body: `{"error":"RecordNotFound","message":"no record"}`,
			want: ErrRecordNotFound,
		},
		{
			name: "bare 404 propagates as an upstream error",
			body: `{"error":"NotFound","message":"no such route"}`,
			want: nil, // any error other than ErrRecordNotFound
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(test.body))
				},
			))
			defer server.Close()

			reader := &atprotoRecordReader{httpClient: server.Client()}
			_, err := reader.Get(
				context.Background(),
				Identity{DID: "did:plc:alice", PDS: server.URL},
				Source{Collection: "app.bsky.actor.profile", RecordKey: "self"},
			)

			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			} else {
				require.NotErrorIs(t, err, ErrRecordNotFound)
			}
		})
	}
}

func TestRecordReaderRejectsNonHTTPSPDS(t *testing.T) {
	t.Parallel()

	reader := &atprotoRecordReader{httpClient: http.DefaultClient}
	_, err := reader.Get(
		context.Background(),
		Identity{DID: "did:plc:alice", PDS: "http://pds.example"},
		Source{Collection: "app.bsky.actor.profile", RecordKey: "self"},
	)
	require.ErrorIs(t, err, ErrNoPDS)
	require.False(t, errors.Is(err, ErrRecordNotFound))
}
