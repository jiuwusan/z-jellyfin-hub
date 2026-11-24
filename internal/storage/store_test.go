package storage

import (
	"bytes"
	"crypto/rand"
	"errors"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentSetupHasOneWinner(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	box, _ := secret.NewBox(key)
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"), box)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var won atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 4; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := db.InitializeAdmin("fixture", "fixture-hash")
			if err == nil {
				won.Add(1)
			} else if !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if won.Load() != 1 {
		t.Fatal("setup did not have exactly one winner")
	}
}

func TestEncryptedPersistenceAndAtomicVersions(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	box, _ := secret.NewBox(key)
	path := filepath.Join(t.TempDir(), "hub.db")
	store, e := Open(path, box)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.PutEntity(Entity{ID: "one", Kind: "proxy", Label: "Fixture", Data: []byte(`{"password":"fixture-sensitive-value"}`)}); e != nil {
		t.Fatal(e)
	}
	entity, e := store.Entity("proxy", "one")
	if e != nil || !bytes.Contains(entity.Data, []byte("fixture-sensitive-value")) {
		t.Fatal("read failed")
	}
	sub := Subscription{ID: "sub", Label: "Fixture", ProxyID: "one", CredentialID: "credential", PolicyID: "policy", Enabled: true, TokenHash: []byte("fixture-hash"), RefreshIntervalSeconds: 900, MinClientVersionCode: 56}
	if e = store.SaveSubscription(sub); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Publish("sub", 0, []byte(`{"revision":1,"password":"fixture-sensitive-value"}`), "first"); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Publish("sub", 0, []byte(`{"revision":2}`), "conflicting"); e != ErrConflict {
		t.Fatal("stale publication accepted", e)
	}
	raw, e := store.Release("sub", 1)
	if e != nil || !bytes.Contains(raw, []byte("fixture-sensitive-value")) {
		t.Fatal("release roundtrip failed")
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if e = store.Backup(backup); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{path, path + "-wal", backup} {
		b, e := os.ReadFile(p)
		if e == nil && bytes.Contains(b, []byte("fixture-sensitive-value")) {
			t.Fatal("plaintext leaked to storage")
		}
	}
}

func TestWrongMasterKeyRefusesExistingDatabase(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	box, _ := secret.NewBox(key)
	path := filepath.Join(t.TempDir(), "hub.db")
	first, e := Open(path, box)
	if e != nil {
		t.Fatal(e)
	}
	first.Close()
	key[0] ^= 1
	wrong, _ := secret.NewBox(key)
	second, e := Open(path, wrong)
	if e == nil {
		second.Close()
		t.Fatal("wrong master key accepted an existing database")
	}
}
