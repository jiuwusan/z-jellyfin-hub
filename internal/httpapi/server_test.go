package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminOriginCSRFAndSubscriptionETagRevocation(t *testing.T) {
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
	svc := service.New(db)
	handler := New(svc, Options{})
	call := func(method, path, body string, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://hub.example.com"+path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	loginJSON, _ := json.Marshal(map[string]string{"username": "fixture", "password": password})
	if w := call("POST", "/api/v1/admin/login", string(loginJSON), nil, "", "https://evil.example"); w.Code != 403 {
		t.Fatal("foreign origin login accepted", w.Code)
	}
	login := call("POST", "/api/v1/admin/login", string(loginJSON), nil, "", "https://hub.example.com")
	if login.Code != 200 {
		t.Fatal("login failed", login.Code)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("insecure cookie")
	}
	var auth map[string]string
	json.Unmarshal(login.Body.Bytes(), &auth)
	entity := `{"label":"Proxy","data":{"type":"http","host":"proxy.example.com","port":8080,"username":"","password":""}}`
	if w := call("POST", "/api/v1/admin/entities/proxy", entity, cookies[0], "", "https://hub.example.com"); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if w := call("POST", "/api/v1/admin/entities/proxy", entity, cookies[0], auth["csrf"], "https://hub.example.com"); w.Code != 200 {
		t.Fatal("valid write failed", w.Code, w.Body.String())
	}
	if w := call("GET", "/api/v1/admin/subscriptions", "", nil, "", ""); w.Code != 401 {
		t.Fatal("unauthenticated admin read accepted")
	}
	proxy, _ := svc.SaveEntity("proxy", "", "Proxy", []byte(`{"type":"http","host":"proxy.example.com","port":8080,"username":"","password":""}`))
	server, _ := svc.SaveEntity("server", "", "Server", []byte(`{"baseUrl":"http://media.internal:8096/jellyfin"}`))
	cred, _ := svc.SaveEntity("credential", "", "Credential", []byte(`{"serverId":"`+server.ID+`","username":"fixture","password":"fixture-only"}`))
	policy, _ := svc.SaveEntity("policy", "", "Policy", []byte(`{"resolution":"1080p","totalBitrateBps":4000000}`))
	sub, link, e := svc.SaveSubscription(storage.Subscription{Label: "Fixture", ProxyID: proxy.ID, CredentialID: cred.ID, PolicyID: policy.ID})
	if e != nil {
		t.Fatal(e)
	}
	preview, _ := svc.Preview(sub.ID)
	svc.Publish(sub.ID, 0, preview.DraftDigest, "")
	get := func(tag, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/subscription/config", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Client-Version-Code", "57")
		r.Header.Set("If-None-Match", tag)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	token := link[len(link)-43:]
	first := get("", token)
	if first.Code != 200 || !bytes.Contains(first.Body.Bytes(), []byte("fixture-only")) {
		t.Fatal("config missing", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" || get(etag, token).Code != 304 {
		t.Fatal("conditional delivery failed")
	}
	svc.SetEnabled(sub.ID, false)
	if get(etag, token).Code != 410 {
		t.Fatal("304 bypassed disabled state")
	}
	svc.SetEnabled(sub.ID, true)
	svc.Rotate(sub.ID)
	if get(etag, token).Code != 401 {
		t.Fatal("304 bypassed token revocation")
	}
	before, _ := db.Metadata("admin_password")
	bad, _ := json.Marshal(map[string]string{"oldPassword": password, "newPassword": "Test!23"})
	if call("POST", "/api/v1/admin/password", string(bad), cookies[0], auth["csrf"], "https://hub.example.com").Code != 400 {
		t.Fatal("short administrator password accepted")
	}
	after, _ := db.Metadata("admin_password")
	if after != before {
		t.Fatal("invalid password change modified administrator")
	}
	good, _ := json.Marshal(map[string]string{"oldPassword": password, "newPassword": "Test!234"})
	if w := call("POST", "/api/v1/admin/password", string(good), cookies[0], auth["csrf"], "https://hub.example.com"); w.Code != 200 {
		t.Fatal("eight-character password change failed", w.Code)
	}
	if call("GET", "/api/v1/admin/session", "", cookies[0], "", "").Code != 401 {
		t.Fatal("password change kept old session")
	}
	updated, _ := json.Marshal(map[string]string{"username": "fixture", "password": "Test!234"})
	if call("POST", "/api/v1/admin/login", string(updated), nil, "", "https://hub.example.com").Code != 200 {
		t.Fatal("new password login failed")
	}

}
