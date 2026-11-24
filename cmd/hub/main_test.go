package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
)

func TestKeyPersistenceAndBackupRestores(t *testing.T) {
	root := t.TempDir()
	secrets := filepath.Join(root, "secrets")
	t.Setenv("HUB_MASTER_KEY_FILE", filepath.Join(secrets, "hub.key"))
	t.Setenv("HUB_DATA_DIR", filepath.Join(root, "original"))
	if err := prepareMasterKey(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(secrets, "hub.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareMasterKey(); err != nil {
		t.Fatal(err)
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

func TestFreshStartupCreatesStableKeyAndRefusesMissingExistingKey(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	key := filepath.Join(root, "secrets", "hub.key")
	t.Setenv("HUB_DATA_DIR", data)
	t.Setenv("HUB_MASTER_KEY_FILE", key)
	db, err := store()
	if err == nil {
		db.Close()
		t.Fatal("store unexpectedly opened before startup initialization")
	}
	// serve must initialize its secret before opening storage, without any public URL.
	t.Setenv("HUB_LISTEN", "invalid-listen-address")
	if err = serve(); err == nil {
		t.Fatal("invalid listen fixture unexpectedly started")
	}
	original, err := os.ReadFile(key)
	if err != nil {
		t.Fatal("fresh startup did not generate master key", err)
	}
	if err = serve(); err == nil {
		t.Fatal("invalid listen fixture unexpectedly started")
	}
	after, _ := os.ReadFile(key)
	if string(original) != string(after) {
		t.Fatal("restart changed master key")
	}
	if err = os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if err = serve(); err == nil {
		t.Fatal("existing database started without its key")
	}
	if _, err = os.Stat(key); !os.IsNotExist(err) {
		t.Fatal("existing database caused a replacement key to be generated")
	}
}

func TestSetupCodeStableUntilInitializationCompletes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HUB_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("HUB_MASTER_KEY_FILE", filepath.Join(root, "secrets", "hub.key"))
	if err := prepareMasterKey(); err != nil {
		t.Fatal(err)
	}
	db, err := store()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = prepareSetup(db); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(setupCodePath())
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareSetup(db); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(setupCodePath())
	if string(before) != string(after) {
		t.Fatal("restart changed unfinished setup code")
	}
	if err = db.InitializeAdmin("fixture", "fixture-hash"); err != nil {
		t.Fatal(err)
	}
	if err = prepareSetup(db); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(setupCodePath()); !os.IsNotExist(err) {
		t.Fatal("completed setup code remained on disk")
	}
}
