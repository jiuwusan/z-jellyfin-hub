package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
)

func TestAnyAccessAddressWithSameOriginAndCSRF(t *testing.T) {
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
			write := func(from, csrf string) int {
				request := httptest.NewRequest("POST", origin+"/hub/api/v1/admin/logout", strings.NewReader("{}"))
				request.Header.Set("Origin", from)
				request.Header.Set("X-CSRF-Token", csrf)
				request.Header.Set("X-Forwarded-Host", "evil.example")
				request.AddCookie(cookies[0])
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, request)
				return result.Code
			}
			if write("https://evil.example", session["csrf"]) != 403 || write(origin, "") != 403 || write("", session["csrf"]) != 403 {
				t.Fatal("cross-site or missing CSRF accepted")
			}
			if write(origin, session["csrf"]) != 200 {
				t.Fatal("valid same-origin write rejected")
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
