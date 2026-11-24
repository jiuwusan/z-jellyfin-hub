package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// No host allowlist and no trust in Forwarded/X-Forwarded-* headers.
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	u, e := url.Parse(r.Header.Get("Origin"))
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && strings.EqualFold(u.Host, r.Host) && (r.TLS == nil || u.Scheme == "https")
}

func secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	u, e := url.Parse(r.Header.Get("Origin"))
	return e == nil && u.Scheme == "https" && sameOrigin(r)
}

// A reverse proxy may keep or strip the deployment prefix. The browser owns its base URL.
func routeRequest(r *http.Request) *http.Request {
	path := r.URL.Path
	for _, marker := range []string{"/api/v1/", "/assets/", "/health/"} {
		if at := strings.Index(path, marker); at >= 0 {
			path = path[at:]
			break
		}
	}
	if strings.HasSuffix(path, "/") && !strings.Contains(path, "/api/") {
		path = "/"
	}
	if strings.HasSuffix(path, "/index.html") {
		path = "/index.html"
	}
	if path == r.URL.Path {
		return r
	}
	next := r.Clone(r.Context())
	location := *r.URL
	location.Path = path
	location.RawPath = ""
	next.URL = &location
	return next
}
