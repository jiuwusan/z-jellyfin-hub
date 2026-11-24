package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
)

func masterKeyPath() string { return env("HUB_MASTER_KEY_FILE", "/secrets/hub.key") }
func setupCodePath() string { return filepath.Join(filepath.Dir(masterKeyPath()), "setup.code") }

func createPrivateFile(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = file.WriteString(value + "\n")
	closed := file.Close()
	if err != nil {
		return err
	}
	return closed
}

func prepareMasterKey() error {
	path := masterKeyPath()
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(filepath.Join(env("HUB_DATA_DIR", "/data"), "hub.db")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("existing database requires its original master key")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return createPrivateFile(path, base64.StdEncoding.EncodeToString(key))
}

func prepareSetup(db *storage.Store) error {
	if _, err := db.Metadata("admin_password"); err == nil {
		// A stale setup file is no longer usable after the transaction commits.
		_ = os.Remove(setupCodePath())
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	token, err := secret.Token()
	if err != nil {
		return err
	}
	if err = createPrivateFile(setupCodePath(), token); err != nil {
		return err
	}
	raw, err := os.ReadFile(setupCodePath())
	if err != nil {
		return err
	}
	digest, err := secret.Digest(strings.TrimSpace(string(raw)))
	if err != nil {
		return errors.New("setup code file is invalid")
	}
	return db.SetMetadata("setup_code_hash", hex.EncodeToString(digest))
}
