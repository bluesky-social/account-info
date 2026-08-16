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

	grain := requireProfileSource(t, sources, "social.grain.actor.profile")
	require.Equal(t, "social.grain.actor.profile", grain.Collection)
	require.Equal(t, "self", grain.RecordKey)
	require.Equal(t, ProfileSelectors{
		DisplayName: "$.displayName",
		Description: "$.description",
		Avatar:      "$.avatar",
		CreatedAt:   "$.createdAt",
	}, grain.Selectors)
	require.NotNil(t, grain.App)
	require.Equal(t, "Grain", grain.App.Name)
	require.Equal(t, "https://grain.social/icon-192.png", grain.App.IconURL)
	profileURL, err = grain.App.ProfileURL(Identity{Handle: "joebasser.com"})
	require.NoError(t, err)
	require.Equal(t, "https://grain.social/profile/joebasser.com", profileURL)

	spark := requireProfileSource(t, sources, "so.sprk.actor.profile")
	require.Equal(t, "so.sprk.actor.profile", spark.Collection)
	require.Equal(t, "self", spark.RecordKey)
	require.Equal(t, ProfileSelectors{
		DisplayName: "$.displayName",
		Description: "$.description",
		Avatar:      "$.avatar",
		CreatedAt:   "$.createdAt",
	}, spark.Selectors)
	require.NotNil(t, spark.App)
	require.Equal(t, "Spark", spark.App.Name)
	require.Equal(t, "https://sprk.so/meta/apple-touch-icon.png", spark.App.IconURL)
	profileURL, err = spark.App.ProfileURL(Identity{Handle: "joebasser.com"})
	require.NoError(t, err)
	require.Equal(t, "https://sprk.so/profile/joebasser.com", profileURL)

	sifa := requireProfileSource(t, sources, "id.sifa.profile.self")
	require.Equal(t, "id.sifa.profile.self", sifa.Collection)
	require.Equal(t, "self", sifa.RecordKey)
	require.Equal(t, ProfileSelectors{
		DisplayName: "$.displayName",
		Description: "$.about",
		Avatar:      "$.avatar",
		CreatedAt:   "$.createdAt",
	}, sifa.Selectors)
	require.NotNil(t, sifa.App)
	require.Equal(t, "Sifa ID", sifa.App.Name)
	require.Equal(t, "https://sifa.id/apple-icon.png", sifa.App.IconURL)
	profileURL, err = sifa.App.ProfileURL(Identity{Handle: "knotbin.com"})
	require.NoError(t, err)
	require.Equal(t, "https://sifa.id/p/knotbin.com", profileURL)
}

func TestTangledProfileExtraction(t *testing.T) {
	t.Parallel()

	sources, err := parseProfileSources(accountinfo.ProfilesJSON())
	require.NoError(t, err)
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

func TestGrainProfileExtraction(t *testing.T) {
	t.Parallel()

	sources, err := parseProfileSources(accountinfo.ProfilesJSON())
	require.NoError(t, err)
	grain := requireProfileSource(t, sources, "social.grain.actor.profile")
	value := []byte(`{
		"$type":"social.grain.actor.profile",
		"avatar":{
			"ref":{"$link":"bafkreicf4fsbv5eb4vyfxha55m5gnz6pp4ccfocsucyc2gvzeazpwfxhkm"},
			"size":993453,
			"$type":"blob",
			"mimeType":"image/jpeg"
		},
		"createdAt":"2026-04-07T15:04:15.903Z",
		"displayName":"Joe Basser"
	}`)

	summary, err := extractJSONProfile(
		Identity{
			DID: "did:plc:qed67d2sst5xqsbuveiv7fjp",
			PDS: "https://suillus.us-west.host.bsky.network",
		},
		grain.Collection,
		value,
		grain.compiledSelectors,
	)
	require.NoError(t, err)
	require.Equal(t, "Joe Basser", summary.DisplayName)
	require.Empty(t, summary.Description)
	require.Equal(t, "2026-04-07T15:04:15.903Z", summary.CreatedAt)
	require.Equal(t, &BlobRef{
		CID:         "bafkreicf4fsbv5eb4vyfxha55m5gnz6pp4ccfocsucyc2gvzeazpwfxhkm",
		ContentType: "image/jpeg",
		Size:        993453,
	}, summary.AvatarRef)
	require.Equal(
		t,
		"https://suillus.us-west.host.bsky.network/xrpc/com.atproto.sync.getBlob?cid=bafkreicf4fsbv5eb4vyfxha55m5gnz6pp4ccfocsucyc2gvzeazpwfxhkm&did=did%3Aplc%3Aqed67d2sst5xqsbuveiv7fjp",
		summary.Avatar,
	)
}

func TestSparkProfileExtraction(t *testing.T) {
	t.Parallel()

	sources, err := parseProfileSources(accountinfo.ProfilesJSON())
	require.NoError(t, err)
	spark := requireProfileSource(t, sources, "so.sprk.actor.profile")
	value := []byte(`{
		"$type":"so.sprk.actor.profile",
		"avatar":{
			"ref":{"$link":"bafkreichh65megb3qxl5rxghb7ruxfw4tunnjymyaapl5vubmulxxuaxby"},
			"size":191243,
			"$type":"blob",
			"mimeType":"image/jpeg"
		},
		"description":"Building this app",
		"displayName":"Joe Basser"
	}`)

	summary, err := extractJSONProfile(
		Identity{
			DID: "did:plc:qed67d2sst5xqsbuveiv7fjp",
			PDS: "https://suillus.us-west.host.bsky.network",
		},
		spark.Collection,
		value,
		spark.compiledSelectors,
	)
	require.NoError(t, err)
	require.Equal(t, "Joe Basser", summary.DisplayName)
	require.Equal(t, "Building this app", summary.Description)
	require.Empty(t, summary.CreatedAt)
	require.Equal(t, &BlobRef{
		CID:         "bafkreichh65megb3qxl5rxghb7ruxfw4tunnjymyaapl5vubmulxxuaxby",
		ContentType: "image/jpeg",
		Size:        191243,
	}, summary.AvatarRef)
	require.Equal(
		t,
		"https://suillus.us-west.host.bsky.network/xrpc/com.atproto.sync.getBlob?cid=bafkreichh65megb3qxl5rxghb7ruxfw4tunnjymyaapl5vubmulxxuaxby&did=did%3Aplc%3Aqed67d2sst5xqsbuveiv7fjp",
		summary.Avatar,
	)
}

func TestSifaProfileExtraction(t *testing.T) {
	t.Parallel()

	sources, err := parseProfileSources(accountinfo.ProfilesJSON())
	require.NoError(t, err)
	sifa := requireProfileSource(t, sources, "id.sifa.profile.self")
	value := []byte(`{
		"$type":"id.sifa.profile.self",
		"about":"16 year old software engineer. Co-founder and CTO of Spark Social.",
		"avatar":{
			"ref":{"$link":"bafkreicnui2gniokq72ukyolei3tfkp2lw6hvisfoj6fwqpebhu5knp35y"},
			"size":18745,
			"$type":"blob",
			"mimeType":"image/jpeg"
		},
		"openTo":[],
		"headline":"AT Protocol full-stack engineer",
		"createdAt":"2026-03-22T18:47:21.326Z",
		"preferredWorkplace":[]
	}`)

	summary, err := extractJSONProfile(
		Identity{
			DID: "did:plc:6hbqm2oftpotwuw7gvvrui3i",
			PDS: "https://knotbin.xyz",
		},
		sifa.Collection,
		value,
		sifa.compiledSelectors,
	)
	require.NoError(t, err)
	require.Empty(t, summary.DisplayName)
	require.Equal(
		t,
		"16 year old software engineer. Co-founder and CTO of Spark Social.",
		summary.Description,
	)
	require.Equal(t, "2026-03-22T18:47:21.326Z", summary.CreatedAt)
	require.Equal(t, &BlobRef{
		CID:         "bafkreicnui2gniokq72ukyolei3tfkp2lw6hvisfoj6fwqpebhu5knp35y",
		ContentType: "image/jpeg",
		Size:        18745,
	}, summary.AvatarRef)
	require.Equal(
		t,
		"https://knotbin.xyz/xrpc/com.atproto.sync.getBlob?cid=bafkreicnui2gniokq72ukyolei3tfkp2lw6hvisfoj6fwqpebhu5knp35y&did=did%3Aplc%3A6hbqm2oftpotwuw7gvvrui3i",
		summary.Avatar,
	)
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
