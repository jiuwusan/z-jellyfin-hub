package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
)

func TestFirstRunSetupRequiresCodeAndCannotReplaceAdministrator(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	box, _ := secret.NewBox(key)
	db, e := storage.Open(filepath.Join(t.TempDir(), "hub.db"), box)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	code, _ := secret.Token()
	hash, _ := secret.Digest(code)
	db.SetMetadata("setup_code_hash", hex.EncodeToString(hash))
	handler := New(service.New(db), Options{})
	call := func(path, origin, token string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"code": token, "username": "fixture-admin", "password": "Fixture-password-42"})
		method := "POST"
		if path == "status" {
			method = "GET"
		}
		r := httptest.NewRequest(method, "http://192.0.2.5:8080/hub/api/v1/setup/"+path, strings.NewReader(string(body)))
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("status", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"required":true`) {
		t.Fatal("first-run status missing", w.Code)
	}
	if call("complete", "https://evil.example", code).Code != 403 {
		t.Fatal("foreign setup accepted")
	}
	if call("complete", "http://192.0.2.5:8080", "wrong").Code != 401 {
		t.Fatal("invalid setup code accepted")
	}
	if w := call("complete", "http://192.0.2.5:8080", code); w.Code != 200 {
		t.Fatal("valid setup rejected", w.Code)
	}
	if call("complete", "http://192.0.2.5:8080", code).Code != 409 {
		t.Fatal("reinitialization allowed")
	}
	if !strings.Contains(call("status", "", "").Body.String(), `"required":false`) {
		t.Fatal("setup remained available")
	}
	stored, _ := db.Metadata("admin_password")
	if !secret.VerifyPassword(stored, "Fixture-password-42") {
		t.Fatal("password not hashed correctly")
	}
}
