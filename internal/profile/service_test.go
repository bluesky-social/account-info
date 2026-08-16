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
	records   map[string]Record
	errors    map[string]error
	avatar    Avatar
	avatarErr error
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

func (f *fakeReader) GetBlob(
	_ context.Context,
	_ Identity,
	_ BlobRef,
) (Avatar, error) {
	return f.avatar, f.avatarErr
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
		Source{
			Collection: "app.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{DisplayName: "App Alice"}, nil
			},
		},
		Source{
			Collection: "org.example.profile",
			RecordKey:  "self",
			App: &ProfileApp{
				Name: "Example App",
				Icon: "example",
				ProfileURL: func(identity Identity) (string, error) {
					return "https://app.example/profile/" + identity.Handle, nil
				},
			},
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
	require.Equal(t, "org.example.profile", account.Default)
	require.Equal(t, "Alice", account.DisplayName)
	require.Len(t, account.Profiles, 1)
	require.Equal(t, "org.example.profile", account.Profiles[0].Collection)
	require.Equal(t, &AppLink{
		Name: "Example App",
		Icon: "example",
		URL:  "https://app.example/profile/alice.example",
	}, account.Profiles[0].App)
}

func TestServiceLookupSelectsOldestProfileAsDefault(t *testing.T) {
	t.Parallel()

	identity := Identity{
		DID: "did:plc:alice",
		PDS: "https://pds.example",
	}
	reader := &fakeReader{records: map[string]Record{
		"new.example.profile": {Collection: "new.example.profile"},
		"old.example.profile": {Collection: "old.example.profile"},
	}}
	service := NewService(
		&fakeResolver{identity: identity},
		reader,
		Source{
			Collection: "new.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{
					DisplayName: "New Alice",
					CreatedAt:   "2025-01-02T03:04:05Z",
				}, nil
			},
		},
		Source{
			Collection: "old.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{
					DisplayName: "Old Alice",
					CreatedAt:   "2024-01-02T03:04:05Z",
				}, nil
			},
		},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, "old.example.profile", account.Default)
	require.Equal(t, "Old Alice", account.DisplayName)
	require.Len(t, account.Profiles, 2)
}

func TestServiceLookupBreaksCreatedAtTiesByCollection(t *testing.T) {
	t.Parallel()

	const createdAt = "2024-01-02T03:04:05Z"
	identity := Identity{DID: "did:plc:alice", PDS: "https://pds.example"}
	reader := &fakeReader{records: map[string]Record{
		"z.example.profile": {Collection: "z.example.profile"},
		"a.example.profile": {Collection: "a.example.profile"},
	}}
	service := NewService(
		&fakeResolver{identity: identity},
		reader,
		Source{
			Collection: "z.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{CreatedAt: createdAt}, nil
			},
		},
		Source{
			Collection: "a.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{CreatedAt: createdAt}, nil
			},
		},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, "a.example.profile", account.Default)
}

func TestServiceLookupRejectsUnorderableProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		createdAt string
	}{
		{name: "missing createdAt"},
		{name: "invalid createdAt", createdAt: "yesterday"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			identity := Identity{DID: "did:plc:alice", PDS: "https://pds.example"}
			reader := &fakeReader{records: map[string]Record{
				"valid.example.profile":   {Collection: "valid.example.profile"},
				"invalid.example.profile": {Collection: "invalid.example.profile"},
			}}
			service := NewService(
				&fakeResolver{identity: identity},
				reader,
				Source{
					Collection: "valid.example.profile",
					RecordKey:  "self",
					Extract: func(Identity, json.RawMessage) (Summary, error) {
						return Summary{CreatedAt: "2024-01-02T03:04:05Z"}, nil
					},
				},
				Source{
					Collection: "invalid.example.profile",
					RecordKey:  "self",
					Extract: func(Identity, json.RawMessage) (Summary, error) {
						return Summary{CreatedAt: test.createdAt}, nil
					},
				},
			)

			_, err := service.Lookup(context.Background(), "alice.example", nil)
			require.ErrorIs(t, err, ErrProfileCreatedAt)
			require.ErrorContains(t, err, "invalid.example.profile")
		})
	}
}

func TestServiceLookupRejectsInvalidAppLink(t *testing.T) {
	t.Parallel()

	service := NewService(
		&fakeResolver{identity: Identity{
			DID:    "did:plc:alice",
			Handle: "alice.example",
			PDS:    "https://pds.example",
		}},
		&fakeReader{records: map[string]Record{
			"app.example.profile": {Collection: "app.example.profile"},
		}},
		Source{
			Collection: "app.example.profile",
			RecordKey:  "self",
			App: &ProfileApp{
				Name: "Unsafe App",
				Icon: "unsafe",
				ProfileURL: func(Identity) (string, error) {
					return "javascript:alert(1)", nil
				},
			},
		},
	)

	_, err := service.Lookup(context.Background(), "alice.example", nil)
	require.ErrorContains(t, err, "invalid app profile URL")
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
		Source{Collection: "app.example.profile", RecordKey: "self"},
		Source{Collection: "org.example.profile", RecordKey: "self"},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, "app.example.profile", account.Default)
	require.Len(t, account.Profiles, 1)
	require.Equal(t, "app.example.profile", account.Profiles[0].Collection)
}

func TestNewServiceDeduplicatesSources(t *testing.T) {
	t.Parallel()

	service := NewService(
		&fakeResolver{},
		&fakeReader{},
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
		"createdAt":"2024-01-02T03:04:05.123Z",
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
	require.Equal(t, "2024-01-02T03:04:05.123Z", summary.CreatedAt)
	require.Equal(
		t,
		"https://pds.example/xrpc/com.atproto.sync.getBlob"+
			"?cid=bafycid&did=did%3Aplc%3Aalice",
		summary.Avatar,
	)
	require.Equal(t, &BlobRef{
		CID:         "bafycid",
		ContentType: "image/jpeg",
		Size:        123,
	}, summary.AvatarRef)
}

func TestServiceAvatar(t *testing.T) {
	t.Parallel()

	want := Avatar{Content: []byte("image"), ContentType: "image/jpeg", CID: "bafycid"}
	reader := &fakeReader{
		records: map[string]Record{
			"app.example.profile": {
				Collection: "app.example.profile",
				Value:      json.RawMessage(`{"avatar":true}`),
			},
		},
		avatar: want,
	}
	service := NewService(
		&fakeResolver{identity: Identity{DID: "did:plc:alice", PDS: "https://pds.example"}},
		reader,
		Source{
			Collection: "app.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{AvatarRef: &BlobRef{
					CID:         "bafycid",
					ContentType: "image/jpeg",
					Size:        5,
				}}, nil
			},
		},
	)

	got, err := service.Avatar(context.Background(), "alice.example")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestServiceLookupExposesAvatarContentType(t *testing.T) {
	t.Parallel()

	service := NewService(
		&fakeResolver{identity: Identity{
			DID:    "did:plc:alice",
			Handle: "alice.example",
			PDS:    "https://pds.example",
		}},
		&fakeReader{records: map[string]Record{
			"app.example.profile": {
				Collection: "app.example.profile",
				Value:      json.RawMessage(`{"avatar":true}`),
			},
		}},
		Source{
			Collection: "app.example.profile",
			RecordKey:  "self",
			Extract: func(Identity, json.RawMessage) (Summary, error) {
				return Summary{
					Avatar: "https://pds.example/avatar",
					AvatarRef: &BlobRef{
						CID:         "bafycid",
						ContentType: "image/png",
						Size:        123,
					},
				}, nil
			},
		},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, "image/png", account.AvatarContentType)
}

func TestServiceAvatarErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records map[string]Record
		extract func(Identity, json.RawMessage) (Summary, error)
		want    error
	}{
		{name: "profile missing", records: map[string]Record{}, want: ErrProfileNotFound},
		{
			name:    "avatar missing",
			records: map[string]Record{"app.example.profile": {Collection: "app.example.profile"}},
			extract: func(Identity, json.RawMessage) (Summary, error) { return Summary{}, nil },
			want:    ErrAvatarNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(
				&fakeResolver{identity: Identity{DID: "did:plc:alice", PDS: "https://pds.example"}},
				&fakeReader{records: test.records},
				Source{Collection: "app.example.profile", RecordKey: "self", Extract: test.extract},
			)
			_, err := service.Avatar(context.Background(), "alice.example")
			require.ErrorIs(t, err, test.want)
		})
	}
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
		Source{Collection: "app.example.profile", RecordKey: "self"},
	)

	_, err := service.Lookup(context.Background(), "alice.example", nil)
	require.ErrorIs(t, err, upstreamErr)
}
