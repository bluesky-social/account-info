package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const profileConfigVersion = 1

type profileConfig struct {
	Version  int                   `json:"version"`
	Profiles []profileSourceConfig `json:"profiles"`
}

type profileSourceConfig struct {
	Collection string           `json:"collection"`
	RecordKey  string           `json:"recordKey"`
	Selectors  ProfileSelectors `json:"selectors"`
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
