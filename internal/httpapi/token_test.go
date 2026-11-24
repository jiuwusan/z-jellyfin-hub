package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
)

func TestTokenBindingAndRebuiltService(t *testing.T) {
	token, _ := secret.Token()
	var oldInstance, oldID, oldETag string
	for generation := 0; generation < 2; generation++ {
		key := make([]byte, 32)
		rand.Read(key)
		box, _ := secret.NewBox(key)
		db, err := storage.Open(filepath.Join(t.TempDir(), "hub.db"), box)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		password, _ := secret.Token()
		hash, _ := secret.HashPassword(password)
		db.SetMetadata("admin_username", "fixture")
		db.SetMetadata("admin_password", hash)
		svc := service.New(db)
		handler := New(svc, Options{})
		call := func(method, path string, body any, cookie *http.Cookie, csrf, bearer, etag string) *httptest.ResponseRecorder {
			raw, _ := json.Marshal(body)
			r := httptest.NewRequest(method, "https://hub.example"+path, strings.NewReader(string(raw)))
			r.Header.Set("Origin", "https://hub.example")
			r.Header.Set("X-CSRF-Token", csrf)
			r.Header.Set("X-Client-Version-Code", "57")
			r.Header.Set("Authorization", "Bearer "+bearer)
			r.Header.Set("If-None-Match", etag)
			if cookie != nil {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			return w
		}
		login := call("POST", "/api/v1/admin/login", map[string]string{"username": "fixture", "password": password}, nil, "", "", "")
		if login.Code != 200 {
			t.Fatal("fixture login failed")
		}
		cookie := login.Result().Cookies()[0]
		var auth map[string]string
		json.Unmarshal(login.Body.Bytes(), &auth)
		proxy, _ := svc.SaveEntity("proxy", "", "Proxy", []byte(`{"type":"http","host":"proxy.example.com","port":8080,"username":"","password":""}`))
		server, _ := svc.SaveEntity("server", "", "Server", []byte(`{"baseUrl":"http://media.internal:8096"}`))
		credential, _ := svc.SaveEntity("credential", "", "Account", []byte(`{"serverId":"`+server.ID+`","username":"fixture","password":"fixture-only"}`))
		policy, _ := svc.SaveEntity("policy", "", "Policy", []byte(`{"resolution":"1080p","totalBitrateBps":4000000}`))
		draft := map[string]any{"label": "Fixture", "proxyId": proxy.ID, "credentialId": credential.ID, "policyId": policy.ID, "token": token}
		saved := call("POST", "/api/v1/admin/subscriptions", draft, cookie, auth["csrf"], "", "")
		if saved.Code != 200 {
			t.Fatalf("manual token rejected: %d", saved.Code)
		}
		var created struct {
			Subscription storage.Subscription `json:"subscription"`
			Link         string               `json:"link"`
		}
		json.Unmarshal(saved.Body.Bytes(), &created)
		if created.Link != "api/v1/subscription/config#"+token {
			t.Fatal("link contains an ID or changed token")
		}
		malformed := map[string]any{"label": "Malformed", "proxyId": proxy.ID, "credentialId": credential.ID, "policyId": policy.ID, "token": token + "\n"}
		if call("POST", "/api/v1/admin/subscriptions", malformed, cookie, auth["csrf"], "", "").Code != 400 {
			t.Fatal("noncanonical new token accepted")
		}
		duplicate := call("POST", "/api/v1/admin/subscriptions", draft, cookie, auth["csrf"], "", "")
		if duplicate.Code != 409 {
			t.Fatal("duplicate token accepted", duplicate.Code)
		}
		preview, _ := svc.Preview(created.Subscription.ID)
		if _, err = svc.Publish(created.Subscription.ID, 0, preview.DraftDigest, ""); err != nil {
			t.Fatal(err)
		}
		config := call("GET", "/api/v1/subscription/config", nil, nil, "", token, oldETag)
		if config.Code != 200 {
			t.Fatal("restored link returned stale 304 or failed", config.Code)
		}
		var body struct {
			Schema   int    `json:"schemaVersion"`
			Instance string `json:"serverInstanceId"`
			ID       string `json:"subscriptionId"`
			Revision int64  `json:"revision"`
		}
		json.Unmarshal(config.Body.Bytes(), &body)
		if body.Schema != 2 || len(body.Instance) != 32 || body.Revision != 1 {
			t.Fatal("invalid instance/version response")
		}
		if generation == 1 && (body.Instance == oldInstance || body.ID == oldID || config.Header().Get("ETag") == oldETag) {
			t.Fatal("rebuilt database kept old identity/ETag")
		}
		oldInstance, oldID, oldETag = body.Instance, body.ID, config.Header().Get("ETag")
		if call("GET", "/api/v1/subscription/config", nil, nil, "", token, oldETag).Code != 304 {
			t.Fatal("valid ETag not reused")
		}
		rawList := call("GET", "/api/v1/admin/subscriptions", nil, cookie, "", "", "").Body.String()
		if strings.Contains(rawList, token) {
			t.Fatal("token leaked into subscription list")
		}
		for _, bad := range []string{"short", token[:10] + "\r\n" + token[10:]} {
			invalid := call("POST", "/api/v1/admin/subscriptions/"+body.ID+"/token", map[string]string{"token": bad}, cookie, auth["csrf"], "", "")
			if invalid.Code != 400 {
				t.Fatal("invalid token accepted", invalid.Code)
			}
		}
		if call("GET", "/api/v1/subscription/config", nil, nil, "", token, oldETag).Code != 304 {
			t.Fatal("failed token change destroyed binding")
		}
		otherToken, _ := secret.Token()
		secondDraft := map[string]any{"label": "Second", "proxyId": proxy.ID, "credentialId": credential.ID, "policyId": policy.ID, "token": otherToken}
		if call("POST", "/api/v1/admin/subscriptions", secondDraft, cookie, auth["csrf"], "", "").Code != 200 {
			t.Fatal("second token fixture failed")
		}
		conflict := call("POST", "/api/v1/admin/subscriptions/"+body.ID+"/token", map[string]string{"token": otherToken}, cookie, auth["csrf"], "", "")
		if conflict.Code != 409 || call("GET", "/api/v1/subscription/config", nil, nil, "", token, oldETag).Code != 304 {
			t.Fatal("duplicate token update changed binding")
		}
		report := map[string]any{"deviceId": "fixture-device-12345", "platform": "android", "versionCode": 57, "revision": 1, "status": "applied", "errorCode": ""}
		if call("POST", "/api/v1/subscription/feedback", report, nil, "", token, "").Code != 200 {
			t.Fatal("token-only feedback failed")
		}
		next, _ := secret.Token()
		changed := call("POST", "/api/v1/admin/subscriptions/"+body.ID+"/token", map[string]string{"token": next}, cookie, auth["csrf"], "", "")
		if changed.Code != 200 {
			t.Fatal("manual token change failed", changed.Code)
		}
		if call("GET", "/api/v1/subscription/config", nil, nil, "", token, oldETag).Code != 401 {
			t.Fatal("old token remained valid")
		}
		svc.SetEnabled(body.ID, false)
		if call("GET", "/api/v1/subscription/config", nil, nil, "", next, oldETag).Code != 410 {
			t.Fatal("disabled token bypassed ETag")
		}
	}
}
