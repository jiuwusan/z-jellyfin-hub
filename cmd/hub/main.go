package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"github.com/jiuwusan/z-jellyfin-hub/internal/httpapi"
	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
	"github.com/jiuwusan/z-jellyfin-hub/internal/service"
	"github.com/jiuwusan/z-jellyfin-hub/internal/storage"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
func store() (*storage.Store, error) {
	raw, e := os.ReadFile(masterKeyPath())
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
	if e := prepareMasterKey(); e != nil {
		return e
	}
	address := env("HUB_LISTEN", ":8097")
	db, e := store()
	if e != nil {
		return e
	}
	defer db.Close()
	if e = prepareSetup(db); e != nil {
		return e
	}
	handler := httpapi.New(service.New(db), httpapi.Options{RequireUI: true, SetupComplete: func() { _ = os.Remove(setupCodePath()) }})
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(done)
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-done:
		case <-finished:
			return
		}
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
	case "setup-code":
		db, e := store()
		if e != nil {
			return e
		}
		defer db.Close()
		if _, e = db.Metadata("admin_password"); e == nil {
			return errors.New("setup is already complete")
		}
		raw, e := os.ReadFile(setupCodePath())
		if e != nil {
			return errors.New("setup code is unavailable; start hub first")
		}
		fmt.Println(strings.TrimSpace(string(raw)))
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
		resp, e := client.Get("http://127.0.0.1:8097/health/ready")
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
