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

	source := requireProfileSource(t, sources, "app.bsky.actor.profile")
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
	require.Equal(
		t,
		"https://web-cdn.bsky.app/static/apple-touch-icon.png",
		source.App.IconURL,
	)
	profileURL, err := source.App.ProfileURL(Identity{Handle: "alice.example"})
	require.NoError(t, err)
	require.Equal(t, "https://bsky.app/profile/alice.example", profileURL)

	tangled := requireProfileSource(t, sources, "sh.tangled.actor.profile")
	require.Equal(t, "sh.tangled.actor.profile", tangled.Collection)
	require.Equal(t, "self", tangled.RecordKey)
	require.Equal(t, ProfileSelectors{
		DisplayName: "$.preferredHandle",
		Description: "$.description",
		Avatar:      "$.avatar",
		CreatedAt:   "$.createdAt",
	}, tangled.Selectors)
	require.NotNil(t, tangled.App)
	require.Equal(t, "Tangled", tangled.App.Name)
	require.Equal(t, "https://tangled.org/static/logos/dolly.svg", tangled.App.IconURL)
	profileURL, err = tangled.App.ProfileURL(Identity{Handle: "anirudh.fi"})
	require.NoError(t, err)
	require.Equal(t, "https://tangled.org/anirudh.fi", profileURL)
}

func TestTangledProfileExtraction(t *testing.T) {
	t.Parallel()

	sources, err := parseProfileSources(accountinfo.ProfilesJSON())
	require.NoError(t, err)
	require.Len(t, sources, 2)
	tangled := requireProfileSource(t, sources, "sh.tangled.actor.profile")
	value := []byte(`{
		"$type":"sh.tangled.actor.profile",
		"links":["https://anirudh.fi"],
		"stats":["",""],
		"avatar":{
			"ref":{"$link":"bafkreienfd6ne74ns5trr3cbwtkt6niyxg6zg75xwwajswsml3o2evjrbq"},
			"size":678939,
			"$type":"blob",
			"mimeType":"image/png"
		},
		"bluesky":true,
		"location":"Helsinki",
		"pronouns":"he/him",
		"createdAt":"2026-07-07T17:01:14.085767Z",
		"description":"co-founder/ceo of this thing",
		"pinnedRepositories":["did:plc:j5hmlfdrwkvtxm7cjmu7j2is"]
	}`)

	summary, err := extractJSONProfile(
		Identity{
			DID: "did:plc:hwevmowznbiukdf6uk5dwrrq",
			PDS: "https://eurosky.social",
		},
		tangled.Collection,
		value,
		tangled.compiledSelectors,
	)
	require.NoError(t, err)
	require.Empty(t, summary.DisplayName)
	require.Equal(t, "co-founder/ceo of this thing", summary.Description)
	require.Equal(t, "2026-07-07T17:01:14.085767Z", summary.CreatedAt)
	require.Equal(t, &BlobRef{
		CID:         "bafkreienfd6ne74ns5trr3cbwtkt6niyxg6zg75xwwajswsml3o2evjrbq",
		ContentType: "image/png",
		Size:        678939,
	}, summary.AvatarRef)
}

func TestParseRemoteIconURL(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://app.example/icon.svg",
		"https://app.example/icon.PNG?version=1",
		"https://app.example/icon.jpg",
		"https://app.example/icon.jpeg",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			parsed, err := parseRemoteIconURL(raw)
			require.NoError(t, err)
			require.Equal(t, raw, parsed.String())
		})
	}

	for _, raw := range []string{
		"http://app.example/icon.png",
		"https://app.example/icon.gif",
		"https://user@app.example/icon.png",
		"https://app.example/icon.png#fragment",
		"https://:443/icon.png",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			_, err := parseRemoteIconURL(raw)
			require.ErrorContains(t, err, "must be an HTTPS SVG, PNG, or JPEG URL")
		})
	}
}

func TestNewProfileAppRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config profileAppConfig
		want   string
	}{
		{
			name: "insecure icon URL",
			config: profileAppConfig{
				Name:       "Example",
				IconURL:    "http://app.example/icon.png",
				ProfileURL: "https://app.example/{identifier}",
			},
			want: "iconURL must be an HTTPS SVG, PNG, or JPEG URL",
		},
		{
			name: "insecure profile URL",
			config: profileAppConfig{
				Name:       "Example",
				IconURL:    "https://app.example/icon.png",
				ProfileURL: "javascript:alert({identifier})",
			},
			want: "profileURL must be an HTTPS URL",
		},
		{
			name: "placeholder outside path",
			config: profileAppConfig{
				Name:       "Example",
				IconURL:    "https://app.example/icon.png",
				ProfileURL: "https://app.example/profile?id={identifier}",
			},
			want: "profileURL must contain exactly one {identifier} in its path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := newProfileApp(test.config)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func requireProfileSource(
	t testing.TB,
	sources []Source,
	collection string,
) Source {
	t.Helper()
	for _, source := range sources {
		if source.Collection == collection {
			return source
		}
	}
	require.FailNow(t, "profile source not found", collection)
	return Source{}
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseProfileSources(test.config)
			require.ErrorContains(t, err, test.want)
		})
	}
}
