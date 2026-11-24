package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
)

func TestInitRefusesOverwriteAndBackupRestores(t *testing.T) {
	root := t.TempDir()
	secrets := filepath.Join(root, "secrets")
	if err := initialize(secrets); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(secrets, "hub.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err = initialize(secrets); err == nil {
		t.Fatal("initialization overwrote existing secrets")
	}
	after, _ := os.ReadFile(filepath.Join(secrets, "hub.key"))
	if string(original) != string(after) {
		t.Fatal("master key changed")
	}
	t.Setenv("HUB_MASTER_KEY_FILE", filepath.Join(secrets, "hub.key"))
	t.Setenv("HUB_DATA_DIR", filepath.Join(root, "original"))
	db, err := store()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PutEntity(storage.Entity{ID: "fixture", Kind: "proxy", Label: "Test", Data: []byte(`{"password":"fixture-value"}`)}); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(root, "restored")
	if err = os.MkdirAll(restored, 0700); err != nil {
		t.Fatal(err)
	}
	if err = db.Backup(filepath.Join(restored, "hub.db")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("HUB_DATA_DIR", restored)
	restoredDB, err := store()
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	e, err := restoredDB.Entity("proxy", "fixture")
	if err != nil || string(e.Data) != `{"password":"fixture-value"}` {
		t.Fatal("backup restore could not decrypt original entity")
	}
}

func TestRejectUnsafePublicURLBeforeOpeningStorage(t *testing.T) {
	for _, fixture := range []struct{ public, dev, listen string }{
		{"http://hub.example.com", "", ":8080"},
		{"http://hub.example.com", "true", "127.0.0.1:8080"},
		{"http://127.0.0.1:8080", "true", ":8080"},
		{"https://user:password@hub.example.com", "", ":8080"},
		{"https://hub.example.com?secret=fixture", "", ":8080"},
	} {
		t.Setenv("HUB_PUBLIC_URL", fixture.public)
		t.Setenv("HUB_DEV_ALLOW_HTTP", fixture.dev)
		t.Setenv("HUB_LISTEN", fixture.listen)
		t.Setenv("HUB_MASTER_KEY_FILE", filepath.Join(t.TempDir(), "absent"))
		if err := serve(); err == nil || err.Error() == "master key file is unavailable" {
			t.Fatal("unsafe URL reached storage startup")
		}
	}
}
