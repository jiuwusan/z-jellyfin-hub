package domain

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDocumentedConfigurationExample(t *testing.T) {
	raw, err := os.ReadFile("../../docs/api/subscription-v2.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var value Configuration
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err = value.Validate(); err != nil {
		t.Fatal("documented example is incompatible", err)
	}
}

func TestConfigurationValidation(t *testing.T) {
	c := Configuration{SchemaVersion: 2, ServerInstanceID: "11111111111111111111111111111111", SubscriptionID: "fixture", Revision: 1, MinClientVersionCode: 56, RefreshIntervalSeconds: 900,
		Connection: Connection{Proxy: Proxy{Type: "http", Host: "proxy.example.com", Port: 8080}, Jellyfin: Jellyfin{BaseURL: "http://media.internal:8096/jellyfin", Username: "fixture", Password: "fixture-only"}}, Playback: Policy{Resolution: "1080p", TotalBitrateBPS: 4000000}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Playback.TotalBitrateBPS = 123
	if c.Validate() == nil {
		t.Fatal("unsupported bitrate silently accepted")
	}
	c.Playback.TotalBitrateBPS = 4000000
	c.Connection.Proxy.Host = "http://proxy.example.com"
	if c.Validate() == nil {
		t.Fatal("URL accepted as proxy host")
	}
	c.Connection.Proxy.Host = "proxy.example.com"
	c.Connection.Jellyfin.BaseURL = "http://media.internal/path?secret=bad"
	if c.Validate() == nil {
		t.Fatal("query accepted in server base URL")
	}
}
