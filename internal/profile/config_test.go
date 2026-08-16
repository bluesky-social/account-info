package profile

import (
	"testing"

	accountinfo "github.com/bluesky-social/account-info"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedProfileSources(t *testing.T) {
	t.Parallel()

	sources, err := parseProfileSources(accountinfo.ProfilesJSON())
	require.NoError(t, err)
	require.Len(t, sources, 1)

	source := sources[0]
	require.Equal(t, "app.bsky.actor.profile", source.Collection)
	require.Equal(t, "self", source.RecordKey)
	require.Equal(t, ProfileSelectors{
		DisplayName: "$.displayName",
		Description: "$.description",
		Avatar:      "$.avatar",
		CreatedAt:   "$.createdAt",
	}, source.Selectors)
	require.NotNil(t, source.App)
	require.Equal(t, "Bluesky", source.App.Name)
	require.Equal(t, "bluesky", source.App.Icon)

	tests := []struct {
		name     string
		identity Identity
		want     string
	}{
		{
			name: "handle",
			identity: Identity{
				DID:    "did:plc:alice",
				Handle: "alice.example",
			},
			want: "https://bsky.app/profile/alice.example",
		},
		{
			name:     "DID fallback",
			identity: Identity{DID: "did:plc:alice"},
			want:     "https://bsky.app/profile/did:plc:alice",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profileURL, err := source.App.ProfileURL(test.identity)
			require.NoError(t, err)
			require.Equal(t, test.want, profileURL)
		})
	}
}

func TestParseProfileSourcesRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "unknown field",
			config: `{"version":1,"profiles":[],"typo":true}`,
			want:   `unknown field "typo"`,
		},
		{
			name:   "unsupported version",
			config: `{"version":2,"profiles":[]}`,
			want:   "unsupported profile configuration version 2",
		},
		{
			name:   "no profiles",
			config: `{"version":1,"profiles":[]}`,
			want:   "profile configuration is empty",
		},
		{
			name: "invalid collection",
			config: `{
				"version":1,
				"profiles":[{
					"collection":"not-an-nsid",
					"recordKey":"self",
					"selectors":{
						"displayName":"$.displayName",
						"description":"$.description",
						"avatar":"$.avatar",
						"createdAt":"$.createdAt"
					}
				}]
			}`,
			want: "invalid collection",
		},
		{
			name: "unsafe app URL",
			config: `{
				"version":1,
				"profiles":[{
					"collection":"app.example.profile",
					"recordKey":"self",
					"selectors":{
						"displayName":"$.displayName",
						"description":"$.description",
						"avatar":"$.avatar",
						"createdAt":"$.createdAt"
					},
					"app":{
						"name":"Example",
						"icon":"example",
						"profileURL":"javascript:alert({identifier})"
					}
				}]
			}`,
			want: "profileURL must be an HTTPS URL",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseProfileSources(test.config)
			require.ErrorContains(t, err, test.want)
		})
	}
}
