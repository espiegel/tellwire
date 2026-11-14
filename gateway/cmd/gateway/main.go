// Command gateway is the WebSocket-to-Telnet gateway for the Tellwire web client.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/espiegel/tellwire/gateway/internal/config"
	"github.com/espiegel/tellwire/gateway/internal/server"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error:\n%v\n", err)
		os.Exit(2)
	}
	log := newLogger(cfg)
	srv, err := server.New(cfg, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(2)
	}
	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	profiles := make([]string, 0, len(cfg.Upstreams))
	for _, u := range cfg.Upstreams {
		profiles = append(profiles, fmt.Sprintf("%s=%s(tls=%v)", u.Name, u.Addr(), u.TLS))
	}
	log.Info("gateway listening", "version", version, "addr", cfg.Listen, "upstreams", profiles,
		"max_conns", cfg.MaxConnections, "max_per_ip", cfg.MaxPerIP)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		log.Info("shutdown requested; draining", "active", srv.ActiveSessions())
		sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = hs.Shutdown(sctx) // stops the listener; hijacked WebSockets are handled below
		if err := srv.Shutdown(sctx); err != nil {
			log.Warn("drain incomplete", "active", srv.ActiveSessions())
		}
		log.Info("bye")
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: lvl}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// healthcheck is used by the container HEALTHCHECK (distroless has no curl).
func healthcheck() int {
	addr := os.Getenv("GATEWAY_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		return 1
	}
	return 0
}
