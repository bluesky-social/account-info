package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

//go:embed index.html
var indexHTML string

//go:embed style.css
var stylesheet string

//go:embed assets/*.svg assets/apps/*.svg
var staticAssets embed.FS

var indexTemplate = template.Must(template.New("index.html").Parse(indexHTML))

type indexPage struct {
	Styles      template.CSS
	Identifier  string
	LookupError string
}

func routes(accounts accountLookup) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	mux.HandleFunc("GET /lookup", handleLookup)
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /assets/favicon.svg", handleFavicon)
	mux.HandleFunc("GET /assets/apps/{file}", handleAppIcon)
	mux.HandleFunc("GET /avatar/{identifier}", handleAvatar(accounts))
	mux.HandleFunc("GET /{identifier}", handleAccount(accounts))
	return mux
}

func handleAppIcon(w http.ResponseWriter, request *http.Request) {
	icon, ok := strings.CutSuffix(request.PathValue("file"), ".svg")
	if !ok || !validAssetName(icon) {
		http.NotFound(w, request)
		return
	}
	writeSVGAsset(w, request, "assets/apps/"+icon+".svg")
}

func handleFavicon(w http.ResponseWriter, request *http.Request) {
	writeSVGAsset(w, request, "assets/favicon.svg")
}

func writeSVGAsset(w http.ResponseWriter, request *http.Request, path string) {
	content, err := staticAssets.ReadFile(path)
	if err != nil {
		http.NotFound(w, request)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}

func validAssetName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789-", character) {
			return false
		}
	}
	return true
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeIndexHTML(w, http.StatusOK, indexPage{})
}

func writeIndexHTML(w http.ResponseWriter, status int, page indexPage) {
	var body bytes.Buffer
	page.Styles = template.CSS(stylesheet)
	if err := indexTemplate.Execute(&body, page); err != nil {
		slog.Error("render index page", "error", err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
		return
	}
	if status == http.StatusOK {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	setHTMLHeaders(w)
	w.WriteHeader(status)
	if _, err := body.WriteTo(w); err != nil {
		slog.Error("write index page", "error", err)
	}
}

func handleLookup(w http.ResponseWriter, request *http.Request) {
	identifier := request.URL.Query().Get("identifier")
	if identifier == "" {
		http.Error(w, "identifier is required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(
		w,
		request,
		"/"+url.PathEscape(identifier),
		http.StatusSeeOther,
	)
}

func setHTMLHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set(
		"Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; base-uri 'none'; "+
			"form-action 'self'; frame-ancestors 'none'",
	)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "ok")
}
