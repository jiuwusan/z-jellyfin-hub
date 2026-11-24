package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jiuwusan/z-jellyfin-hub/internal/domain"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed webassets
var assets embed.FS

type Options struct {
	RequireUI     bool
	Now           func() time.Time
	SetupComplete func()
}
type session struct {
	CSRF, Username string
	Until          time.Time
}
type window struct {
	Count int
	Until time.Time
}
type server struct {
	svc      *service.Service
	options  Options
	mu       sync.Mutex
	sessions map[[32]byte]session
	limits   map[string]window
}

func New(svc *service.Service, options Options) http.Handler {
	if options.Now == nil {
		options.Now = time.Now
	}
	s := &server{svc: svc, options: options, sessions: map[[32]byte]session{}, limits: map[string]window{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /api/v1/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/v1/setup/complete", s.setup)
	mux.HandleFunc("POST /api/v1/admin/login", s.login)
	mux.HandleFunc("GET /api/v1/admin/session", s.admin(false, s.session))
	mux.HandleFunc("POST /api/v1/admin/logout", s.admin(true, s.logout))
	mux.HandleFunc("POST /api/v1/admin/password", s.admin(true, s.password))
	mux.HandleFunc("GET /api/v1/admin/entities/{kind}", s.admin(false, s.entities))
	mux.HandleFunc("POST /api/v1/admin/entities/{kind}", s.admin(true, s.saveEntity))
	mux.HandleFunc("PUT /api/v1/admin/entities/{kind}/{id}", s.admin(true, s.saveEntity))
	mux.HandleFunc("DELETE /api/v1/admin/entities/{kind}/{id}", s.admin(true, s.deleteEntity))
	mux.HandleFunc("GET /api/v1/admin/subscriptions", s.admin(false, s.subscriptions))
	mux.HandleFunc("POST /api/v1/admin/subscriptions", s.admin(true, s.saveSubscription))
	mux.HandleFunc("PUT /api/v1/admin/subscriptions/{id}", s.admin(true, s.saveSubscription))
	mux.HandleFunc("DELETE /api/v1/admin/subscriptions/{id}", s.admin(true, s.deleteSubscription))
	mux.HandleFunc("GET /api/v1/admin/subscriptions/{id}/preview", s.admin(false, s.preview))
	mux.HandleFunc("GET /api/v1/admin/subscriptions/{id}/versions", s.admin(false, s.versions))
	mux.HandleFunc("GET /api/v1/admin/subscriptions/{id}/feedback", s.admin(false, s.feedbackList))
	mux.HandleFunc("POST /api/v1/admin/subscriptions/{id}/publish", s.admin(true, s.publish))
	mux.HandleFunc("POST /api/v1/admin/subscriptions/{id}/rollback", s.admin(true, s.rollback))
	mux.HandleFunc("POST /api/v1/admin/subscriptions/{id}/rotate", s.admin(true, s.rotate))
	mux.HandleFunc("POST /api/v1/admin/subscriptions/{id}/token", s.admin(true, s.setToken))
	mux.HandleFunc("POST /api/v1/admin/subscriptions/{id}/state", s.admin(true, s.state))
	mux.HandleFunc("GET /api/v1/subscription/config", s.configuration)
	mux.HandleFunc("POST /api/v1/subscription/feedback", s.feedback)
	mux.HandleFunc("/", s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, routeRequest(r))
	})
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code string) {
	messages := map[string]string{"UNAUTHORIZED": "认证无效，请重新登录或更新订阅", "FORBIDDEN": "请求校验失败", "CONFIG_INVALID": "配置不完整或包含不支持的值", "CONFLICT": "配置已变化，请刷新预览后重试", "REFERENCE_IN_USE": "配置仍被引用，不能删除", "NOT_FOUND": "记录不存在", "SUBSCRIPTION_DISABLED": "订阅已停用", "NOT_PUBLISHED": "订阅尚未发布", "CLIENT_TOO_OLD": "请更新客户端", "RATE_LIMITED": "操作过于频繁，请稍后重试", "TOKEN_IN_USE": "令牌已绑定其他订阅，请使用不同令牌", "INTERNAL": "服务暂时不可用", "UI_NOT_BUILT": "管理页面尚未构建"}
	jsonResponse(w, status, map[string]string{"code": code, "message": messages[code]})
}
func result(w http.ResponseWriter, value any, e error) {
	if e == nil {
		jsonResponse(w, 200, value)
		return
	}
	switch {
	case errors.Is(e, service.ErrUnauthorized):
		fail(w, 401, "UNAUTHORIZED")
	case errors.Is(e, service.ErrDisabled):
		fail(w, 410, "SUBSCRIPTION_DISABLED")
	case errors.Is(e, service.ErrNotPublished):
		fail(w, 409, "NOT_PUBLISHED")
	case errors.Is(e, service.ErrClientOld):
		fail(w, 426, "CLIENT_TOO_OLD")
	case errors.Is(e, domain.ErrInvalid):
		fail(w, 400, "CONFIG_INVALID")
	case errors.Is(e, storage.ErrTokenInUse):
		fail(w, 409, "TOKEN_IN_USE")
	case errors.Is(e, storage.ErrConflict):
		fail(w, 409, "CONFLICT")
	case errors.Is(e, service.ErrReferenced):
		fail(w, 409, "REFERENCE_IN_USE")
	case errors.Is(e, sql.ErrNoRows):
		fail(w, 404, "NOT_FOUND")
	default:
		fail(w, 500, "INTERNAL")
	}
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(value); e != nil {
		fail(w, 400, "CONFIG_INVALID")
		return false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		fail(w, 400, "CONFIG_INVALID")
		return false
	}
	return true
}
func (s *server) allowed(r *http.Request, bucket string, max int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := bucket + ":" + host
	now := s.options.Now()
	value := s.limits[key]
	if !now.Before(value.Until) {
		value = window{Until: now.Add(time.Minute)}
	}
	if value.Count >= max {
		return false
	}
	value.Count++
	if len(s.limits) >= 4096 {
		for k, v := range s.limits {
			if !now.Before(v.Until) {
				delete(s.limits, k)
			}
		}
		if len(s.limits) >= 4096 {
			return false
		}
	}
	s.limits[key] = value
	return true
}

const httpsSessionCookie = "__Host-hub_session"
const httpSessionCookie = "hub_session_http"

func (s *server) lookup(r *http.Request) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Secure cookies are only sent by browsers over HTTPS, including behind TLS termination.
	for _, name := range []string{httpsSessionCookie, httpSessionCookie} {
		cookie, e := r.Cookie(name)
		if e != nil {
			continue
		}
		key := sha256.Sum256([]byte(cookie.Value))
		v, ok := s.sessions[key]
		if ok && s.options.Now().Before(v.Until) {
			return v, true
		}
		delete(s.sessions, key)
	}
	return session{}, false
}
func (s *server) admin(write bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, ok := s.lookup(r)
		if !ok {
			fail(w, 401, "UNAUTHORIZED")
			return
		}
		if write && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(v.CSRF)) != 1 {
			fail(w, 403, "FORBIDDEN")
			return
		}
		next(w, r)
	}
}
func (s *server) cookie(w http.ResponseWriter, r *http.Request, value string, age int) {
	secure := secureRequest(r)
	name := httpSessionCookie
	if secure {
		name = httpsSessionCookie
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age})
	if age < 0 && secure {
		http.SetCookie(w, &http.Cookie{Name: httpSessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: age})
	}
}
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r, "login", 6) {
		fail(w, 429, "RATE_LIMITED")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	name, e := s.svc.Store.Metadata("admin_username")
	hash, f := s.svc.Store.Metadata("admin_password")
	valid := secret.VerifyPassword(hash, input.Password)
	if e != nil || f != nil || input.Username != name || !valid {
		fail(w, 401, "UNAUTHORIZED")
		return
	}
	token, e := secret.Token()
	if e != nil {
		fail(w, 500, "INTERNAL")
		return
	}
	csrf, e := secret.Token()
	if e != nil {
		fail(w, 500, "INTERNAL")
		return
	}
	s.mu.Lock()
	now := s.options.Now()
	for k, v := range s.sessions {
		if !now.Before(v.Until) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 128 {
		s.mu.Unlock()
		fail(w, 429, "RATE_LIMITED")
		return
	}
	s.sessions[sha256.Sum256([]byte(token))] = session{CSRF: csrf, Username: name, Until: now.Add(8 * time.Hour)}
	s.mu.Unlock()
	s.cookie(w, r, token, 8*3600)
	jsonResponse(w, 200, map[string]string{"username": name, "csrf": csrf})
}
func (s *server) session(w http.ResponseWriter, r *http.Request) {
	v, _ := s.lookup(r)
	jsonResponse(w, 200, map[string]string{"username": v.Username, "csrf": v.CSRF})
}
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	for _, name := range []string{httpsSessionCookie, httpSessionCookie} {
		if cookie, e := r.Cookie(name); e == nil {
			delete(s.sessions, sha256.Sum256([]byte(cookie.Value)))
		}
	}
	s.mu.Unlock()
	s.cookie(w, r, "", -1)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *server) password(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Old string `json:"oldPassword"`
		New string `json:"newPassword"`
	}
	if !decode(w, r, &input) {
		return
	}
	hash, e := s.svc.Store.Metadata("admin_password")
	if e != nil || !secret.VerifyPassword(hash, input.Old) {
		fail(w, 401, "UNAUTHORIZED")
		return
	}
	next, e := secret.HashPassword(input.New)
	if e != nil {
		fail(w, 400, "CONFIG_INVALID")
		return
	}
	if e = s.svc.Store.SetMetadata("admin_password", next); e != nil {
		result(w, nil, e)
		return
	}
	s.mu.Lock()
	s.sessions = map[[32]byte]session{}
	s.mu.Unlock()
	s.cookie(w, r, "", -1)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *server) entities(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.Entities(r.PathValue("kind"))
	result(w, v, e)
}
func (s *server) saveEntity(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Label string          `json:"label"`
		Data  json.RawMessage `json:"data"`
	}
	if !decode(w, r, &input) {
		return
	}
	v, e := s.svc.SaveEntity(r.PathValue("kind"), r.PathValue("id"), input.Label, input.Data)
	result(w, v, e)
}
func (s *server) deleteEntity(w http.ResponseWriter, r *http.Request) {
	result(w, map[string]bool{"ok": true}, s.svc.DeleteEntity(r.PathValue("kind"), r.PathValue("id")))
}
func (s *server) subscriptions(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.Store.Subscriptions()
	result(w, v, e)
}
func (s *server) saveSubscription(w http.ResponseWriter, r *http.Request) {
	var v struct {
		storage.Subscription
		Token string `json:"token"`
	}
	if !decode(w, r, &v) {
		return
	}
	v.ID = r.PathValue("id")
	saved, link, e := s.svc.SaveSubscriptionWithToken(v.Subscription, v.Token)
	result(w, map[string]any{"subscription": saved, "link": link}, e)
}
func (s *server) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	result(w, map[string]bool{"ok": true}, s.svc.DeleteSubscription(r.PathValue("id")))
}

func (s *server) setToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &input) {
		return
	}
	link, e := s.svc.SetToken(r.PathValue("id"), input.Token)
	result(w, map[string]string{"link": link}, e)
}
func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.Preview(r.PathValue("id"))
	result(w, v, e)
}
func (s *server) versions(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.Store.Versions(r.PathValue("id"))
	result(w, v, e)
}
func (s *server) feedbackList(w http.ResponseWriter, r *http.Request) {
	v, e := s.svc.Store.Feedback(r.PathValue("id"))
	result(w, v, e)
}
func (s *server) publish(w http.ResponseWriter, r *http.Request) {
	var v struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		DraftDigest      string `json:"draftDigest"`
		Note             string `json:"note"`
	}
	if !decode(w, r, &v) {
		return
	}
	revision, e := s.svc.Publish(r.PathValue("id"), v.ExpectedRevision, v.DraftDigest, v.Note)
	result(w, map[string]int64{"revision": revision}, e)
}
func (s *server) rollback(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision         int64 `json:"revision"`
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &v) {
		return
	}
	revision, e := s.svc.Rollback(r.PathValue("id"), v.Revision, v.ExpectedRevision)
	result(w, map[string]int64{"revision": revision}, e)
}
func (s *server) rotate(w http.ResponseWriter, r *http.Request) {
	link, e := s.svc.Rotate(r.PathValue("id"))
	result(w, map[string]string{"link": link}, e)
}
func (s *server) state(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Enabled *bool `json:"enabled"`
	}
	if !decode(w, r, &v) {
		return
	}
	if v.Enabled == nil {
		fail(w, 400, "CONFIG_INVALID")
		return
	}
	result(w, map[string]bool{"ok": true}, s.svc.SetEnabled(r.PathValue("id"), *v.Enabled))
}
func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}
func (s *server) configuration(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r, "config", 120) {
		fail(w, 429, "RATE_LIMITED")
		return
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Client-Version-Code"))
	binding, e := s.svc.AuthenticateToken(bearer(r))
	if e != nil {
		result(w, nil, e)
		return
	}
	c, e := s.svc.Current(binding.ID, bearer(r), version)
	if e != nil {
		result(w, nil, e)
		return
	}
	tag := "\"" + c.ServerInstanceID + "-" + c.SubscriptionID + "-" + strconv.FormatInt(c.Revision, 10) + "\""
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Vary", "Authorization, X-Client-Version-Code")
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(304)
		return
	}
	jsonResponse(w, 200, c)
}
func (s *server) feedback(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r, "feedback", 60) {
		fail(w, 429, "RATE_LIMITED")
		return
	}
	binding, e := s.svc.AuthenticateToken(bearer(r))
	if e != nil {
		result(w, nil, e)
		return
	}
	var v storage.Feedback
	if !decode(w, r, &v) {
		return
	}
	statuses := map[string]bool{"synced": true, "applied": true, "pending": true, "error": true}
	codes := map[string]bool{"": true, "NETWORK": true, "PROXY_AUTH": true, "JELLYFIN_AUTH": true, "UNSUPPORTED_CONFIG": true, "REVOKED": true}
	if len(v.DeviceID) < 16 || len(v.DeviceID) > 128 || v.Platform != "android" && v.Platform != "ios" || v.VersionCode < 1 || v.Revision < 0 || !statuses[v.Status] || !codes[v.ErrorCode] {
		fail(w, 400, "CONFIG_INVALID")
		return
	}
	hash := sha256.Sum256([]byte(binding.ID + ":" + v.DeviceID))
	v.DeviceID = hex.EncodeToString(hash[:])
	result(w, map[string]bool{"ok": true}, s.svc.Store.PutFeedback(binding.ID, v))
}
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	if _, e := s.svc.Store.Metadata("key_check"); e != nil {
		fail(w, 503, "INTERNAL")
		return
	}
	if s.options.RequireUI {
		if _, e := fs.Stat(assets, "webassets/dist/index.html"); e != nil {
			fail(w, 503, "UI_NOT_BUILT")
			return
		}
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" || strings.HasPrefix(r.URL.Path, "/api/") {
		fail(w, 404, "NOT_FOUND")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		sub, _ := fs.Sub(assets, "webassets/dist")
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		if !strings.Contains(r.URL.Path, ".") && !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusPermanentRedirect)
			return
		}
		fail(w, 404, "NOT_FOUND")
		return
	}
	body, e := fs.ReadFile(assets, "webassets/dist/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if e != nil {
		body, _ = fs.ReadFile(assets, "webassets/fallback.html")
		w.WriteHeader(503)
	}
	w.Write(body)
}
