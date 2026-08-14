package profile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcalabro/atmos/cbor"
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

func TestRecordReaderGetsVerifiedAvatarBlob(t *testing.T) {
	t.Parallel()

	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	cid := cbor.ComputeCID(cbor.CodecRaw, content).String()
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(w http.ResponseWriter, request *http.Request) {
			require.Equal(t, "/xrpc/com.atproto.sync.getBlob", request.URL.Path)
			require.Equal(t, "did:plc:alice", request.URL.Query().Get("did"))
			require.Equal(t, cid, request.URL.Query().Get("cid"))
			require.Equal(t, "image/png, image/jpeg", request.Header.Get("Accept"))
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		},
	))
	defer server.Close()

	reader := &atprotoRecordReader{httpClient: server.Client()}
	avatar, err := reader.GetBlob(
		context.Background(),
		Identity{DID: "did:plc:alice", PDS: server.URL},
		BlobRef{CID: cid, ContentType: "image/png", Size: int64(len(content))},
	)
	require.NoError(t, err)
	require.Equal(t, content, avatar.Content)
	require.Equal(t, "image/png", avatar.ContentType)
	require.Equal(t, cid, avatar.CID)
}

func TestRecordReaderRejectsInvalidAvatarBlob(t *testing.T) {
	t.Parallel()

	pngA := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
	pngB := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x01}

	tests := []struct {
		name    string
		content []byte
		ref     BlobRef
	}{
		{
			name:    "body exceeds declared size",
			content: pngA,
			ref: BlobRef{
				CID:         cbor.ComputeCID(cbor.CodecRaw, pngA[:8]).String(),
				ContentType: "image/png",
				Size:        8,
			},
		},
		{
			name:    "content type mismatch",
			content: pngA,
			ref: BlobRef{
				CID:         cbor.ComputeCID(cbor.CodecRaw, pngA).String(),
				ContentType: "image/jpeg",
				Size:        int64(len(pngA)),
			},
		},
		{
			name:    "CID mismatch",
			content: pngA,
			ref: BlobRef{
				CID:         cbor.ComputeCID(cbor.CodecRaw, pngB).String(),
				ContentType: "image/png",
				Size:        int64(len(pngA)),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write(test.content)
				},
			))
			defer server.Close()

			reader := &atprotoRecordReader{httpClient: server.Client()}
			_, err := reader.GetBlob(
				context.Background(),
				Identity{DID: "did:plc:alice", PDS: server.URL},
				test.ref,
			)
			require.Error(t, err)
		})
	}
}

func TestValidateAvatarRef(t *testing.T) {
	t.Parallel()

	validCID := cbor.ComputeCID(cbor.CodecRaw, []byte("avatar")).String()
	dagCBORCID := cbor.ComputeCID(cbor.CodecDagCBOR, []byte("avatar")).String()
	tests := []struct {
		name string
		ref  BlobRef
	}{
		{name: "empty size", ref: BlobRef{CID: validCID, ContentType: "image/png"}},
		{name: "oversize", ref: BlobRef{CID: validCID, ContentType: "image/png", Size: maxAvatarSize + 1}},
		{name: "unsupported type", ref: BlobRef{CID: validCID, ContentType: "image/svg+xml", Size: 1}},
		{name: "invalid CID", ref: BlobRef{CID: "not-a-cid", ContentType: "image/png", Size: 1}},
		{name: "non-raw CID", ref: BlobRef{CID: dagCBORCID, ContentType: "image/png", Size: 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateAvatarRef(test.ref)
			require.Error(t, err)
		})
	}
}
