// CORS support for the web app (see web/), whose dev server runs on a
// different origin/port than this API. Same CORS_ALLOWED_ORIGINS
// convention as ../core and ../detect_boxes.
package main

import (
	"net/http"
	"os"
	"slices"
	"strings"
)

const defaultCorsAllowedOrigins = "*"

// corsAllowedOrigins reads CORS_ALLOWED_ORIGINS as a comma-separated list of
// origins allowed to call this API cross-origin (e.g. the web app's dev
// server origin). Defaults to allowing any origin.
func corsAllowedOrigins() []string {
	v := os.Getenv("CORS_ALLOWED_ORIGINS")
	if v == "" {
		v = defaultCorsAllowedOrigins
	}
	var origins []string
	for _, o := range strings.Split(v, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	return origins
}

func withCORS(next http.Handler, allowedOrigins []string) http.Handler {
	allowAll := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if slices.Contains(allowedOrigins, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
