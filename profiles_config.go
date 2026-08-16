// Package accountinfo provides build-time account.info configuration.
package accountinfo

import _ "embed"

//go:embed profiles.json
var profilesJSON string

// ProfilesJSON returns the profile-source registry embedded in the binary.
func ProfilesJSON() string {
	return profilesJSON
}
