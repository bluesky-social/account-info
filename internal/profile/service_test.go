package profile

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeResolver struct {
	identity Identity
	err      error
}

func (f *fakeResolver) Resolve(
	_ context.Context,
	_ string,
) (Identity, error) {
	return f.identity, f.err
}

type fakeReader struct {
	records map[string]Record
	errors  map[string]error
}

func (f *fakeReader) Get(
	_ context.Context,
	_ Identity,
	source Source,
) (Record, error) {
	if err := f.errors[source.Collection]; err != nil {
		return Record{}, err
	}
	record, ok := f.records[source.Collection]
	if !ok {
		return Record{}, ErrRecordNotFound
	}
	return record, nil
}

func TestServiceLookup(t *testing.T) {
	t.Parallel()

	identity := Identity{
		DID:    "did:plc:alice",
		Handle: "alice.example",
		PDS:    "https://pds.example",
	}
	reader := &fakeReader{records: map[string]Record{
		"app.example.profile": {
			Collection: "app.example.profile",
			URI:        "at://did:plc:alice/app.example.profile/self",
			Value:      json.RawMessage(`{"name":"Alice"}`),
		},
		"org.example.profile": {
			Collection: "org.example.profile",
			URI:        "at://did:plc:alice/org.example.profile/self",
			Value:      json.RawMessage(`{"title":"Alice"}`),
		},
	}}
	service := NewService(
		&fakeResolver{identity: identity},
		reader,
		"app.example.profile",
		Source{
			Collection: "app.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{DisplayName: "Authoritative Alice"}, nil
			},
		},
		Source{
			Collection: "org.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{DisplayName: "Alice"}, nil
			},
		},
	)

	account, err := service.Lookup(
		context.Background(),
		"alice.example",
		[]string{"org.example.profile"},
	)
	require.NoError(t, err)
	require.Equal(t, identity.DID, account.DID)
	require.Equal(t, identity.Handle, account.Handle)
	require.Equal(t, identity.PDS, account.PDS)
	require.Empty(t, account.Authoritative)
	require.Equal(t, "Alice", account.DisplayName)
	require.Len(t, account.Profiles, 1)
	require.Equal(t, "org.example.profile", account.Profiles[0].Collection)
}

func TestServiceLookupAllSkipsMissingRecords(t *testing.T) {
	t.Parallel()

	service := NewService(
		&fakeResolver{identity: Identity{
			DID: "did:plc:alice",
			PDS: "https://pds.example",
		}},
		&fakeReader{records: map[string]Record{
			"app.example.profile": {Collection: "app.example.profile"},
		}},
		"app.example.profile",
		Source{Collection: "app.example.profile", RecordKey: "self"},
		Source{Collection: "org.example.profile", RecordKey: "self"},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, "app.example.profile", account.Authoritative)
	require.Len(t, account.Profiles, 1)
	require.Equal(t, "app.example.profile", account.Profiles[0].Collection)
}

func TestNewServiceDeduplicatesSources(t *testing.T) {
	t.Parallel()

	service := NewService(
		&fakeResolver{},
		&fakeReader{},
		"app.example.profile",
		Source{Collection: "app.example.profile", RecordKey: "self"},
		Source{Collection: "app.example.profile", RecordKey: "self"},
		Source{Collection: "org.example.profile", RecordKey: "self"},
	)

	require.Equal(
		t,
		[]string{"app.example.profile", "org.example.profile"},
		service.Collections(),
	)
}

func TestBlueskyProfileSummary(t *testing.T) {
	t.Parallel()

	value := json.RawMessage(`{
		"$type":"app.bsky.actor.profile",
		"displayName":"Alice",
		"description":"Builder",
		"avatar":{
			"$type":"blob",
			"ref":{"$link":"bafycid"},
			"mimeType":"image/jpeg",
			"size":123
		}
	}`)
	summary, err := extractBlueskyProfile(Identity{
		DID: "did:plc:alice",
		PDS: "https://pds.example/",
	}, value)
	require.NoError(t, err)
	require.Equal(t, "Alice", summary.DisplayName)
	require.Equal(t, "Builder", summary.Description)
	require.Equal(
		t,
		"https://pds.example/xrpc/com.atproto.sync.getBlob"+
			"?cid=bafycid&did=did%3Aplc%3Aalice",
		summary.Avatar,
	)
}

func TestBlobURLRejectsInvalidPDS(t *testing.T) {
	t.Parallel()

	_, err := blobURL("javascript:alert(1)", "did:plc:alice", "bafycid")
	require.Error(t, err)
}

func TestServiceLookupErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		identity    Identity
		collections []string
		want        error
	}{
		{
			name:     "no PDS",
			identity: Identity{DID: "did:plc:alice"},
			want:     ErrNoPDS,
		},
		{
			name: "unsupported collection",
			identity: Identity{
				DID: "did:plc:alice",
				PDS: "https://pds.example",
			},
			collections: []string{"bad.example.profile"},
			want:        ErrUnsupportedCollection,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(
				&fakeResolver{identity: test.identity},
				&fakeReader{},
				"app.example.profile",
				Source{Collection: "app.example.profile", RecordKey: "self"},
			)
			_, err := service.Lookup(
				context.Background(),
				"alice.example",
				test.collections,
			)
			require.ErrorIs(t, err, test.want)
		})
	}
}

func TestServiceLookupPropagatesReaderError(t *testing.T) {
	t.Parallel()

	upstreamErr := errors.New("upstream failure")
	service := NewService(
		&fakeResolver{identity: Identity{
			DID: "did:plc:alice",
			PDS: "https://pds.example",
		}},
		&fakeReader{errors: map[string]error{
			"app.example.profile": upstreamErr,
		}},
		"app.example.profile",
		Source{Collection: "app.example.profile", RecordKey: "self"},
	)

	_, err := service.Lookup(context.Background(), "alice.example", nil)
	require.ErrorIs(t, err, upstreamErr)
}
