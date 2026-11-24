package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
)

func TestAnyAccessAddressWithSessionAndCSRF(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	box, _ := secret.NewBox(key)
	db, e := storage.Open(filepath.Join(t.TempDir(), "hub.db"), box)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	password, _ := secret.Token()
	hash, _ := secret.HashPassword(password)
	db.SetMetadata("admin_username", "fixture")
	db.SetMetadata("admin_password", hash)
	body, _ := json.Marshal(map[string]string{"username": "fixture", "password": password})
	names := map[bool]string{}
	for _, origin := range []string{"http://nas.example:18080", "https://other.example:9443", "http://192.0.2.5:8080", "https://[2001:db8::5]:9443", "https://new.example"} {
		t.Run(origin, func(t *testing.T) {
			handler := New(service.New(db), Options{})
			r := httptest.NewRequest("POST", origin+"/hub/api/v1/admin/login", strings.NewReader(string(body)))
			r.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("address rejected: %d", w.Code)
			}
			if w.Header().Get("Strict-Transport-Security") != "" {
				t.Fatal("HSTS unexpectedly disables HTTP access on other ports")
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Secure != strings.HasPrefix(origin, "https:") {
				t.Fatal("cookie scheme mismatch")
			}
			names[cookies[0].Secure] = cookies[0].Name
			var session map[string]string
			json.Unmarshal(w.Body.Bytes(), &session)
			write := func(from, csrf string, cookie *http.Cookie) int {
				request := httptest.NewRequest("POST", origin+"/hub/api/v1/admin/entities/proxy", strings.NewReader(`{"label":"Fixture","data":{"type":"http","host":"proxy.example","port":8080}}`))
				request.Header.Set("Origin", from)
				request.Header.Set("Sec-Fetch-Site", "cross-site")
				request.Header.Set("X-CSRF-Token", csrf)
				request.Header.Set("X-Forwarded-Host", "evil.example")
				if cookie != nil {
					request.AddCookie(cookie)
				}
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, request)
				return result.Code
			}
			if write(origin, "", cookies[0]) != 403 || write("", "wrong", cookies[0]) != 403 {
				t.Fatal("missing or invalid CSRF accepted")
			}
			if write(origin, session["csrf"], nil) != 401 {
				t.Fatal("missing session accepted")
			}
			for _, from := range []string{origin, "https://other.example:9443", "", "null"} {
				if got := write(from, session["csrf"], cookies[0]); got != 200 {
					t.Fatalf("valid session/CSRF rejected with Origin %q: %d", from, got)
				}
			}
		})
	}
	if names[true] == names[false] {
		t.Fatal("HTTP login would overwrite existing Secure cookie")
	}
}

func TestArbitraryNestedDeploymentPrefixes(t *testing.T) {
	for _, prefix := range []string{"", "/custom-console", "/tools/video/control", "/api/v1/portal", "/assets/portal", "/health/tools"} {
		for _, endpoint := range []string{"/api/v1/setup/status", "/assets/chunk.js", "/health/ready"} {
			r := httptest.NewRequest("GET", "http://proxy.example"+prefix+endpoint, nil)
			if got := routeRequest(r).URL.Path; got != endpoint {
				t.Errorf("prefix %s endpoint %s became %s", prefix, endpoint, got)
			}
		}
		r := httptest.NewRequest("GET", "http://proxy.example"+prefix+"/", nil)
		if got := routeRequest(r).URL.Path; got != "/" {
			t.Errorf("landing prefix %s became %s", prefix, got)
		}
	}
}

func TestSecureCookieDetectionWithoutOriginRestriction(t *testing.T) {
	for _, tc := range []struct {
		name, target, origin string
		secureCookie, want   bool
	}{
		{"direct TLS without Origin", "https://hub.example/", "", false, true},
		{"HTTP without Origin", "http://nas.example/", "", false, false},
		{"TLS termination drops port", "http://hub.example/", "https://hub.example:9443", false, true},
		{"TLS termination rewrites host", "http://hub.internal/", "https://public.example:9443", false, true},
		{"HTTP Origin", "http://nas.example/", "http://nas.example:8097", false, false},
		{"existing HTTPS session without Origin", "http://hub.internal/", "", true, true},
		{"explicit HTTP takes precedence", "http://hub.internal/", "http://nas.example:8097", true, false},
		{"TLS takes precedence", "https://hub.example/", "http://other.example", false, true},
		{"null Origin", "http://hub.internal/", "null", false, false},
		{"invalid Origin", "http://hub.internal/", "://", false, false},
		{"Origin with path", "http://hub.internal/", "https://public.example/path", false, false},
		{"Origin with user", "http://hub.internal/", "https://user@public.example", false, false},
		{"Origin with query", "http://hub.internal/", "https://public.example?x=1", false, false},
		{"Origin with fragment", "http://hub.internal/", "https://public.example#x", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.target, nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			r.Header.Set("X-Forwarded-Proto", "https")
			if tc.secureCookie {
				r.AddCookie(&http.Cookie{Name: httpsSessionCookie, Value: "fixture"})
			}
			if got := secureRequest(r); got != tc.want {
				t.Fatalf("Secure = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionsThroughTLSProxyWithRewrittenHost(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "hub.db"), box)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	password, err := secret.Token()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := secret.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetMetadata("admin_username", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err = db.SetMetadata("admin_password", hash); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(New(service.New(db), Options{}))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = "hub.internal" // Models proxy_set_header Host $host losing the public port.
		r.Header.Del("X-Forwarded-Host")
		r.Header.Del("X-Forwarded-Proto")
	}
	frontend := httptest.NewTLSServer(proxy)
	defer frontend.Close()
	client := frontend.Client()
	client.Jar, _ = cookiejar.New(nil)
	defer client.CloseIdleConnections()
	loginBody, _ := json.Marshal(map[string]string{"username": "fixture", "password": password})
	call := func(base, path, origin, csrf, body string) (int, []*http.Cookie, map[string]string) {
		t.Helper()
		method := "POST"
		if path == "session" {
			method = "GET"
		}
		r, err := http.NewRequest(method, base+"/api/v1/admin/"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var data map[string]string
		if path == "login" || path == "session" {
			_ = json.NewDecoder(res.Body).Decode(&data)
		}
		return res.StatusCode, res.Cookies(), data
	}
	assertCookie := func(cookies []*http.Cookie, secure bool, deleted bool) {
		t.Helper()
		name := httpSessionCookie
		if secure {
			name = httpsSessionCookie
		}
		for _, cookie := range cookies {
			if cookie.Name == name {
				if cookie.Secure != secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || (cookie.MaxAge < 0) != deleted {
					t.Fatal("incorrect session cookie attributes")
				}
				return
			}
		}
		t.Fatal("expected session cookie missing")
	}
	status, cookies, session := call(frontend.URL, "login", frontend.URL, "", string(loginBody))
	if status != 200 {
		t.Fatalf("proxied HTTPS login: %d", status)
	}
	assertCookie(cookies, true, false)
	if status, _, _ = call(frontend.URL, "session", "", "", ""); status != 200 {
		t.Fatalf("HTTPS session read: %d", status)
	}
	// Same hostname over plain HTTP must get its own independent cookie.
	status, cookies, _ = call(backend.URL, "login", backend.URL, "", string(loginBody))
	if status != 200 {
		t.Fatalf("HTTP login: %d", status)
	}
	assertCookie(cookies, false, false)
	if status, _, _ = call(frontend.URL, "logout", "", "wrong", "{}"); status != 403 {
		t.Fatalf("invalid CSRF accepted: %d", status)
	}
	status, cookies, _ = call(frontend.URL, "logout", "", session["csrf"], "{}")
	if status != 200 {
		t.Fatalf("logout without Origin: %d", status)
	}
	assertCookie(cookies, true, true)
	assertCookie(cookies, false, true)
	if status, _, _ = call(frontend.URL, "session", "", "", ""); status != 401 {
		t.Fatalf("session survived logout: %d", status)
	}
	status, _, session = call(frontend.URL, "login", frontend.URL, "", string(loginBody))
	if status != 200 {
		t.Fatalf("second HTTPS login: %d", status)
	}
	body, _ := json.Marshal(map[string]string{"oldPassword": password, "newPassword": "FixtureNew!742"})
	status, cookies, _ = call(frontend.URL, "password", "", session["csrf"], string(body))
	if status != 200 {
		t.Fatalf("password change without Origin: %d", status)
	}
	assertCookie(cookies, true, true)
	if status, _, _ = call(frontend.URL, "session", "", "", ""); status != 401 {
		t.Fatalf("session survived password change: %d", status)
	}
}
