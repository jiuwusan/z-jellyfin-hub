package storage

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	_ "modernc.org/sqlite"
	"time"
)

var ErrConflict = errors.New("configuration changed; refresh before publishing")

type Store struct {
	db  *sql.DB
	box *secret.Box
}

// The transaction prevents concurrent setup requests from replacing the first administrator.
func (s *Store) InitializeAdmin(username, passwordHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRow("SELECT value FROM metadata WHERE key='admin_password'").Scan(&existing)
	if err == nil {
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec("INSERT INTO metadata(key,value) VALUES('admin_username',?),('admin_password',?)", username, passwordHash); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM metadata WHERE key='setup_code_hash'"); err != nil {
		return err
	}
	return tx.Commit()
}

type Entity struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Label       string          `json:"label"`
	Data        json.RawMessage `json:"data"`
	UpdatedAt   int64           `json:"updatedAt"`
	HasPassword bool            `json:"hasPassword,omitempty"`
}
type Subscription struct {
	ID                     string `json:"id"`
	Label                  string `json:"label"`
	ProxyID                string `json:"proxyId"`
	CredentialID           string `json:"credentialId"`
	PolicyID               string `json:"policyId"`
	Enabled                bool   `json:"enabled"`
	TokenHash              []byte `json:"-"`
	Revision               int64  `json:"revision"`
	RefreshIntervalSeconds int    `json:"refreshIntervalSeconds"`
	MinClientVersionCode   int    `json:"minClientVersionCode"`
}
type Version struct {
	Revision  int64  `json:"revision"`
	CreatedAt int64  `json:"createdAt"`
	Note      string `json:"note"`
}
type Feedback struct {
	DeviceID    string `json:"deviceId"`
	Platform    string `json:"platform"`
	VersionCode int    `json:"versionCode"`
	Revision    int64  `json:"revision"`
	Status      string `json:"status"`
	ErrorCode   string `json:"errorCode"`
	SeenAt      int64  `json:"seenAt"`
}

func Open(path string, box *secret.Box) (*Store, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
 CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS entities(id TEXT PRIMARY KEY,kind TEXT NOT NULL,label TEXT NOT NULL,payload BLOB NOT NULL,updated INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS subscriptions(id TEXT PRIMARY KEY,label TEXT NOT NULL,proxy_id TEXT NOT NULL,credential_id TEXT NOT NULL,policy_id TEXT NOT NULL,enabled INTEGER NOT NULL,token_hash BLOB NOT NULL,revision INTEGER NOT NULL DEFAULT 0,refresh_interval INTEGER NOT NULL,min_client INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS releases(subscription_id TEXT NOT NULL,revision INTEGER NOT NULL,payload BLOB NOT NULL,created INTEGER NOT NULL,note TEXT NOT NULL,PRIMARY KEY(subscription_id,revision),FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE);
 CREATE TABLE IF NOT EXISTS feedback(subscription_id TEXT NOT NULL,device_id TEXT NOT NULL,platform TEXT NOT NULL,version_code INTEGER NOT NULL,revision INTEGER NOT NULL,status TEXT NOT NULL,error_code TEXT NOT NULL,seen_at INTEGER NOT NULL,PRIMARY KEY(subscription_id,device_id),FOREIGN KEY(subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	store := &Store{db, box}
	if err := store.checkKey(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}
func (s *Store) checkKey() error {
	encoded, e := s.Metadata("key_check")
	if e == nil {
		sealed, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return secret.ErrInvalid
		}
		plain, err := s.box.Open("database-key-check", sealed)
		if err != nil || string(plain) != "z-jellyfin-hub" {
			return secret.ErrInvalid
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	// Older schemas can be upgraded only after validating an existing encrypted record.
	var id, kind string
	var payload []byte
	err := s.db.QueryRow("SELECT id,kind,payload FROM entities LIMIT 1").Scan(&id, &kind, &payload)
	if err == nil {
		if _, e = s.box.Open("entity:"+kind+":"+id, payload); e != nil {
			return e
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	} else {
		var revision int64
		err = s.db.QueryRow("SELECT subscription_id,revision,payload FROM releases LIMIT 1").Scan(&id, &revision, &payload)
		if err == nil {
			if _, e = s.box.Open(fmt.Sprintf("release:%s:%d", id, revision), payload); e != nil {
				return e
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	sealed, e := s.box.Seal("database-key-check", []byte("z-jellyfin-hub"))
	if e != nil {
		return e
	}
	return s.SetMetadata("key_check", base64.StdEncoding.EncodeToString(sealed))
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Metadata(key string) (string, error) {
	var v string
	e := s.db.QueryRow("SELECT value FROM metadata WHERE key=?", key).Scan(&v)
	return v, e
}
func (s *Store) SetMetadata(key, value string) error {
	_, e := s.db.Exec("INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return e
}
func (s *Store) PutEntity(v Entity) error {
	encrypted, e := s.box.Seal("entity:"+v.Kind+":"+v.ID, v.Data)
	if e != nil {
		return e
	}
	res, e := s.db.Exec(`INSERT INTO entities(id,kind,label,payload,updated) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET label=excluded.label,payload=excluded.payload,updated=excluded.updated WHERE entities.kind=excluded.kind`, v.ID, v.Kind, v.Label, encrypted, time.Now().Unix())
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func (s *Store) Entity(kind, id string) (Entity, error) {
	var v Entity
	var encrypted []byte
	e := s.db.QueryRow("SELECT id,kind,label,payload,updated FROM entities WHERE kind=? AND id=?", kind, id).Scan(&v.ID, &v.Kind, &v.Label, &encrypted, &v.UpdatedAt)
	if e != nil {
		return v, e
	}
	v.Data, e = s.box.Open("entity:"+kind+":"+id, encrypted)
	return v, e
}
func (s *Store) Entities(kind string) ([]Entity, error) {
	rows, e := s.db.Query("SELECT id,kind,label,payload,updated FROM entities WHERE kind=? ORDER BY updated DESC,id", kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Entity{}
	for rows.Next() {
		var v Entity
		var encrypted []byte
		if e = rows.Scan(&v.ID, &v.Kind, &v.Label, &encrypted, &v.UpdatedAt); e != nil {
			return nil, e
		}
		v.Data, e = s.box.Open("entity:"+v.Kind+":"+v.ID, encrypted)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) DeleteEntity(kind, id string) error {
	_, e := s.db.Exec("DELETE FROM entities WHERE kind=? AND id=?", kind, id)
	return e
}
func (s *Store) SaveSubscription(v Subscription) error {
	_, e := s.db.Exec(`INSERT INTO subscriptions(id,label,proxy_id,credential_id,policy_id,enabled,token_hash,revision,refresh_interval,min_client) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET label=excluded.label,proxy_id=excluded.proxy_id,credential_id=excluded.credential_id,policy_id=excluded.policy_id,enabled=excluded.enabled,token_hash=excluded.token_hash,refresh_interval=excluded.refresh_interval,min_client=excluded.min_client`, v.ID, v.Label, v.ProxyID, v.CredentialID, v.PolicyID, v.Enabled, v.TokenHash, v.Revision, v.RefreshIntervalSeconds, v.MinClientVersionCode)
	return e
}

const subColumns = "id,label,proxy_id,credential_id,policy_id,enabled,token_hash,revision,refresh_interval,min_client"

type scanner interface{ Scan(...any) error }

func scanSub(row scanner) (Subscription, error) {
	var v Subscription
	e := row.Scan(&v.ID, &v.Label, &v.ProxyID, &v.CredentialID, &v.PolicyID, &v.Enabled, &v.TokenHash, &v.Revision, &v.RefreshIntervalSeconds, &v.MinClientVersionCode)
	return v, e
}
func (s *Store) Subscription(id string) (Subscription, error) {
	return scanSub(s.db.QueryRow("SELECT "+subColumns+" FROM subscriptions WHERE id=?", id))
}
func (s *Store) Subscriptions() ([]Subscription, error) {
	rows, e := s.db.Query("SELECT " + subColumns + " FROM subscriptions ORDER BY label,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		v, e := scanSub(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) DeleteSubscription(id string) error {
	_, e := s.db.Exec("DELETE FROM subscriptions WHERE id=?", id)
	return e
}
func (s *Store) Publish(id string, expected int64, plain []byte, note string) (int64, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var current int64
	if e = tx.QueryRow("SELECT revision FROM subscriptions WHERE id=?", id).Scan(&current); e != nil {
		return 0, e
	}
	if current != expected {
		return 0, ErrConflict
	}
	next := current + 1
	encrypted, e := s.box.Seal(fmt.Sprintf("release:%s:%d", id, next), plain)
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec("INSERT INTO releases(subscription_id,revision,payload,created,note) VALUES(?,?,?,?,?)", id, next, encrypted, time.Now().Unix(), note); e != nil {
		return 0, e
	}
	if _, e = tx.Exec("UPDATE subscriptions SET revision=? WHERE id=? AND revision=?", next, id, current); e != nil {
		return 0, e
	}
	return next, tx.Commit()
}
func (s *Store) Release(id string, revision int64) ([]byte, error) {
	var encrypted []byte
	e := s.db.QueryRow("SELECT payload FROM releases WHERE subscription_id=? AND revision=?", id, revision).Scan(&encrypted)
	if e != nil {
		return nil, e
	}
	return s.box.Open(fmt.Sprintf("release:%s:%d", id, revision), encrypted)
}
func (s *Store) Versions(id string) ([]Version, error) {
	rows, e := s.db.Query("SELECT revision,created,note FROM releases WHERE subscription_id=? ORDER BY revision DESC", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if e = rows.Scan(&v.Revision, &v.CreatedAt, &v.Note); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) PutFeedback(id string, v Feedback) error {
	_, e := s.db.Exec(`INSERT INTO feedback(subscription_id,device_id,platform,version_code,revision,status,error_code,seen_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(subscription_id,device_id) DO UPDATE SET platform=excluded.platform,version_code=excluded.version_code,revision=excluded.revision,status=excluded.status,error_code=excluded.error_code,seen_at=excluded.seen_at`, id, v.DeviceID, v.Platform, v.VersionCode, v.Revision, v.Status, v.ErrorCode, time.Now().Unix())
	if e != nil {
		return e
	}
	_, e = s.db.Exec("DELETE FROM feedback WHERE subscription_id=? AND device_id NOT IN (SELECT device_id FROM feedback WHERE subscription_id=? ORDER BY seen_at DESC,device_id LIMIT 50)", id, id)
	return e
}
func (s *Store) Feedback(id string) ([]Feedback, error) {
	rows, e := s.db.Query("SELECT device_id,platform,version_code,revision,status,error_code,seen_at FROM feedback WHERE subscription_id=? ORDER BY seen_at DESC LIMIT 50", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Feedback{}
	for rows.Next() {
		var v Feedback
		if e = rows.Scan(&v.DeviceID, &v.Platform, &v.VersionCode, &v.Revision, &v.Status, &v.ErrorCode, &v.SeenAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Backup(path string) error { _, e := s.db.Exec("VACUUM INTO ?", path); return e }
