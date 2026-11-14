// Package server implements the gateway's HTTP surface: the WebSocket
// endpoint, health probes and metrics.
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/espiegel/tellwire/gateway/internal/config"
	"github.com/espiegel/tellwire/gateway/internal/protocol"
)

// Server is the gateway.
type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	lim     *limiter
	met     metrics
	rootCAs *x509.CertPool // nil = system roots
	origins map[string]struct{}

	shuttingDown atomic.Bool
	baseCtx      context.Context
	baseCancel   context.CancelFunc
	wg           sync.WaitGroup
}

// New validates cfg-derived resources (CA file) and returns a Server.
func New(cfg *config.Config, log *slog.Logger) (*Server, error) {
	s := &Server{
		cfg:     cfg,
		log:     log,
		lim:     newLimiter(cfg.MaxConnections, cfg.MaxPerIP),
		origins: map[string]struct{}{},
	}
	s.baseCtx, s.baseCancel = context.WithCancel(context.Background())
	for _, o := range cfg.AllowedOrigins {
		s.origins[strings.ToLower(o)] = struct{}{}
	}
	if cfg.UpstreamCAFile != "" {
		pem, err := os.ReadFile(cfg.UpstreamCAFile)
		if err != nil {
			return nil, fmt.Errorf("read GATEWAY_UPSTREAM_CA_FILE: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("GATEWAY_UPSTREAM_CA_FILE %s contains no certificates", cfg.UpstreamCAFile)
		}
		s.rootCAs = pool
	}
	return s, nil
}

// SetRootCAs overrides the upstream trust pool (used by tests).
func (s *Server) SetRootCAs(p *x509.CertPool) { s.rootCAs = p }

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.handleWS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if s.shuttingDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "shutting down")
			return
		}
		fmt.Fprintln(w, "ready")
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		// Not exposed publicly: the Caddy config does not route /metrics, so it
		// is reachable only on the internal Docker network / loopback.
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, ips := s.lim.counts()
		s.met.write(w, s.cfg.MaxConnections, ips)
	})
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// originAllowed requires an Origin header that exactly matches (scheme, host,
// port) one of the configured origins. Requests without Origin (non-browser
// clients) are refused: browsers always send it on WebSocket handshakes.
func (s *Server) originAllowed(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" && u.Path != "/" {
		return false
	}
	_, ok := s.origins[strings.ToLower(u.Scheme+"://"+u.Host)]
	return ok
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.shuttingDown.Load() {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	if !s.originAllowed(r) {
		s.met.end("origin_rejected")
		s.log.Warn("websocket origin rejected", "origin", r.Header.Get("Origin"))
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{protocol.Subprotocol},
		// Origin has already been verified strictly above (scheme+host+port);
		// the library's own check only compares hosts.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept already replied
	}
	ws.SetReadLimit(s.cfg.MaxMessageBytes)

	s.wg.Add(1)
	defer s.wg.Done()
	s.met.total.Add(1)
	s.met.active.Add(1)
	defer s.met.active.Add(-1)

	newSession(s, ws, clientIP(r, s.cfg.TrustedProxies)).run(s.baseCtx)
}

// Shutdown stops accepting sessions, notifies live ones and waits for them to
// finish (or ctx to expire).
func (s *Server) Shutdown(ctx context.Context) error {
	s.shuttingDown.Store(true)
	s.baseCancel()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ActiveSessions reports live sessions (for tests and drain logging).
func (s *Server) ActiveSessions() int { return int(s.met.active.Load()) }

func (s *Server) tlsConfig(u config.Upstream) *tls.Config {
	name := u.ServerName
	if name == "" {
		name = u.Host
	}
	return &tls.Config{ServerName: name, RootCAs: s.rootCAs, MinVersion: tls.VersionTLS12}
}

func (s *Server) dial(ctx context.Context, u config.Upstream) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.DialTimeout)
	defer cancel()
	nd := &net.Dialer{KeepAlive: 30 * time.Second}
	if u.TLS {
		return (&tls.Dialer{NetDialer: nd, Config: s.tlsConfig(u)}).DialContext(ctx, "tcp", u.Addr())
	}
	return nd.DialContext(ctx, "tcp", u.Addr())
}
