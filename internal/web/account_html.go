package web

import (
	"bytes"
	_ "embed"
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
	Handle            string
	Description       string
	MetaDescription   string
	CanonicalURL      string
	AvatarPath        string
	AvatarURL         string
	AvatarAlt         string
	AvatarContentType string
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

	page := accountPage{
		Styles:          template.CSS(stylesheet),
		Title:           title,
		Label:           label,
		Heading:         heading,
		Handle:          account.Handle,
		Description:     account.Description,
		MetaDescription: metaDescription,
		CanonicalURL:    publicOrigin + "/" + escapedLabel,
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
	return page, nil
}

func avatarFilename(contentType string) (string, error) {
	switch contentType {
	case "image/jpeg":
		return "profile.jpg", nil
	case "image/png":
		return "profile.png", nil
	case "image/webp":
		return "profile.webp", nil
	default:
		return "", fmt.Errorf("unsupported avatar content type %q", contentType)
	}
}
