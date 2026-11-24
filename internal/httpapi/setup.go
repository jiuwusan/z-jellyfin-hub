package httpapi

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
)

func (s *server) setupStatus(w http.ResponseWriter, r *http.Request) {
	_, err := s.svc.Store.Metadata("admin_password")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "INTERNAL")
		return
	}
	jsonResponse(w, 200, map[string]bool{"required": errors.Is(err, sql.ErrNoRows)})
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		fail(w, 403, "FORBIDDEN")
		return
	}
	if !s.allowed(r, "setup", 6) {
		fail(w, 429, "RATE_LIMITED")
		return
	}
	if _, err := s.svc.Store.Metadata("admin_password"); err == nil {
		fail(w, 409, "CONFLICT")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "INTERNAL")
		return
	}
	var input struct {
		Code     string `json:"code"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	stored, err := s.svc.Store.Metadata("setup_code_hash")
	digest, decodeErr := hex.DecodeString(stored)
	if err != nil || decodeErr != nil || !secret.Matches(digest, input.Code) {
		fail(w, 401, "UNAUTHORIZED")
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || len(input.Username) > 120 || strings.ContainsAny(input.Username, "\r\n\x00") {
		fail(w, 400, "CONFIG_INVALID")
		return
	}
	hash, err := secret.HashPassword(input.Password)
	if err != nil {
		fail(w, 400, "CONFIG_INVALID")
		return
	}
	if err = s.svc.Store.InitializeAdmin(input.Username, hash); err != nil {
		result(w, nil, err)
		return
	}
	if s.options.SetupComplete != nil {
		s.options.SetupComplete()
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
