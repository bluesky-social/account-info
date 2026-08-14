package web

import (
	"fmt"
	"net/http"
)

func routes(accounts accountLookup) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /avatar/{identifier}", handleAvatar(accounts))
	mux.HandleFunc("GET /{identifier}", handleAccount(accounts))
	return mux
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "account.info")
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "ok")
}
