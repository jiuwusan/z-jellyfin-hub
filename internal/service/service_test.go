package service

import (
	"crypto/rand"
	"encoding/json"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) (*Service, storage.Subscription, string) {
	t.Helper()
	key := make([]byte, 32)
	rand.Read(key)
	box, _ := secret.NewBox(key)
	db, e := storage.Open(filepath.Join(t.TempDir(), "hub.db"), box)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db, "https://hub.example.com")
	proxy, _ := s.SaveEntity("proxy", "", "Proxy", json.RawMessage(`{"type":"http","host":"proxy.example.com","port":8080,"username":"","password":""}`))
	server, _ := s.SaveEntity("server", "", "Server", json.RawMessage(`{"baseUrl":"http://media.internal:8096/jellyfin"}`))
	credential, e := s.SaveEntity("credential", "", "Credential", json.RawMessage(`{"serverId":"`+server.ID+`","username":"fixture","password":"fixture-only-value"}`))
	if e != nil {
		t.Fatal(e)
	}
	policy, _ := s.SaveEntity("policy", "", "Policy", json.RawMessage(`{"resolution":"1080p","totalBitrateBps":4000000}`))
	sub, link, e := s.SaveSubscription(storage.Subscription{Label: "Fixture", ProxyID: proxy.ID, CredentialID: credential.ID, PolicyID: policy.ID})
	if e != nil {
		t.Fatal(e)
	}
	return s, sub, link
}
func TestDraftPublishRollbackAndRevocation(t *testing.T) {
	s, sub, link := fixture(t)
	token := link[len(link)-43:]
	if _, e := s.Current(sub.ID, token, 56); e != ErrNotPublished {
		t.Fatal("unpublished subscription delivered", e)
	}
	preview, e := s.Preview(sub.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Publish(sub.ID, preview.ExpectedRevision, preview.DraftDigest, "first"); e != nil {
		t.Fatal(e)
	}
	first, e := s.Current(sub.ID, token, 56)
	if e != nil || first.Playback.TotalBitrateBPS != 4000000 {
		t.Fatal("publish failed", e)
	}
	s.SaveEntity("policy", sub.PolicyID, "Policy", json.RawMessage(`{"resolution":"720p","totalBitrateBps":3000000}`))
	still, _ := s.Current(sub.ID, token, 56)
	if still.Playback.TotalBitrateBPS != 4000000 {
		t.Fatal("draft leaked to published config")
	}
	if _, e = s.Publish(sub.ID, 0, preview.DraftDigest, "stale"); e == nil {
		t.Fatal("stale publish accepted")
	}
	preview, _ = s.Preview(sub.ID)
	s.Publish(sub.ID, preview.ExpectedRevision, preview.DraftDigest, "second")
	if _, e = s.Rollback(sub.ID, 1, 2); e != nil {
		t.Fatal(e)
	}
	current, _ := s.Current(sub.ID, token, 56)
	if current.Revision != 3 || current.Playback.TotalBitrateBPS != 4000000 {
		t.Fatal("rollback did not create higher revision")
	}
	s.SetEnabled(sub.ID, false)
	if _, e = s.Current(sub.ID, token, 56); e != ErrDisabled {
		t.Fatal("disabled subscription still usable")
	}
	s.SetEnabled(sub.ID, true)
	if _, e = s.Rotate(sub.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Current(sub.ID, token, 56); e != ErrUnauthorized {
		t.Fatal("old token still usable")
	}
}
func TestMaskedEntityAndPasswordPreservation(t *testing.T) {
	s, sub, _ := fixture(t)
	list, e := s.Entities("credential")
	if e != nil || len(list) != 1 {
		t.Fatal(e)
	}
	var data map[string]any
	json.Unmarshal(list[0].Data, &data)
	if _, ok := data["password"]; ok || !list[0].HasPassword {
		t.Fatal("credential leaked")
	}
	old, _ := s.Store.Entity("credential", sub.CredentialID)
	json.Unmarshal(old.Data, &data)
	delete(data, "password")
	raw, _ := json.Marshal(data)
	if _, e = s.SaveEntity("credential", old.ID, "Renamed", raw); e != nil {
		t.Fatal("omitted password not preserved", e)
	}
}
