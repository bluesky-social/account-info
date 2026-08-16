package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

const profileConfigVersion = 1

const profileIdentifierPlaceholder = "{identifier}"

type profileConfig struct {
	Version  int                   `json:"version"`
	Profiles []profileSourceConfig `json:"profiles"`
}

type profileSourceConfig struct {
	Collection string            `json:"collection"`
	RecordKey  string            `json:"recordKey"`
	Selectors  ProfileSelectors  `json:"selectors"`
	App        *profileAppConfig `json:"app,omitempty"`
}

type profileAppConfig struct {
	Name       string `json:"name"`
	IconURL    string `json:"iconURL"`
	ProfileURL string `json:"profileURL"`
}

func parseProfileSources(raw string) ([]Source, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var config profileConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode profile configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode profile configuration: multiple JSON values")
		}
		return nil, fmt.Errorf("decode profile configuration: %w", err)
	}
	if config.Version != profileConfigVersion {
		return nil, fmt.Errorf(
			"unsupported profile configuration version %d",
			config.Version,
		)
	}
	if len(config.Profiles) == 0 {
		return nil, fmt.Errorf("profile configuration is empty")
	}

	sources := make([]Source, 0, len(config.Profiles))
	seen := make(map[string]struct{}, len(config.Profiles))
	for index, configured := range config.Profiles {
		source := Source{
			Collection: configured.Collection,
			RecordKey:  configured.RecordKey,
			Selectors:  configured.Selectors,
		}
		if configured.App != nil {
			app, err := newProfileApp(*configured.App)
			if err != nil {
				return nil, fmt.Errorf("profile %d app: %w", index, err)
			}
			source.App = app
		}
		if err := source.validate(); err != nil {
			return nil, fmt.Errorf("profile %d: %w", index, err)
		}
		if _, exists := seen[source.Collection]; exists {
			return nil, fmt.Errorf(
				"profile %d: duplicate profile source %q",
				index,
				source.Collection,
			)
		}
		seen[source.Collection] = struct{}{}
		sources = append(sources, source)
	}
	return sources, nil
}

func newProfileApp(config profileAppConfig) (*ProfileApp, error) {
	if config.Name == "" {
		return nil, fmt.Errorf("name is empty")
	}
	iconURL, err := parseRemoteIconURL(config.IconURL)
	if err != nil {
		return nil, fmt.Errorf("iconURL %w", err)
	}
	template, err := url.Parse(config.ProfileURL)
	if err != nil || template.Scheme != "https" || template.Hostname() == "" ||
		template.User != nil {
		return nil, fmt.Errorf("profileURL must be an HTTPS URL")
	}
	if strings.Count(config.ProfileURL, profileIdentifierPlaceholder) != 1 ||
		strings.Count(template.Path, profileIdentifierPlaceholder) != 1 {
		return nil, fmt.Errorf(
			"profileURL must contain exactly one %s in its path",
			profileIdentifierPlaceholder,
		)
	}
	if strings.ContainsAny(
		strings.Replace(config.ProfileURL, profileIdentifierPlaceholder, "", 1),
		"{}",
	) {
		return nil, fmt.Errorf("profileURL contains an unsupported placeholder")
	}

	return &ProfileApp{
		Name:    config.Name,
		IconURL: iconURL.String(),
		ProfileURL: func(identity Identity) (string, error) {
			identifier := identity.Handle
			if identifier == "" {
				identifier = identity.DID
			}
			if identifier == "" {
				return "", fmt.Errorf("identity has no handle or DID")
			}
			profileURL := *template
			profileURL.Path = strings.Replace(
				profileURL.Path,
				profileIdentifierPlaceholder,
				identifier,
				1,
			)
			profileURL.RawPath = ""
			return profileURL.String(), nil
		},
	}, nil
}
