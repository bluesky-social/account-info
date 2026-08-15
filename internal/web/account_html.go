package web

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/account-info/internal/profile"
)

//go:embed account.html
var accountHTML string

var accountTemplate = template.Must(template.New("account.html").Parse(accountHTML))

const publicOrigin = "https://account.info"

type accountPage struct {
	Styles            template.CSS
	Title             string
	Label             string
	Heading           string
	DID               string
	Handle            string
	PDS               string
	Description       string
	MetaDescription   string
	CanonicalURL      string
	AvatarPath        string
	AvatarURL         string
	AvatarAlt         string
	AvatarContentType string
	HasDefault        bool
	Profiles          []accountPageProfile
}

type accountPageProfile struct {
	Collection string
	URI        string
	CID        string
	Value      string
	Default    bool
	AppName    string
	AppURL     string
	AppIcon    string
	AppLabel   string
}

func writeAccountHTML(w http.ResponseWriter, account *profile.Account) {
	page, err := newAccountPage(account)
	if err != nil {
		slog.Error("build account page", "error", err)
		writeError(
			w,
			http.StatusInternalServerError,
			"internal_error",
			"failed to encode response",
			nil,
		)
		return
	}

	var body bytes.Buffer
	if err := accountTemplate.Execute(&body, page); err != nil {
		slog.Error("render account page", "error", err)
		writeError(
			w,
			http.StatusInternalServerError,
			"internal_error",
			"failed to encode response",
			nil,
		)
		return
	}

	setHTMLHeaders(w)
	if _, err := body.WriteTo(w); err != nil {
		slog.Error("write account page", "error", err)
	}
}

func newAccountPage(account *profile.Account) (accountPage, error) {
	label := account.Handle
	if label == "" {
		label = account.DID
	}
	heading := account.DisplayName
	if heading == "" {
		heading = label
	}
	title := heading
	if account.DisplayName != "" && account.Handle != "" {
		title += " (@" + account.Handle + ")"
	}
	metaDescription := account.Description
	if metaDescription == "" {
		metaDescription = "AT Protocol profile information for " + label + "."
	}
	escapedLabel := url.PathEscape(label)

	defaultCollection := account.Authoritative
	if defaultCollection == "" && len(account.Profiles) == 1 {
		defaultCollection = account.Profiles[0].Collection
	}
	page := accountPage{
		Styles:          template.CSS(stylesheet),
		Title:           title,
		Label:           label,
		Heading:         heading,
		DID:             account.DID,
		Handle:          account.Handle,
		PDS:             account.PDS,
		Description:     account.Description,
		MetaDescription: metaDescription,
		CanonicalURL:    publicOrigin + "/" + escapedLabel,
		Profiles:        make([]accountPageProfile, 0, len(account.Profiles)),
	}
	if account.Avatar != "" {
		filename, err := avatarFilename(account.AvatarContentType)
		if err != nil {
			return accountPage{}, err
		}
		page.AvatarPath = "/avatar/" + escapedLabel + "/" + filename
		page.AvatarURL = publicOrigin + page.AvatarPath
		page.AvatarAlt = heading + " avatar"
		page.AvatarContentType = account.AvatarContentType
	}

	seen := make(map[string]struct{}, len(account.Profiles))
	for _, record := range account.Profiles {
		if record.Collection == "" {
			return accountPage{}, fmt.Errorf("profile collection is empty")
		}
		if _, exists := seen[record.Collection]; exists {
			return accountPage{}, fmt.Errorf(
				"duplicate profile collection: %s",
				record.Collection,
			)
		}
		seen[record.Collection] = struct{}{}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, record.Value, "", "  "); err != nil {
			return accountPage{}, fmt.Errorf(
				"format %s profile: %w",
				record.Collection,
				err,
			)
		}
		isDefault := record.Collection == defaultCollection
		page.HasDefault = page.HasDefault || isDefault
		pageProfile := accountPageProfile{
			Collection: record.Collection,
			URI:        record.URI,
			CID:        record.CID,
			Value:      pretty.String(),
			Default:    isDefault,
		}
		if record.App != nil {
			pageProfile.AppName = record.App.Name
			pageProfile.AppURL = record.App.URL
			pageProfile.AppIcon = appIconPath(record.App.Icon)
			pageProfile.AppLabel = appLinkLabel(
				account.Handle,
				account.DID,
				record.App.Name,
			)
		}
		page.Profiles = append(page.Profiles, pageProfile)
	}
	if defaultCollection != "" && !page.HasDefault {
		return accountPage{}, fmt.Errorf(
			"default profile collection is missing: %s",
			defaultCollection,
		)
	}
	return page, nil
}

func avatarFilename(contentType string) (string, error) {
	switch contentType {
	case "image/jpeg":
		return "profile.jpg", nil
	case "image/png":
		return "profile.png", nil
	default:
		return "", fmt.Errorf("unsupported avatar content type %q", contentType)
	}
}

func appIconPath(icon string) string {
	if icon == "" {
		return ""
	}
	return "/assets/apps/" + url.PathEscape(icon) + ".svg"
}

func appLinkLabel(handle, did, app string) string {
	if app == "" {
		return ""
	}
	identifier := did
	if handle != "" {
		identifier = "@" + handle
	}
	return "Open " + identifier + " on " + app
}
