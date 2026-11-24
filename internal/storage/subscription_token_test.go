package storage

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
)

func TestSubscriptionTokenMigrationEncryptionAndBackup(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	box, _ := secret.NewBox(key)
	path := filepath.Join(t.TempDir(), "legacy.db")
	token, _ := secret.Token()
	hash, _ := secret.Digest(token)
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE subscriptions(id TEXT PRIMARY KEY,label TEXT NOT NULL,proxy_id TEXT NOT NULL,credential_id TEXT NOT NULL,policy_id TEXT NOT NULL,enabled INTEGER NOT NULL,token_hash BLOB NOT NULL,revision INTEGER NOT NULL DEFAULT 0,refresh_interval INTEGER NOT NULL,min_client INTEGER NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`INSERT INTO subscriptions VALUES('legacy','Legacy','proxy','credential','policy',1,?,4,900,57)`, hash)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	store, err := Open(path, box)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	legacy, err := store.SubscriptionByToken(hash)
	if err != nil || legacy.HasSavedToken || legacy.Revision != 4 {
		t.Fatal("migration changed legacy binding")
	}
	if err = store.SetEncryptedToken("legacy", token); err != nil {
		t.Fatal(err)
	}
	sealed, _ := store.Subscription("legacy")
	if bytes.Contains(sealed.TokenCipher, []byte(token)) {
		t.Fatal("token stored in clear")
	}
	if got, err := store.SubscriptionToken("legacy"); err != nil || got != token {
		t.Fatal("decrypt failed")
	}
	other, _ := secret.Token()
	otherHash, _ := secret.Digest(other)
	otherCipher, _ := store.SealSubscriptionToken("other", other)
	another := legacy
	another.ID = "other"
	another.TokenHash = otherHash
	another.TokenCipher = otherCipher
	if err = store.SaveSubscription(another); err != nil {
		t.Fatal(err)
	}
	if err = store.SetEncryptedToken("legacy", other); !errors.Is(err, ErrTokenInUse) {
		t.Fatal("duplicate token accepted")
	}
	if got, err := store.SubscriptionToken("legacy"); err != nil || got != token {
		t.Fatal("failed rotation changed stored link")
	}
	if _, err = store.db.Exec("UPDATE subscriptions SET token_cipher=? WHERE id='other'", sealed.TokenCipher); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SubscriptionToken("other"); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal("ciphertext accepted under a different subscription")
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err = store.Backup(backup); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{path, path + "-wal", backup} {
		data, e := os.ReadFile(file)
		if e != nil && !os.IsNotExist(e) {
			t.Fatal(e)
		}
		if bytes.Contains(data, []byte(token)) || bytes.Contains(data, []byte(other)) {
			t.Fatal("database or backup contains clear token")
		}
	}
	store.Close()
	store, err = Open(path, box)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.SubscriptionToken("legacy"); err != nil || got != token {
		t.Fatal("reopen lost link")
	}
	restored, err := Open(backup, box)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got, err := restored.SubscriptionToken("legacy"); err != nil || got != token {
		t.Fatal("backup lost link")
	}
	damaged := append([]byte(nil), sealed.TokenCipher...)
	damaged[len(damaged)-1] ^= 1
	if _, err = store.db.Exec("UPDATE subscriptions SET token_cipher=? WHERE id='legacy'", damaged); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SubscriptionToken("legacy"); !errors.Is(err, secret.ErrInvalid) {
		t.Fatal("tampered ciphertext accepted")
	}
}
