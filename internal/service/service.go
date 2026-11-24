package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jiuwusan/z-jellyfin-hub/internal/domain"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
	"strings"
	"sync"
)

var (
	ErrUnauthorized = errors.New("invalid subscription")
	ErrDisabled     = errors.New("subscription disabled")
	ErrNotPublished = errors.New("subscription not published")
	ErrClientOld    = errors.New("client upgrade required")
	ErrReferenced   = errors.New("configuration is referenced")
)

type Service struct {
	Store     *storage.Store
	PublicURL string
	mu        sync.Mutex
}
type Preview struct {
	Configuration    domain.Configuration `json:"configuration"`
	ExpectedRevision int64                `json:"expectedRevision"`
	DraftDigest      string               `json:"draftDigest"`
}

func New(store *storage.Store, publicURL string) *Service {
	return &Service{Store: store, PublicURL: strings.TrimRight(publicURL, "/")}
}
func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func validLabel(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 120 && !strings.ContainsAny(s, "\r\n\x00")
}
func Mask(v storage.Entity) storage.Entity {
	var data map[string]any
	if json.Unmarshal(v.Data, &data) == nil {
		if value, ok := data["password"]; ok {
			v.HasPassword = value != ""
			delete(data, "password")
		}
		v.Data, _ = json.Marshal(data)
	}
	return v
}
func (s *Service) Entities(kind string) ([]storage.Entity, error) {
	list, e := s.Store.Entities(kind)
	for i := range list {
		list[i] = Mask(list[i])
	}
	return list, e
}
func (s *Service) SaveEntity(kind, id, label string, raw json.RawMessage) (storage.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validLabel(label) {
		return storage.Entity{}, domain.ErrInvalid
	}
	if id == "" {
		id = ID()
	} else {
		old, e := s.Store.Entity(kind, id)
		if e != nil {
			return storage.Entity{}, e
		}
		var next, previous map[string]any
		json.Unmarshal(raw, &next)
		json.Unmarshal(old.Data, &previous)
		if next == nil {
			return storage.Entity{}, domain.ErrInvalid
		}
		if _, exists := next["password"]; !exists {
			if value, ok := previous["password"]; ok {
				next["password"] = value
			}
		}
		raw, _ = json.Marshal(next)
	}
	if e := domain.ValidateEntity(kind, raw); e != nil {
		return storage.Entity{}, e
	}
	if kind == "credential" {
		var c domain.Credential
		json.Unmarshal(raw, &c)
		if _, e := s.Store.Entity("server", c.ServerID); e != nil {
			return storage.Entity{}, domain.ErrInvalid
		}
	}
	entity := storage.Entity{ID: id, Kind: kind, Label: strings.TrimSpace(label), Data: raw}
	if e := s.Store.PutEntity(entity); e != nil {
		return storage.Entity{}, e
	}
	return Mask(entity), nil
}
func (s *Service) DeleteEntity(kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, e := s.Store.Subscriptions()
	if e != nil {
		return e
	}
	for _, v := range subs {
		if kind == "proxy" && v.ProxyID == id || kind == "credential" && v.CredentialID == id || kind == "policy" && v.PolicyID == id {
			return ErrReferenced
		}
	}
	if kind == "server" {
		credentials, e := s.Store.Entities("credential")
		if e != nil {
			return e
		}
		for _, v := range credentials {
			var c domain.Credential
			json.Unmarshal(v.Data, &c)
			if c.ServerID == id {
				return ErrReferenced
			}
		}
	}
	return s.Store.DeleteEntity(kind, id)
}
func (s *Service) SaveSubscription(v storage.Subscription) (storage.Subscription, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validLabel(v.Label) {
		return v, "", domain.ErrInvalid
	}
	if v.RefreshIntervalSeconds == 0 {
		v.RefreshIntervalSeconds = 900
	}
	if v.MinClientVersionCode == 0 {
		v.MinClientVersionCode = 56
	}
	token := ""
	if v.ID == "" {
		v.ID = ID()
		v.Enabled = true
		var e error
		token, e = secret.Token()
		if e != nil {
			return v, "", e
		}
		v.TokenHash, _ = secret.Digest(token)
		v.Revision = 0
	} else {
		old, e := s.Store.Subscription(v.ID)
		if e != nil {
			return v, "", e
		}
		v.Enabled = old.Enabled
		v.TokenHash = old.TokenHash
		v.Revision = old.Revision
	}
	if _, e := s.build(v); e != nil {
		return v, "", e
	}
	if e := s.Store.SaveSubscription(v); e != nil {
		return v, "", e
	}
	link := ""
	if token != "" {
		link = s.link(v.ID, token)
	}
	return v, link, nil
}
func (s *Service) link(id, token string) string {
	return s.PublicURL + "/api/v1/subscriptions/" + id + "/config#" + token
}
func (s *Service) Rotate(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.Store.Subscription(id)
	if e != nil {
		return "", e
	}
	token, e := secret.Token()
	if e != nil {
		return "", e
	}
	v.TokenHash, _ = secret.Digest(token)
	if e = s.Store.SaveSubscription(v); e != nil {
		return "", e
	}
	return s.link(id, token), nil
}
func (s *Service) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.Store.Subscription(id)
	if e != nil {
		return e
	}
	v.Enabled = enabled
	return s.Store.SaveSubscription(v)
}
func (s *Service) DeleteSubscription(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Store.DeleteSubscription(id)
}
func (s *Service) build(v storage.Subscription) (domain.Configuration, error) {
	c := domain.Configuration{SchemaVersion: 1, SubscriptionID: v.ID, Revision: v.Revision + 1, MinClientVersionCode: v.MinClientVersionCode, RefreshIntervalSeconds: v.RefreshIntervalSeconds}
	entity, e := s.Store.Entity("proxy", v.ProxyID)
	if e != nil {
		return c, domain.ErrInvalid
	}
	if e = json.Unmarshal(entity.Data, &c.Connection.Proxy); e != nil {
		return c, e
	}
	entity, e = s.Store.Entity("policy", v.PolicyID)
	if e != nil {
		return c, domain.ErrInvalid
	}
	if e = json.Unmarshal(entity.Data, &c.Playback); e != nil {
		return c, e
	}
	entity, e = s.Store.Entity("credential", v.CredentialID)
	if e != nil {
		return c, domain.ErrInvalid
	}
	var credentials domain.Credential
	if e = json.Unmarshal(entity.Data, &credentials); e != nil {
		return c, e
	}
	server, e := s.Store.Entity("server", credentials.ServerID)
	if e != nil {
		return c, domain.ErrInvalid
	}
	var endpoint domain.Server
	if e = json.Unmarshal(server.Data, &endpoint); e != nil {
		return c, e
	}
	c.Connection.Jellyfin = domain.Jellyfin{BaseURL: endpoint.BaseURL, Username: credentials.Username, Password: credentials.Password}
	return c, c.Validate()
}
func fingerprint(c domain.Configuration) string {
	raw, _ := json.Marshal(c)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (s *Service) Preview(id string) (Preview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.Store.Subscription(id)
	if e != nil {
		return Preview{}, e
	}
	c, e := s.build(v)
	if e != nil {
		return Preview{}, e
	}
	out := Preview{Configuration: c, ExpectedRevision: v.Revision, DraftDigest: fingerprint(c)}
	out.Configuration.Connection.Proxy.Password = "********"
	out.Configuration.Connection.Jellyfin.Password = "********"
	return out, nil
}
func (s *Service) Publish(id string, expected int64, digest, note string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.Store.Subscription(id)
	if e != nil {
		return 0, e
	}
	if v.Revision != expected {
		return 0, storage.ErrConflict
	}
	c, e := s.build(v)
	if e != nil {
		return 0, e
	}
	if digest == "" || digest != fingerprint(c) || len(note) > 500 {
		return 0, storage.ErrConflict
	}
	raw, _ := json.Marshal(c)
	return s.Store.Publish(id, expected, raw, note)
}
func (s *Service) Rollback(id string, revision, expected int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.Store.Subscription(id)
	if e != nil {
		return 0, e
	}
	if v.Revision != expected {
		return 0, storage.ErrConflict
	}
	raw, e := s.Store.Release(id, revision)
	if e != nil {
		return 0, e
	}
	var c domain.Configuration
	if e = json.Unmarshal(raw, &c); e != nil {
		return 0, e
	}
	c.Revision = expected + 1
	if c.SubscriptionID != id || c.Validate() != nil {
		return 0, domain.ErrInvalid
	}
	raw, _ = json.Marshal(c)
	return s.Store.Publish(id, expected, raw, "rollback")
}
func (s *Service) Authenticate(id, token string) (storage.Subscription, error) {
	v, e := s.Store.Subscription(id)
	if e != nil || !secret.Matches(v.TokenHash, token) {
		return storage.Subscription{}, ErrUnauthorized
	}
	if !v.Enabled {
		return v, ErrDisabled
	}
	return v, nil
}
func (s *Service) Current(id, token string, version int) (domain.Configuration, error) {
	v, e := s.Authenticate(id, token)
	if e != nil {
		return domain.Configuration{}, e
	}
	if v.Revision == 0 {
		return domain.Configuration{}, ErrNotPublished
	}
	raw, e := s.Store.Release(id, v.Revision)
	if e != nil {
		return domain.Configuration{}, e
	}
	var c domain.Configuration
	if e = json.Unmarshal(raw, &c); e != nil {
		return c, e
	}
	if e = c.Validate(); e != nil {
		return c, e
	}
	if version < c.MinClientVersionCode {
		return c, ErrClientOld
	}
	return c, nil
}
