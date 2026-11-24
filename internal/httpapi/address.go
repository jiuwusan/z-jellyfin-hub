package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// Origin is only a browser scheme hint for cookies behind TLS termination, not
// an authorization check. Host rewrites and absent Origin must not block access.
// Forwarded headers are not trusted. Without a valid Origin, an existing Secure
// cookie preserves HTTPS cookie attributes when logging out or changing password.
func secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	u, e := url.Parse(r.Header.Get("Origin"))
	if e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Path == "" && !u.ForceQuery && u.RawQuery == "" && u.Fragment == "" {
		return u.Scheme == "https"
	}
	cookie, err := r.Cookie(httpsSessionCookie)
	return err == nil && cookie.Value != ""
}

// A reverse proxy may keep or strip the deployment prefix. The browser owns its base URL.
func routeRequest(r *http.Request) *http.Request {
	path := r.URL.Path
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasSuffix(path, "/") {
		path = "/"
	} else if strings.HasSuffix(path, "/index.html") {
		path = "/index.html"
	} else {
		start := -1
		for _, marker := range []string{"/api/v1/", "/assets/", "/health/"} {
			if at := strings.LastIndex(path, marker); at > start {
				start = at
			}
		}
		if start >= 0 {
			path = path[start:]
		}
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
