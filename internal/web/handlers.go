package web

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
)

//go:embed index.html
var indexHTML string

func routes(accounts accountLookup) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /avatar/{identifier}", handleAvatar(accounts))
	mux.HandleFunc("GET /{identifier}", handleAccount(accounts))
	return mux
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set(
		"Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; "+
			"form-action 'none'; frame-ancestors 'none'",
	)
	_, _ = io.WriteString(w, indexHTML)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "ok")
}
