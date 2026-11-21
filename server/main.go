// Command copia-server is a small self-hosted clipboard hub.
//
// Devices upload what they copy (text, images or files) and receive new
// items live over a Server-Sent Events stream. Configuration comes from
// environment variables; see loadConfig.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	Addr          string
	DataDir       string
	Token         string
	MaxItems      int
	MaxAge        time.Duration
	MaxUpload     int64
	AutoSyncLimit int64
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// parseSize accepts plain bytes or a KB/MB/GB suffix (binary multiples).
func parseSize(s string) (int64, error) {
	orig := s
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suffix) {
			mult = u.mult
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q", orig)
	}
	return n * mult, nil
}

func loadConfig() (Config, error) {
	home, _ := os.UserHomeDir()
	c := Config{
		Addr:    env("COPIA_ADDR", ":8787"),
		DataDir: env("COPIA_DATA", filepath.Join(home, "copia", "data")),
		Token:   strings.TrimSpace(os.Getenv("COPIA_TOKEN")),
	}
	if len(c.Token) < 16 {
		return c, errors.New("COPIA_TOKEN must be set to at least 16 characters (generate one with: openssl rand -hex 32)")
	}
	var err error
	if c.MaxItems, err = strconv.Atoi(env("COPIA_MAX_ITEMS", "1")); err != nil || c.MaxItems < 1 {
		return c, errors.New("COPIA_MAX_ITEMS must be a positive integer")
	}
	if c.MaxAge, err = time.ParseDuration(env("COPIA_MAX_AGE", "24h")); err != nil || c.MaxAge <= 0 {
		return c, errors.New("COPIA_MAX_AGE must be a positive duration such as 24h or 90m")
	}
	if c.MaxUpload, err = parseSize(env("COPIA_MAX_UPLOAD", "2GB")); err != nil || c.MaxUpload <= 0 {
		return c, fmt.Errorf("COPIA_MAX_UPLOAD: %v", err)
	}
	if c.AutoSyncLimit, err = parseSize(env("COPIA_AUTOSYNC_LIMIT", "20MB")); err != nil {
		return c, fmt.Errorf("COPIA_AUTOSYNC_LIMIT: %v", err)
	}
	return c, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := loadConfig()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	store, err := OpenStore(cfg.DataDir, cfg.MaxItems, cfg.MaxAge)
	if err != nil {
		log.Error("open store", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}
	hub := NewHub()
	srv := &Server{cfg: cfg, store: store, hub: hub, log: log}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No read/write timeouts: uploads can be large and /events is long-lived.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.pruneLoop(ctx)
	go func() {
		<-ctx.Done()
		hub.CloseAll() // ends open /events streams so Shutdown can finish
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sctx)
	}()

	log.Info("copia listening",
		"addr", cfg.Addr, "data", cfg.DataDir, "items", store.Len(),
		"maxItems", cfg.MaxItems, "maxAge", cfg.MaxAge,
		"autoSyncLimit", cfg.AutoSyncLimit, "maxUpload", cfg.MaxUpload)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	log.Info("stopped")
}
