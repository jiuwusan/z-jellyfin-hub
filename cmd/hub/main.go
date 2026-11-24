package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"github.com/jiuwusan/z-jellyfin-hub/internal/httpapi"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func initialize(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	for _, name := range []string{"hub.key", "admin.password"} {
		if _, e := os.Stat(filepath.Join(dir, name)); !errors.Is(e, os.ErrNotExist) {
			return errors.New("secret files already exist; refusing to overwrite")
		}
	}
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return e
	}
	password, e := secret.Token()
	if e != nil {
		return e
	}
	values := map[string]string{"hub.key": base64.StdEncoding.EncodeToString(key) + "\n", "admin.password": password + "\n"}
	for name, value := range values {
		path := filepath.Join(dir, name)
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.WriteString(value)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if runtime.GOOS != "windows" && os.Geteuid() == 0 {
			if e = os.Chown(path, 10001, 10001); e != nil {
				return e
			}
		}
	}
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		return os.Chown(dir, 10001, 10001)
	}
	return nil
}
func store() (*storage.Store, error) {
	raw, e := os.ReadFile(env("HUB_MASTER_KEY_FILE", "/run/secrets/hub_master_key"))
	if e != nil {
		return nil, errors.New("master key file is unavailable")
	}
	key, e := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(raw)))
	if e != nil {
		return nil, errors.New("master key format is invalid")
	}
	box, e := secret.NewBox(key)
	if e != nil {
		return nil, e
	}
	dir := env("HUB_DATA_DIR", "/data")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	return storage.Open(filepath.Join(dir, "hub.db"), box)
}
func serve() error {
	public := os.Getenv("HUB_PUBLIC_URL")
	u, e := url.Parse(public)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("HUB_PUBLIC_URL must be an HTTPS base URL")
	}
	dev := os.Getenv("HUB_DEV_ALLOW_HTTP") == "true"
	address := env("HUB_LISTEN", ":8080")
	if dev {
		host, _, e := net.SplitHostPort(address)
		if e != nil || host != "127.0.0.1" && host != "::1" || u.Scheme != "http" || u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
			return errors.New("development HTTP requires a loopback listen and public URL")
		}
	} else if u.Scheme != "https" {
		return errors.New("production requires HTTPS public URL")
	}
	db, e := store()
	if e != nil {
		return e
	}
	defer db.Close()
	if _, e = db.Metadata("admin_password"); errors.Is(e, sql.ErrNoRows) {
		password, e := os.ReadFile(env("HUB_ADMIN_PASSWORD_FILE", "/run/secrets/hub_admin_password"))
		if e != nil {
			return errors.New("initial administrator password file is unavailable")
		}
		hash, e := secret.HashPassword(strings.TrimSpace(string(password)))
		if e != nil {
			return errors.New("initial administrator password must contain 12 to 1024 characters")
		}
		username := env("HUB_ADMIN_USER", "admin")
		if e = db.SetMetadata("admin_username", username); e != nil {
			return e
		}
		if e = db.SetMetadata("admin_password", hash); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	handler := httpapi.New(service.New(db, public), httpapi.Options{Dev: dev, RequireUI: true})
	prefix := strings.TrimRight(u.Path, "/")
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if prefix != "" && strings.HasPrefix(r.URL.Path, prefix+"/") {
			r = r.Clone(r.Context())
			next := *r.URL
			next.Path = strings.TrimPrefix(next.Path, prefix)
			r.URL = &next
		}
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: address, Handler: wrapped, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(done)
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	log.Print("z-jellyfin-hub started")
	e = server.ListenAndServe()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "init":
		flags := flag.NewFlagSet("init", flag.ContinueOnError)
		dir := flags.String("secrets-dir", "/secrets", "secret directory")
		if e := flags.Parse(os.Args[2:]); e != nil {
			return e
		}
		if e := initialize(*dir); e != nil {
			return e
		}
		fmt.Println("Secret files generated. Keep them outside Git and back them up separately.")
		return nil
	case "backup":
		flags := flag.NewFlagSet("backup", flag.ContinueOnError)
		out := flags.String("output", "", "new backup database file")
		if e := flags.Parse(os.Args[2:]); e != nil {
			return e
		}
		if *out == "" {
			return errors.New("backup output is required")
		}
		db, e := store()
		if e != nil {
			return e
		}
		defer db.Close()
		return db.Backup(*out)
	case "health":
		client := &http.Client{Timeout: 3 * time.Second}
		resp, e := client.Get("http://127.0.0.1:8080/health/ready")
		if e != nil {
			return errors.New("not ready")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("not ready")
		}
		return nil
	case "serve":
		return serve()
	default:
		return errors.New("unknown command")
	}
}
func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
