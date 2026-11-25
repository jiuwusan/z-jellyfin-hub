package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
)

var ErrInvalid = errors.New("invalid configuration")

type Proxy struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type Jellyfin struct {
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type Connection struct {
	Proxy    *Proxy   `json:"proxy,omitempty"`
	Jellyfin Jellyfin `json:"jellyfin"`
}
type Policy struct {
	Resolution      string `json:"resolution"`
	TotalBitrateBPS int    `json:"totalBitrateBps"`
}
type Configuration struct {
	SchemaVersion          int        `json:"schemaVersion"`
	ServerInstanceID       string     `json:"serverInstanceId"`
	SubscriptionID         string     `json:"subscriptionId"`
	Revision               int64      `json:"revision"`
	MinClientVersionCode   int        `json:"minClientVersionCode"`
	RefreshIntervalSeconds int        `json:"refreshIntervalSeconds"`
	Connection             Connection `json:"connection"`
	Playback               Policy     `json:"playback"`
}
type Server struct {
	BaseURL string `json:"baseUrl"`
}
type Credential struct {
	ServerID string `json:"serverId"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func ValidBaseURL(value string) bool {
	u, e := url.Parse(value)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && len(value) <= 2048 && !strings.ContainsAny(value, "\r\n\t ")
}
func (p Proxy) Validate() error {
	if p.Type != "http" || p.Host == "" || len(p.Host) > 253 || strings.TrimSpace(p.Host) != p.Host || strings.ContainsAny(p.Host, "/@?#\\ \r\n\t") || p.Port < 1 || p.Port > 65535 || len(p.Username) > 256 || len(p.Password) > 1024 {
		return ErrInvalid
	}
	if strings.Contains(p.Host, ":") && net.ParseIP(strings.Trim(p.Host, "[]")) == nil {
		return ErrInvalid
	}
	return nil
}
func (p Policy) Validate() error {
	res := map[string]bool{"auto": true, "screen": true, "360p": true, "480p": true, "720p": true, "1080p": true, "4k": true, "8k": true}
	rates := map[int]bool{0: true, 120000000: true, 80000000: true, 60000000: true, 40000000: true, 20000000: true, 15000000: true, 10000000: true, 8000000: true, 6000000: true, 4000000: true, 3000000: true, 1500000: true, 720000: true, 420000: true}
	if !res[p.Resolution] || !rates[p.TotalBitrateBPS] {
		return ErrInvalid
	}
	return nil
}
func (c Configuration) Validate() error {
	if c.SchemaVersion != 2 || len(c.ServerInstanceID) != 32 || strings.Trim(c.ServerInstanceID, "0123456789abcdef") != "" || c.SubscriptionID == "" || c.Revision < 1 || c.MinClientVersionCode < 1 || c.RefreshIntervalSeconds < 300 || c.RefreshIntervalSeconds > 86400 {
		return ErrInvalid
	}
	if !ValidBaseURL(c.Connection.Jellyfin.BaseURL) || strings.TrimSpace(c.Connection.Jellyfin.Username) == "" || c.Connection.Jellyfin.Password == "" || len(c.Connection.Jellyfin.Password) > 1024 {
		return ErrInvalid
	}
	if c.Connection.Proxy != nil {
		if e := c.Connection.Proxy.Validate(); e != nil {
			return e
		}
	} else if c.MinClientVersionCode < 72 {
		return ErrInvalid
	}
	return c.Playback.Validate()
}
func ValidateEntity(kind string, raw []byte) error {
	var value any
	switch kind {
	case "proxy":
		value = &Proxy{}
	case "server":
		value = &Server{}
	case "credential":
		value = &Credential{}
	case "policy":
		value = &Policy{}
	default:
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e := dec.Decode(value); e != nil {
		return ErrInvalid
	}
	switch v := value.(type) {
	case *Proxy:
		return v.Validate()
	case *Server:
		if !ValidBaseURL(v.BaseURL) {
			return ErrInvalid
		}
	case *Credential:
		if v.ServerID == "" || strings.TrimSpace(v.Username) == "" || len(v.Username) > 256 || v.Password == "" || len(v.Password) > 1024 {
			return ErrInvalid
		}
	case *Policy:
		return v.Validate()
	}
	return nil
}
