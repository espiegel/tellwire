package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/espiegel/tellwire/gateway/internal/config"
	"github.com/espiegel/tellwire/gateway/internal/mockmud"
	"github.com/espiegel/tellwire/gateway/internal/protocol"
)

const testOrigin = "http://localhost:5173"

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// startMock runs a mock MUD on a random loopback port.
func startMock(t *testing.T, mut func(*mockmud.Server)) (string, *mockmud.Server) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return serveMock(t, ln, mut)
}

func serveMock(t *testing.T, ln net.Listener, mut func(*mockmud.Server)) (string, *mockmud.Server) {
	m := &mockmud.Server{}
	if mut != nil {
		mut(m)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = m.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return ln.Addr().String(), m
}

type gw struct {
	*Server
	URL  string // ws://host/ws
	Logs *syncBuf
}

// startGateway runs a gateway whose default profile points at addr.
func startGateway(t *testing.T, addr string, useTLS bool, mut func(*config.Config)) *gw {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	var p int
	fmt.Sscan(port, &p)
	cfg := &config.Config{
		Listen:          "127.0.0.1:0",
		AllowedOrigins:  []string{testOrigin},
		Upstreams:       []config.Upstream{{Name: "default", Host: host, Port: p, TLS: useTLS}},
		MaxConnections:  500,
		MaxPerIP:        100,
		IdleTimeout:     time.Minute,
		DialTimeout:     2 * time.Second,
		WriteTimeout:    5 * time.Second,
		PingInterval:    time.Minute,
		ShutdownTimeout: 5 * time.Second,
		MaxMessageBytes: 16 * 1024,
		MaxSubnegBytes:  64 * 1024,
		OutQueue:        64,
		InputLinesPerS:  0,
		InputBurst:      40,
		GMCP:            true,
		GMCPHello:       true,
		GMCPSupports:    []string{"Char 1", "Room 1", "Comm.Channel 1"},
		ClientName:      "Tellwire",
		ClientVer:       "test",
	}
	if mut != nil {
		mut(cfg)
	}
	logs := &syncBuf{}
	s, err := New(cfg, slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		hs.Close()
	})
	return &gw{Server: s, URL: "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws", Logs: logs}
}

// ---- WebSocket test client ----

type rec struct {
	binary bool
	data   []byte
}

type client struct {
	t      *testing.T
	c      *websocket.Conn
	in     chan rec
	closed chan struct{}
	text   bytes.Buffer // accumulated terminal output
	events []map[string]any
	cerr   error // set after closed
}

func dialWith(t *testing.T, url, origin string, subs []string) (*websocket.Conn, *http.Response, error) {
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h, Subprotocols: subs})
}

func dial(t *testing.T, g *gw) *client {
	t.Helper()
	c, _, err := dialWith(t, g.URL, testOrigin, []string{protocol.Subprotocol})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return newClient(t, c)
}

func newClient(t *testing.T, c *websocket.Conn) *client {
	c.SetReadLimit(1 << 24)
	cl := &client{t: t, c: c, in: make(chan rec, 1<<16), closed: make(chan struct{})}
	go func() {
		defer close(cl.closed)
		for {
			typ, data, err := c.Read(context.Background())
			if err != nil {
				cl.cerr = err
				return
			}
			cl.in <- rec{typ == websocket.MessageBinary, data}
		}
	}()
	t.Cleanup(func() { c.CloseNow() })
	return cl
}

func (cl *client) send(v any) {
	cl.t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cl.c.Write(ctx, websocket.MessageText, b); err != nil {
		cl.t.Fatalf("send: %v", err)
	}
}

func (cl *client) connect(cols, rows int) {
	cl.send(map[string]any{"v": 1, "type": "session.connect", "cols": cols, "rows": rows})
}
func (cl *client) input(s string) {
	cl.send(map[string]any{"v": 1, "type": "terminal.input", "data": s})
}

func (cl *client) absorb(r rec) {
	if r.binary {
		cl.text.Write(r.data)
		return
	}
	var m map[string]any
	if err := json.Unmarshal(r.data, &m); err != nil {
		cl.t.Fatalf("bad JSON from gateway: %q", r.data)
	}
	cl.events = append(cl.events, m)
}

// waitFor consumes frames until cond() is true.
func (cl *client) waitFor(what string, cond func() bool) {
	cl.t.Helper()
	deadline := time.After(8 * time.Second)
	for !cond() {
		select {
		case r := <-cl.in:
			cl.absorb(r)
		case <-cl.closed:
			// drain what is already buffered
			for {
				select {
				case r := <-cl.in:
					cl.absorb(r)
				default:
					if cond() {
						return
					}
					cl.t.Fatalf("connection closed (%v) while waiting for %s\ntext=%q\nevents=%v", cl.cerr, what, cl.text.String(), cl.events)
				}
			}
		case <-deadline:
			cl.t.Fatalf("timeout waiting for %s\ntext=%q\nevents=%v", what, cl.text.String(), cl.events)
		}
	}
}

func (cl *client) waitText(sub string) {
	cl.t.Helper()
	cl.waitFor("text "+sub, func() bool { return strings.Contains(cl.text.String(), sub) })
}

func (cl *client) hasEvent(typ string, kv ...any) bool {
	for _, e := range cl.events {
		if e["type"] != typ {
			continue
		}
		ok := true
		for i := 0; i+1 < len(kv); i += 2 {
			if fmt.Sprint(e[kv[i].(string)]) != fmt.Sprint(kv[i+1]) {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (cl *client) waitEvent(typ string, kv ...any) {
	cl.t.Helper()
	cl.waitFor(fmt.Sprintf("event %s %v", typ, kv), func() bool { return cl.hasEvent(typ, kv...) })
}

func (cl *client) waitClosed() {
	cl.t.Helper()
	deadline := time.After(8 * time.Second)
	for {
		select {
		case r := <-cl.in:
			cl.absorb(r)
		case <-cl.closed:
			for {
				select {
				case r := <-cl.in:
					cl.absorb(r)
				default:
					return
				}
			}
		case <-deadline:
			cl.t.Fatalf("connection not closed\nevents=%v", cl.events)
		}
	}
}

// login connects and logs in with the given name; the password is returned
// for the caller's assertions.
func (cl *client) login(name, password string) {
	cl.t.Helper()
	cl.connect(100, 30)
	cl.waitText("By what name")
	cl.input(name + "\n")
	cl.waitText("Password:")
	cl.input(password + "\n")
	cl.waitText("HP: [")
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

// selfSignedCert makes a throwaway server certificate for the given names.
func selfSignedCert(t *testing.T, dns []string, ips []net.IP) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: dns, IPAddresses: ips, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}
