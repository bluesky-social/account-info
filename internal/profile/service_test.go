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
	source *Source,
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

func mustNewTestService(
	t testing.TB,
	resolver identityResolver,
	reader recordReader,
	sources ...Source,
) *Service {
	t.Helper()
	for index := range sources {
		if sources[index].Extract == nil && !sources[index].Selectors.configured() {
			sources[index].Extract = func(Identity, json.RawMessage) (Summary, error) {
				return Summary{}, nil
			}
		}
	}
	service, err := NewService(resolver, reader, sources...)
	require.NoError(t, err)
	return service
}

func extractTestJSONProfile(
	t testing.TB,
	account Identity,
	collection string,
	value json.RawMessage,
	selectors ProfileSelectors,
) (Summary, error) {
	t.Helper()
	compiled, err := selectors.compile()
	require.NoError(t, err)
	return extractJSONProfile(account, collection, value, compiled)
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
			Value: json.RawMessage(`{
				"$type":"org.example.profile",
				"profile":{
					"name":"Alice",
					"bio":"Builder",
					"createdAt":"2024-01-02T03:04:05Z"
				}
			}`),
		},
	}}
	service := mustNewTestService(t,
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
			Selectors: ProfileSelectors{
				DisplayName: "$.profile.name",
				Description: "$.profile.bio",
				Avatar:      "$.profile.avatar",
				CreatedAt:   "$.profile.createdAt",
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
	require.Equal(t, "Builder", account.Description)
	require.Len(t, account.Profiles, 1)
	require.Equal(t, "org.example.profile", account.Profiles[0].Collection)
	require.JSONEq(
		t,
		string(reader.records["org.example.profile"].Value),
		string(account.Profiles[0].Value),
	)
}

func TestServiceLookupSelectsOldestProfileAsDefault(t *testing.T) {
	t.Parallel()

	identity := Identity{
		DID: "did:plc:alice",
		PDS: "https://pds.example",
	}
	reader := &fakeReader{records: map[string]Record{
		"new.example.profile": {
			Collection: "new.example.profile",
			Value: json.RawMessage(`{
				"$type":"new.example.profile",
				"name":"New Alice",
				"created":"2025-01-02T03:04:05Z"
			}`),
		},
		"old.example.profile": {
			Collection: "old.example.profile",
			Value: json.RawMessage(`{
				"$type":"old.example.profile",
				"name":"Old Alice",
				"created":"2024-01-02T03:04:05Z"
			}`),
		},
	}}
	service := mustNewTestService(t,
		&fakeResolver{identity: identity},
		reader,
		Source{
			Collection: "new.example.profile",
			RecordKey:  "self",
			Selectors: ProfileSelectors{
				DisplayName: "$.name",
				Description: "$.bio",
				Avatar:      "$.avatar",
				CreatedAt:   "$.created",
			},
		},
		Source{
			Collection: "old.example.profile",
			RecordKey:  "self",
			Selectors: ProfileSelectors{
				DisplayName: "$.name",
				Description: "$.bio",
				Avatar:      "$.avatar",
				CreatedAt:   "$.created",
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
	service := mustNewTestService(t,
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

func TestServiceLookupIgnoresUnorderableProfilesWhenSelectingDefault(t *testing.T) {
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
			service := mustNewTestService(t,
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

			account, err := service.Lookup(context.Background(), "alice.example", nil)
			require.NoError(t, err)
			require.Equal(t, "valid.example.profile", account.Default)
			require.Len(t, account.Profiles, 2)
		})
	}
}

func TestServiceLookupLeavesDefaultUnsetWhenNoProfileAgeIsKnown(t *testing.T) {
	t.Parallel()

	service := mustNewTestService(t,
		&fakeResolver{identity: Identity{
			DID: "did:plc:alice",
			PDS: "https://pds.example",
		}},
		&fakeReader{records: map[string]Record{
			"app.example.profile": {Collection: "app.example.profile"},
			"org.example.profile": {Collection: "org.example.profile"},
		}},
		Source{Collection: "app.example.profile", RecordKey: "self"},
		Source{Collection: "org.example.profile", RecordKey: "self"},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Empty(t, account.Default)
	require.Len(t, account.Profiles, 2)
}

func TestServiceLookupToleratesLegacyTangledProfile(t *testing.T) {
	t.Parallel()

	const (
		bskyCollection    = "app.bsky.actor.profile"
		tangledCollection = "sh.tangled.actor.profile"
	)
	selectors := func(displayName, description, avatar, createdAt string) ProfileSelectors {
		return ProfileSelectors{
			DisplayName: displayName,
			Description: description,
			Avatar:      avatar,
			CreatedAt:   createdAt,
		}
	}
	service := mustNewTestService(t,
		&fakeResolver{identity: Identity{
			DID:    "did:plc:alice",
			Handle: "alice.example",
			PDS:    "https://pds.example",
		}},
		&fakeReader{records: map[string]Record{
			bskyCollection: {
				Collection: bskyCollection,
				Value: json.RawMessage(`{
					"$type":"app.bsky.actor.profile",
					"displayName":"Alice",
					"createdAt":"2024-01-02T03:04:05Z"
				}`),
			},
			tangledCollection: {
				Collection: tangledCollection,
				Value: json.RawMessage(
					`{"$type":"sh.tangled.actor.profile","bluesky":false}`,
				),
			},
		}},
		Source{
			Collection: bskyCollection,
			RecordKey:  "self",
			Selectors: selectors(
				"$.displayName", "$.description", "$.avatar", "$.createdAt",
			),
		},
		Source{
			Collection: tangledCollection,
			RecordKey:  "self",
			Selectors: selectors(
				"$.preferredHandle", "$.description", "$.avatar", "$.createdAt",
			),
		},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, bskyCollection, account.Default)
	require.Equal(t, "Alice", account.DisplayName)
	require.Len(t, account.Profiles, 2)
}

func TestServiceLookupUsesValidFieldsFromPartiallyInvalidProfile(t *testing.T) {
	t.Parallel()

	const collection = "app.example.profile"
	service := mustNewTestService(t,
		&fakeResolver{identity: Identity{
			DID: "did:plc:alice",
			PDS: "https://pds.example",
		}},
		&fakeReader{records: map[string]Record{
			collection: {
				Collection: collection,
				Value: json.RawMessage(`{
					"$type":"app.example.profile",
					"displayName":42,
					"description":"Builder",
					"createdAt":"2024-01-02T03:04:05Z"
				}`),
			},
		}},
		Source{
			Collection: collection,
			RecordKey:  "self",
			Selectors: ProfileSelectors{
				DisplayName: "$.displayName",
				Description: "$.description",
				Avatar:      "$.avatar",
				CreatedAt:   "$.createdAt",
			},
		},
	)

	account, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, collection, account.Default)
	require.Empty(t, account.DisplayName)
	require.Equal(t, "Builder", account.Description)
	require.Len(t, account.Profiles, 1)
}

func TestServiceLookupAllSkipsMissingRecords(t *testing.T) {
	t.Parallel()

	service := mustNewTestService(t,
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

func TestNewServiceRejectsDuplicateSources(t *testing.T) {
	t.Parallel()

	source := Source{
		Collection: "app.example.profile",
		RecordKey:  "self",
		Extract: func(Identity, json.RawMessage) (Summary, error) {
			return Summary{}, nil
		},
	}
	_, err := NewService(
		&fakeResolver{},
		&fakeReader{},
		source,
		source,
	)
	require.ErrorContains(t, err, "duplicate profile source")
}

func TestNewServiceRejectsInvalidProfileSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source Source
		want   string
	}{
		{
			name: "missing selectors",
			source: Source{
				Collection: "app.example.profile",
				RecordKey:  "self",
			},
			want: "profile selectors are not configured",
		},
		{
			name: "invalid selector",
			source: Source{
				Collection: "app.example.profile",
				RecordKey:  "self",
				Selectors: ProfileSelectors{
					DisplayName: "displayName",
					Description: "$.description",
					Avatar:      "$.avatar",
					CreatedAt:   "$.createdAt",
				},
			},
			want: "invalid displayName selector",
		},
		{
			name: "incomplete selectors",
			source: Source{
				Collection: "app.example.profile",
				RecordKey:  "self",
				Selectors: ProfileSelectors{
					DisplayName: "$.displayName",
					Description: "$.description",
					Avatar:      "$.avatar",
				},
			},
			want: "createdAt selector is empty",
		},
		{
			name: "non-singular selector",
			source: Source{
				Collection: "app.example.profile",
				RecordKey:  "self",
				Selectors: ProfileSelectors{
					DisplayName: "$..displayName",
					Description: "$.description",
					Avatar:      "$.avatar",
					CreatedAt:   "$.createdAt",
				},
			},
			want: "displayName selector must be singular",
		},
		{
			name: "conflicting extraction strategies",
			source: Source{
				Collection: "app.example.profile",
				RecordKey:  "self",
				Selectors: ProfileSelectors{
					DisplayName: "$.displayName",
					Description: "$.description",
					Avatar:      "$.avatar",
					CreatedAt:   "$.createdAt",
				},
				Extract: func(Identity, json.RawMessage) (Summary, error) {
					return Summary{}, nil
				},
			},
			want: "both selectors and custom extractor are configured",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewService(&fakeResolver{}, &fakeReader{}, test.source)
			require.ErrorContains(t, err, test.want)
		})
	}
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
			"ref":{"$link":"bafkreiaff2bptwyp4fg7o533pplheq4l3bxuiltnhkxgwabqyvt4achj6q"},
			"mimeType":"image/jpeg",
			"size":123
		}
	}`)
	summary, err := extractTestJSONProfile(t,
		Identity{
			DID: "did:plc:alice",
			PDS: "https://pds.example/",
		},
		"app.bsky.actor.profile",
		value,
		ProfileSelectors{
			DisplayName: "$.displayName",
			Description: "$.description",
			Avatar:      "$.avatar",
			CreatedAt:   "$.createdAt",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "Alice", summary.DisplayName)
	require.Equal(t, "Builder", summary.Description)
	require.Equal(t, "2024-01-02T03:04:05.123Z", summary.CreatedAt)
	require.Equal(
		t,
		"https://pds.example/xrpc/com.atproto.sync.getBlob"+
			"?cid=bafkreiaff2bptwyp4fg7o533pplheq4l3bxuiltnhkxgwabqyvt4achj6q"+
			"&did=did%3Aplc%3Aalice",
		summary.Avatar,
	)
	require.Equal(t, &BlobRef{
		CID:         "bafkreiaff2bptwyp4fg7o533pplheq4l3bxuiltnhkxgwabqyvt4achj6q",
		ContentType: "image/jpeg",
		Size:        123,
	}, summary.AvatarRef)
}

func TestExtractJSONProfileUsesConfiguredPointers(t *testing.T) {
	t.Parallel()

	value := json.RawMessage(`{
		"$type":"example.actor.profile",
		"profile":{
			"name":"Alice",
			"bio":"Builder",
			"created":"2024-01-02T03:04:05.123Z",
			"image":{
				"$type":"blob",
				"ref":{"$link":"bafkreiaff2bptwyp4fg7o533pplheq4l3bxuiltnhkxgwabqyvt4achj6q"},
				"mimeType":"image/jpeg",
				"size":898458
			}
		}
	}`)
	summary, err := extractTestJSONProfile(t,
		Identity{DID: "did:plc:alice", PDS: "https://pds.example"},
		"example.actor.profile",
		value,
		ProfileSelectors{
			DisplayName: "$.profile.name",
			Description: "$.profile.bio",
			Avatar:      "$.profile.image",
			CreatedAt:   "$.profile.created",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "Alice", summary.DisplayName)
	require.Equal(t, "Builder", summary.Description)
	require.Equal(t, "2024-01-02T03:04:05.123Z", summary.CreatedAt)
	require.Equal(t, &BlobRef{
		CID:         "bafkreiaff2bptwyp4fg7o533pplheq4l3bxuiltnhkxgwabqyvt4achj6q",
		ContentType: "image/jpeg",
		Size:        898458,
	}, summary.AvatarRef)
	require.Equal(
		t,
		"https://pds.example/xrpc/com.atproto.sync.getBlob"+
			"?cid=bafkreiaff2bptwyp4fg7o533pplheq4l3bxuiltnhkxgwabqyvt4achj6q"+
			"&did=did%3Aplc%3Aalice",
		summary.Avatar,
	)
}

func TestExtractJSONProfileRejectsMismatchedRecordType(t *testing.T) {
	t.Parallel()

	_, err := extractTestJSONProfile(t,
		Identity{},
		"example.actor.profile",
		json.RawMessage(`{"$type":"other.actor.profile"}`),
		ProfileSelectors{
			DisplayName: "$.displayName",
			Description: "$.description",
			Avatar:      "$.avatar",
			CreatedAt:   "$.createdAt",
		},
	)
	require.ErrorContains(t, err, "record type does not match collection")
}

func TestExtractJSONProfileAllowsAbsentOptionalValues(t *testing.T) {
	t.Parallel()

	summary, err := extractTestJSONProfile(t,
		Identity{},
		"example.actor.profile",
		json.RawMessage(`{"$type":"example.actor.profile"}`),
		ProfileSelectors{
			DisplayName: "$.displayName",
			Description: "$.description",
			Avatar:      "$.avatar",
			CreatedAt:   "$.createdAt",
		},
	)
	require.NoError(t, err)
	require.Equal(t, Summary{}, summary)
}

func TestExtractJSONProfileRejectsInvalidSelectedValue(t *testing.T) {
	t.Parallel()

	summary, err := extractTestJSONProfile(t,
		Identity{},
		"example.actor.profile",
		json.RawMessage(`{
			"$type":"example.actor.profile",
			"displayName":42,
			"description":"Builder",
			"createdAt":"2024-01-02T03:04:05Z"
		}`),
		ProfileSelectors{
			DisplayName: "$.displayName",
			Description: "$.description",
			Avatar:      "$.avatar",
			CreatedAt:   "$.createdAt",
		},
	)
	require.ErrorContains(t, err, "extract displayName")
	require.ErrorContains(t, err, "selected value is not a string")
	require.Equal(t, "Builder", summary.Description)
	require.Equal(t, "2024-01-02T03:04:05Z", summary.CreatedAt)
}

func TestExtractJSONProfilePreservesTextWhenAvatarIsInvalid(t *testing.T) {
	t.Parallel()

	summary, err := extractTestJSONProfile(t,
		Identity{},
		"example.actor.profile",
		json.RawMessage(`{
			"$type":"example.actor.profile",
			"displayName":"Alice",
			"description":"Builder",
			"avatar":"legacy avatar",
			"createdAt":"2024-01-02T03:04:05Z"
		}`),
		ProfileSelectors{
			DisplayName: "$.displayName",
			Description: "$.description",
			Avatar:      "$.avatar",
			CreatedAt:   "$.createdAt",
		},
	)
	require.ErrorContains(t, err, "decode avatar")
	require.Equal(t, "Alice", summary.DisplayName)
	require.Equal(t, "Builder", summary.Description)
	require.Nil(t, summary.AvatarRef)
}

func TestJSONPathSelectsQuotedPropertyNames(t *testing.T) {
	t.Parallel()

	summary, err := extractTestJSONProfile(t,
		Identity{},
		"example.actor.profile",
		json.RawMessage(`{
			"$type":"example.actor.profile",
			"a/b":{"~name":"value"}
		}`),
		ProfileSelectors{
			DisplayName: "$['a/b']['~name']",
			Description: "$.description",
			Avatar:      "$.avatar",
			CreatedAt:   "$.createdAt",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "value", summary.DisplayName)
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
	service := mustNewTestService(t,
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

	service := mustNewTestService(t,
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
			service := mustNewTestService(t,
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

			service := mustNewTestService(t,
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
	service := mustNewTestService(t,
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
