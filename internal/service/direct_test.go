package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDirectSubscriptionPublishAndSwitch(t *testing.T) {
	s, sub, link := fixture(t)
	proxyID := sub.ProxyID
	sub.ProxyID = ""
	saved, _, err := s.SaveSubscription(sub)
	if err != nil {
		t.Fatal("direct subscription rejected", err)
	}
	preview, err := s.Preview(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(preview.Configuration)
	var payload map[string]any
	json.Unmarshal(raw, &payload)
	connection := payload["connection"].(map[string]any)
	if _, present := connection["proxy"]; present {
		t.Fatal("direct preview contains proxy")
	}
	if preview.Configuration.MinClientVersionCode < 72 {
		t.Fatal("old clients may receive direct configuration")
	}
	if _, err = s.Publish(saved.ID, preview.ExpectedRevision, preview.DraftDigest, "direct fixture"); err != nil {
		t.Fatal(err)
	}
	direct, err := s.Current(saved.ID, strings.SplitN(link, "#", 2)[1], 72)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(direct)
	if strings.Contains(string(raw), `"proxy":`) {
		t.Fatal("published direct configuration contains proxy")
	}
	if _, err = s.Current(saved.ID, strings.SplitN(link, "#", 2)[1], 71); err != ErrClientOld {
		t.Fatal("old client not rejected", err)
	}
	saved.ProxyID = "nonexistent-proxy"
	if _, _, err = s.SaveSubscription(saved); err == nil {
		t.Fatal("invalid proxy silently became direct")
	}
	saved.ProxyID = proxyID
	if _, _, err = s.SaveSubscription(saved); err != nil {
		t.Fatal("switch back to proxy failed", err)
	}
	preview, err = s.Preview(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(preview.Configuration)
	if !strings.Contains(string(raw), `"proxy":`) {
		t.Fatal("switching back lost proxy")
	}
	proxyRevision, err := s.Publish(saved.ID, preview.ExpectedRevision, preview.DraftDigest, "proxy fixture")
	if err != nil {
		t.Fatal(err)
	}
	if config, err := s.Current(saved.ID, strings.SplitN(link, "#", 2)[1], 57); err != nil || config.Connection.Proxy == nil {
		t.Fatal("proxy publication lost old-client compatibility", err)
	}
	if _, err = s.Rollback(saved.ID, 1, proxyRevision); err != nil {
		t.Fatal("rollback to direct failed", err)
	}
	if config, err := s.Current(saved.ID, strings.SplitN(link, "#", 2)[1], 72); err != nil || config.Connection.Proxy != nil || config.MinClientVersionCode < 72 {
		t.Fatal("direct rollback lost mode or compatibility gate", err)
	}
}
