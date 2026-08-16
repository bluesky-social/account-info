package profile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProfileSources(t *testing.T) {
	t.Parallel()

	sources, err := parseProfileSources(`{
		"version":1,
		"profiles":[{
			"collection":"app.example.profile",
			"recordKey":"self",
			"selectors":{
				"displayName":"$.name",
				"description":"$.bio",
				"avatar":"$.image",
				"createdAt":"$.created"
			},
			"app":{
				"name":"Example",
				"iconURL":"https://app.example/icon.png",
				"profileURL":"https://app.example/profile/{identifier}"
			}
		}]
	}`)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	source := sources[0]
	require.Equal(t, "app.example.profile", source.Collection)
	require.Equal(t, "self", source.RecordKey)
	require.Equal(t, ProfileSelectors{
		DisplayName: "$.name",
		Description: "$.bio",
		Avatar:      "$.image",
		CreatedAt:   "$.created",
	}, source.Selectors)
	require.NotNil(t, source.App)
	require.Equal(t, "Example", source.App.Name)
	require.Equal(t, "https://app.example/icon.png", source.App.IconURL)
	profileURL, err := source.App.ProfileURL(Identity{Handle: "alice.example"})
	require.NoError(t, err)
	require.Equal(t, "https://app.example/profile/alice.example", profileURL)
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
