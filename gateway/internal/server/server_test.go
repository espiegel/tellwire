package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/espiegel/tellwire/gateway/internal/config"
	"github.com/espiegel/tellwire/gateway/internal/mockmud"
	"github.com/espiegel/tellwire/gateway/internal/protocol"
)

func TestEndToEndLoginRoomAndANSI(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.connect(100, 30)
	cl.waitEvent("connection.status", "state", "connected")
	cl.waitText("By what name")
	cl.input("tester\n")
	cl.waitText("Password:")
	cl.input("hunter2\n")
	cl.waitText("Arrakeen Shuttle Landing Port")
	// ANSI escapes survive byte-for-byte.
	cl.waitText("\x1b[33mgolden sand\x1b[0m")
	// Emoji (multi-byte UTF-8) survive.
	cl.input("unicode\n")
	cl.waitText("🪱")
	// No Telnet command bytes leak into the terminal stream.
	if bytes.IndexByte(cl.text.Bytes(), 0xff) >= 0 {
		t.Fatal("IAC byte leaked into terminal output")
	}
	// Every newline in the output is CR LF.
	out := cl.text.String()
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Fatal("bare LF in terminal output")
	}
}

func TestPasswordMaskingAndNeverLogged(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.connect(80, 24)
	cl.waitText("By what name")
	cl.input("tester\n")
	// Server sends WILL ECHO: gateway tells the browser to stop echoing/mask.
	cl.waitEvent("session.echo", "enabled", "false")
	cl.waitText("Password:")
	cl.input("s3cr3t-pa55w0rd\n")
	cl.waitText("HP: [")
	// ...and to resume afterwards. The last echo event must be enabled=true.
	var echoes []string
	for _, e := range cl.events {
		if e["type"] == "session.echo" {
			echoes = append(echoes, fmt.Sprint(e["enabled"]))
		}
	}
	if got := strings.Join(echoes, ","); got != "true,false,true" {
		t.Fatalf("echo sequence = %s, want true,false,true", got)
	}
	if strings.Contains(cl.text.String(), "s3cr3t") {
		t.Fatal("password echoed in terminal output")
	}
	cl.input("quit\n")
	cl.waitClosed()
	if strings.Contains(g.Logs.String(), "s3cr3t") || strings.Contains(g.Logs.String(), "tester") {
		t.Fatalf("sensitive input found in logs:\n%s", g.Logs.String())
	}
}

func TestGMCPNegotiationAndForwarding(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.login("tester", "pw")
	cl.waitEvent("gmcp.message", "package", "Room.Info")
	cl.waitEvent("gmcp.message", "package", "Char.Vitals")
	cl.waitEvent("gmcp.message", "package", "Comm.Channel.List")
	// The mock only sends GMCP if the gateway opted in via Core.Supports.Set.
	cl.input("gmcp\n")
	cl.waitText("client-opted-in=true")
	// Payload preserved, compact JSON, unknown packages untouched.
	for _, e := range cl.events {
		if e["type"] == "gmcp.message" && e["package"] == "Room.Info" {
			d := e["data"].(map[string]any)
			if d["name"] != "Arrakeen Shuttle Landing Port" || d["area"] != "arrakis" {
				t.Fatalf("room data %v", d)
			}
			if len(d["exits"].(map[string]any)) != 2 {
				t.Fatalf("exits %v", d["exits"])
			}
		}
	}
	// Moving sends a new Room.Info; a fight sends a delta Char.Vitals with only hp+string.
	cl.input("north\n")
	cl.waitFor("second room info", func() bool {
		n := 0
		for _, e := range cl.events {
			if e["package"] == "Room.Info" {
				n++
			}
		}
		return n == 2
	})
	cl.input("hit\n")
	cl.waitFor("vitals delta", func() bool {
		for _, e := range cl.events {
			if e["package"] == "Char.Vitals" {
				if d, ok := e["data"].(map[string]any); ok && len(d) == 2 && d["hp"] == float64(355) {
					return true
				}
			}
		}
		return false
	})
}

func TestGMCPDisabledByConfig(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, func(c *config.Config) { c.GMCP = false })
	cl := dial(t, g)
	cl.login("tester", "pw")
	cl.input("gmcp\n")
	cl.waitText("offered=false client-opted-in=false")
	if cl.hasEvent("gmcp.message") {
		t.Fatal("GMCP forwarded while disabled")
	}
}

func TestTerminalTypeAndWindowSize(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.login("tester", "pw")
	cl.input("term\n")
	cl.waitText("TTYPE: TELLWIRE, XTERM-256COLOR, MTTS 13 | size: 100x30")
	cl.send(map[string]any{"v": 1, "type": "terminal.resize", "cols": 132, "rows": 43})
	time.Sleep(100 * time.Millisecond)
	cl.input("term\n")
	cl.waitText("size: 132x43")
}

func TestLineEndingNormalisation(t *testing.T) {
	for name, nl := range map[string]string{"lf": "\n", "lfcr": "\n\r", "crlf": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			addr, _ := startMock(t, func(m *mockmud.Server) { m.Newline = nl })
			g := startGateway(t, addr, false, nil)
			cl := dial(t, g)
			cl.login("tester", "pw")
			cl.input("look\n")
			cl.waitText("Exits:")
			out := cl.text.String()
			if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") || strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\r") {
				t.Fatalf("line endings not normalised to CRLF: %q", out)
			}
		})
	}
}

func TestOriginValidation(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cases := []struct {
		name, origin string
		ok           bool
	}{
		{"allowed", testOrigin, true},
		{"allowed uppercase host", "http://LOCALHOST:5173", true},
		{"missing", "", false},
		{"other host", "http://evil.example", false},
		{"wrong port", "http://localhost:9999", false},
		{"wrong scheme", "https://localhost:5173", false},
		{"suffix trick", "http://localhost:5173.evil.example", false},
		{"null", "null", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws, resp, err := dialWith(t, g.URL, c.origin, []string{protocol.Subprotocol})
			if c.ok {
				if err != nil {
					t.Fatalf("rejected: %v", err)
				}
				ws.CloseNow()
				return
			}
			if err == nil {
				ws.CloseNow()
				t.Fatal("origin accepted")
			}
			if resp == nil || resp.StatusCode != http.StatusForbidden {
				t.Fatalf("want 403, got %v", resp)
			}
		})
	}
}

func TestSubprotocolRequired(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	ws, _, err := dialWith(t, g.URL, testOrigin, nil)
	if err != nil {
		t.Fatal(err)
	}
	cl := newClient(t, ws)
	cl.waitClosed()
	if websocket.CloseStatus(cl.cerr) != websocket.StatusPolicyViolation {
		t.Fatalf("close status %v", cl.cerr)
	}
}

func TestClientCannotChooseUpstream(t *testing.T) {
	addr, mock := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	for _, prof := range []string{"evil.example:22", "127.0.0.1:1", "../etc", "mock"} {
		cl := dial(t, g)
		cl.send(map[string]any{"v": 1, "type": "session.connect", "profile": prof})
		cl.waitEvent("connection.error", "code", protocol.ErrUnknownProfile)
		cl.waitClosed()
	}
	// Unknown JSON fields such as host/port are ignored, never dialled.
	cl := dial(t, g)
	cl.send(map[string]any{"v": 1, "type": "session.connect", "host": "evil.example", "port": 22})
	cl.waitEvent("connection.status", "state", "connected")
	if mock.Accepted() != 1 {
		t.Fatalf("expected exactly the configured upstream to be dialled, accepted=%d", mock.Accepted())
	}
}

func TestBadFirstMessagesAndBinaryFrames(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)

	cl := dial(t, g)
	cl.input("look\n") // not session.connect
	cl.waitEvent("connection.error", "code", protocol.ErrBadRequest)
	cl.waitClosed()

	ws, _, _ := dialWith(t, g.URL, testOrigin, []string{protocol.Subprotocol})
	cl = newClient(t, ws)
	_ = ws.Write(context.Background(), websocket.MessageText, []byte("not json"))
	cl.waitEvent("connection.error", "code", protocol.ErrBadRequest)

	ws, _, _ = dialWith(t, g.URL, testOrigin, []string{protocol.Subprotocol})
	cl = newClient(t, ws)
	_ = ws.Write(context.Background(), websocket.MessageText, []byte(`{"v":2,"type":"session.connect"}`))
	cl.waitEvent("connection.error", "code", protocol.ErrBadRequest)

	// After connecting, a binary frame from the browser is a protocol violation.
	cl = dial(t, g)
	cl.connect(80, 24)
	cl.waitEvent("connection.status", "state", "connected")
	_ = cl.c.Write(context.Background(), websocket.MessageBinary, []byte("raw"))
	cl.waitEvent("connection.error", "code", protocol.ErrBadRequest)
	cl.waitClosed()
}

func TestSessionIsolationConcurrent(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cl := dial(t, g)
			name := fmt.Sprintf("player%02d", i)
			cl.login(name, "pw")
			for j := 0; j < 5; j++ {
				cl.input(fmt.Sprintf("echo token-%s-%d\n", name, j))
				cl.waitText(fmt.Sprintf("token-%s-%d", name, j))
			}
			cl.input("quit\n")
			cl.waitClosed()
			out := cl.text.String()
			for k := 0; k < n; k++ {
				if k != i && strings.Contains(out, fmt.Sprintf("player%02d", k)) {
					errs <- fmt.Sprintf("session %d saw data from session %d", i, k)
				}
			}
			if !strings.Contains(out, "Welcome, "+name) {
				errs <- "missing own welcome for " + name
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	eventually(t, "all sessions to be released", func() bool { return g.ActiveSessions() == 0 })
}

func TestUpstreamQuitClosesBrowserCleanly(t *testing.T) {
	addr, mock := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.login("tester", "pw")
	cl.input("quit\n")
	cl.waitText("Farewell")
	cl.waitEvent("connection.status", "state", "disconnected")
	cl.waitClosed()
	if websocket.CloseStatus(cl.cerr) != websocket.StatusNormalClosure {
		t.Fatalf("close status %v", cl.cerr)
	}
	eventually(t, "mock to see connection closed", func() bool { return mock.Active() == 0 })
}

func TestUpstreamCrashReported(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.login("tester", "pw")
	cl.input("crash\n")
	cl.waitEvent("connection.status", "state", "disconnected")
	cl.waitClosed()
}

func TestBrowserDisconnectClosesUpstream(t *testing.T) {
	addr, mock := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	// Explicit session.disconnect.
	cl := dial(t, g)
	cl.login("tester", "pw")
	eventually(t, "upstream open", func() bool { return mock.Active() == 1 })
	cl.send(map[string]any{"v": 1, "type": "session.disconnect"})
	cl.waitEvent("connection.status", "state", "disconnected")
	cl.waitClosed()
	eventually(t, "upstream closed after disconnect", func() bool { return mock.Active() == 0 })
	// Abrupt browser close (tab closed).
	cl = dial(t, g)
	cl.login("tester2", "pw")
	eventually(t, "upstream open again", func() bool { return mock.Active() == 1 })
	cl.c.CloseNow()
	eventually(t, "upstream closed after abrupt close", func() bool { return mock.Active() == 0 })
	eventually(t, "sessions released", func() bool { return g.ActiveSessions() == 0 })
}

func TestReconnectGetsFreshIndependentSession(t *testing.T) {
	addr, mock := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	for i := 0; i < 3; i++ {
		cl := dial(t, g)
		cl.login(fmt.Sprintf("round%d", i), "pw")
		cl.waitText("Welcome, " + fmt.Sprintf("round%d", i))
		cl.input("quit\n")
		cl.waitClosed()
	}
	if mock.Accepted() != 3 {
		t.Fatalf("accepted=%d", mock.Accepted())
	}
}

func TestUpstreamUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // nothing listens here any more
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.connect(80, 24)
	cl.waitEvent("connection.error", "code", protocol.ErrUpstreamDial)
	cl.waitClosed()
	if strings.Contains(fmt.Sprint(cl.events), "127.0.0.1") {
		t.Fatal("upstream address leaked to the browser")
	}
}

func TestUpstreamTLS(t *testing.T) {
	cert, pool := selfSignedCert(t, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	_, otherPool := selfSignedCert(t, []string{"localhost"}, nil)
	rawLn, _ := net.Listen("tcp", "127.0.0.1:0")
	ln := tls.NewListener(rawLn, &tls.Config{Certificates: []tls.Certificate{cert}})
	addr, _ := serveMock(t, ln, nil)

	t.Run("trusted certificate connects", func(t *testing.T) {
		g := startGateway(t, addr, true, nil)
		g.SetRootCAs(pool)
		cl := dial(t, g)
		cl.login("tester", "pw")
	})
	t.Run("unknown authority is refused", func(t *testing.T) {
		g := startGateway(t, addr, true, nil) // system roots only
		cl := dial(t, g)
		cl.connect(80, 24)
		cl.waitEvent("connection.error", "code", protocol.ErrUpstreamTLS)
		cl.waitClosed()
		if strings.Contains(cl.text.String(), "By what name") {
			t.Fatal("received MUD output over an unverified connection")
		}
	})
	t.Run("different CA is refused", func(t *testing.T) {
		g := startGateway(t, addr, true, nil)
		g.SetRootCAs(otherPool)
		cl := dial(t, g)
		cl.connect(80, 24)
		cl.waitEvent("connection.error", "code", protocol.ErrUpstreamTLS)
	})
	t.Run("hostname mismatch is refused", func(t *testing.T) {
		wrongCert, wrongPool := selfSignedCert(t, []string{"other.example"}, nil)
		raw, _ := net.Listen("tcp", "127.0.0.1:0")
		a, _ := serveMock(t, tls.NewListener(raw, &tls.Config{Certificates: []tls.Certificate{wrongCert}}), nil)
		g := startGateway(t, a, true, nil)
		g.SetRootCAs(wrongPool) // trusted CA, but the cert is for another name
		cl := dial(t, g)
		cl.connect(80, 24)
		cl.waitEvent("connection.error", "code", protocol.ErrUpstreamTLS)
	})
	t.Run("plaintext server on TLS profile is refused", func(t *testing.T) {
		plain, _ := startMock(t, nil)
		g := startGateway(t, plain, true, nil)
		cl := dial(t, g)
		cl.connect(80, 24)
		cl.waitFor("tls error", func() bool { return cl.hasEvent("connection.error") })
		if cl.hasEvent("connection.status", "state", "connected") {
			t.Fatal("connected over a non-TLS server")
		}
	})
}

func TestConnectionLimits(t *testing.T) {
	addr, _ := startMock(t, nil)
	t.Run("per IP", func(t *testing.T) {
		g := startGateway(t, addr, false, func(c *config.Config) { c.MaxPerIP = 2 })
		a, b := dial(t, g), dial(t, g)
		a.connect(80, 24)
		b.connect(80, 24)
		a.waitEvent("connection.status", "state", "connected")
		b.waitEvent("connection.status", "state", "connected")
		c := dial(t, g)
		c.connect(80, 24)
		c.waitEvent("connection.error", "code", protocol.ErrLimitIP)
		c.waitClosed()
		// Freeing a slot lets a new session in.
		a.send(map[string]any{"v": 1, "type": "session.disconnect"})
		a.waitClosed()
		eventually(t, "slot released", func() bool { n, _ := g.lim.counts(); return n == 1 })
		d := dial(t, g)
		d.connect(80, 24)
		d.waitEvent("connection.status", "state", "connected")
	})
	t.Run("global", func(t *testing.T) {
		g := startGateway(t, addr, false, func(c *config.Config) { c.MaxConnections = 1 })
		a := dial(t, g)
		a.connect(80, 24)
		a.waitEvent("connection.status", "state", "connected")
		b := dial(t, g)
		b.connect(80, 24)
		b.waitEvent("connection.error", "code", protocol.ErrLimitGlobal)
	})
}

func TestOversizedMessageClosesConnection(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, func(c *config.Config) { c.MaxMessageBytes = 1024 })
	cl := dial(t, g)
	cl.login("tester", "pw")
	big := strings.Repeat("A", 4096)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cl.c.Write(ctx, websocket.MessageText, []byte(`{"v":1,"type":"terminal.input","data":"`+big+`"}`))
	cl.waitClosed()
	if websocket.CloseStatus(cl.cerr) != websocket.StatusMessageTooBig {
		t.Fatalf("close status = %v (%v)", websocket.CloseStatus(cl.cerr), cl.cerr)
	}
	eventually(t, "sessions released", func() bool { return g.ActiveSessions() == 0 })
}

// fakeUpstream accepts one connection, runs script, then closes it.
func fakeUpstream(t *testing.T, script func(c net.Conn)) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		script(c)
	}()
	return ln.Addr().String()
}

func TestHostileUpstreamStreamsAreBounded(t *testing.T) {
	// A sub-negotiation far beyond MaxSubnegBytes, before and after GMCP is
	// negotiated. The gateway must discard it, stay in sync and keep working.
	huge := func() []byte {
		b := append([]byte{255, 250, 201}, bytes.Repeat([]byte("x"), 300_000)...)
		return append(b, 255, 240)
	}
	script := func(c net.Conn) {
		c.Write(huge())
		c.Write([]byte("after-first\r\n"))
		c.Write([]byte{255, 251, 201}) // WILL GMCP
		time.Sleep(100 * time.Millisecond)
		c.Write(huge())
		c.Write([]byte("after-second\r\n"))
		time.Sleep(300 * time.Millisecond)
	}
	addr := fakeUpstream(t, script)
	g := startGateway(t, addr, false, func(c *config.Config) { c.MaxSubnegBytes = 1024 })
	cl := dial(t, g)
	cl.connect(80, 24)
	cl.waitText("after-second")
	if got := cl.text.String(); got != "after-first\r\nafter-second\r\n" {
		t.Fatalf("terminal text = %q", got)
	}
	if cl.hasEvent("gmcp.message") {
		t.Fatalf("oversized GMCP was forwarded: %v", cl.events)
	}
}

func TestMalformedGMCPIsForwardedFlagged(t *testing.T) {
	script := func(c net.Conn) {
		c.Write([]byte{255, 251, 201})
		time.Sleep(100 * time.Millisecond) // let the gateway answer DO GMCP
		c.Write([]byte("\xff\xfa\xc9Char.Vitals {\"hp\":\xff\xf0"))
		c.Write([]byte("\xff\xfa\xc9Bad Name {}\xff\xf0"))
		c.Write([]byte("\xff\xfa\xc9Future.Thing {\"a\":[1,2]}\xff\xf0"))
		c.Write([]byte("done\r\n"))
		time.Sleep(300 * time.Millisecond)
	}
	addr := fakeUpstream(t, script)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.connect(80, 24)
	cl.waitText("done")
	cl.waitEvent("gmcp.message", "package", "Char.Vitals", "malformed", true)
	cl.waitEvent("gmcp.message", "package", "Future.Thing")
	cl.waitFor("bad name malformed", func() bool {
		for _, e := range cl.events {
			if e["type"] == "gmcp.message" && e["malformed"] == true && e["package"] == "Bad" {
				return true
			}
		}
		return false
	})
}

func TestFragmentedTelnetOverTCP(t *testing.T) {
	// The upstream trickles a mixed stream one byte per write.
	stream := []byte("\xff\xfb\xc9\xff\xfb\x01pw: \xff\xfa\xc9Room.Info {\"name\":\"x\"}\xff\xf0\x1b[31mred\x1b[0m\r\n")
	script := func(c net.Conn) {
		c.(*net.TCPConn).SetNoDelay(true)
		for i := range stream {
			c.Write(stream[i : i+1])
			time.Sleep(2 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
	}
	addr := fakeUpstream(t, script)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.connect(80, 24)
	cl.waitText("\x1b[31mred\x1b[0m\r\n")
	cl.waitEvent("gmcp.message", "package", "Room.Info")
	cl.waitEvent("session.echo", "enabled", "false")
	if got := cl.text.String(); !strings.HasPrefix(got, "pw: ") || bytes.IndexByte([]byte(got), 0xff) >= 0 {
		t.Fatalf("terminal text = %q", got)
	}
}

func TestSlowBrowserBackpressureEndsSession(t *testing.T) {
	addr, mock := startMock(t, nil)
	g := startGateway(t, addr, false, func(c *config.Config) {
		c.WriteTimeout = 300 * time.Millisecond
		c.OutQueue = 4
	})
	// A raw connection that we simply stop reading from (no pump goroutine).
	ws, _, err := dialWith(t, g.URL, testOrigin, []string{protocol.Subprotocol})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(1 << 24)
	ctx := context.Background()
	write := func(s string) {
		if err := ws.Write(ctx, websocket.MessageText, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	readUntil := func(sub string) {
		var acc []byte
		for !strings.Contains(string(acc), sub) {
			rc, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, data, err := ws.Read(rc)
			cancel()
			if err != nil {
				t.Fatalf("waiting for %q: %v", sub, err)
			}
			acc = append(acc, data...)
		}
	}
	write(`{"v":1,"type":"session.connect"}`)
	readUntil("By what name")
	write(`{"v":1,"type":"terminal.input","data":"u\n"}`)
	readUntil("Password:")
	write(`{"v":1,"type":"terminal.input","data":"p\n"}`)
	readUntil("HP: [")
	// ~20 MB of output, far beyond every kernel and queue buffer; never read it.
	write(`{"v":1,"type":"terminal.input","data":"flood 250000\n"}`)
	eventually(t, "write timeout ends session and releases upstream", func() bool { return mock.Active() == 0 })
	eventually(t, "gateway sessions released", func() bool { return g.ActiveSessions() == 0 })
}

func TestIdleTimeout(t *testing.T) {
	addr, mock := startMock(t, nil)
	g := startGateway(t, addr, false, func(c *config.Config) { c.IdleTimeout = 300 * time.Millisecond })
	cl := dial(t, g)
	cl.login("tester", "pw")
	cl.waitEvent("connection.error", "code", protocol.ErrIdleTimeout)
	cl.waitClosed()
	eventually(t, "upstream released", func() bool { return mock.Active() == 0 })
}

func TestIdleTimeoutResetByInput(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, func(c *config.Config) { c.IdleTimeout = 600 * time.Millisecond })
	cl := dial(t, g)
	cl.login("tester", "pw")
	for i := 0; i < 4; i++ {
		time.Sleep(300 * time.Millisecond)
		cl.input(fmt.Sprintf("echo alive%d\n", i))
		cl.waitText(fmt.Sprintf("alive%d", i))
	}
	if cl.hasEvent("connection.error") {
		t.Fatal("idle timeout fired despite activity")
	}
}

func TestInputRateLimit(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, func(c *config.Config) { c.InputLinesPerS = 1; c.InputBurst = 3 })
	cl := dial(t, g)
	cl.connect(80, 24)
	cl.waitEvent("connection.status", "state", "connected")
	for i := 0; i < 10; i++ {
		cl.input("look\n")
	}
	cl.waitEvent("connection.error", "code", protocol.ErrRateLimited)
	// A rate-limit error is not fatal.
	time.Sleep(1200 * time.Millisecond)
	cl.input("echo still-here\n")
	cl.waitText("still-here")
}

func TestGracefulShutdownNotifiesSessions(t *testing.T) {
	addr, mock := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	cl := dial(t, g)
	cl.login("tester", "pw")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := g.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	cl.waitEvent("connection.error", "code", protocol.ErrShuttingDown)
	cl.waitClosed()
	eventually(t, "upstream released", func() bool { return mock.Active() == 0 })
	// New connections are refused while draining.
	_, resp, err := dialWith(t, g.URL, testOrigin, []string{protocol.Subprotocol})
	if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 during shutdown, got %v %v", resp, err)
	}
}

func TestHealthReadyMetrics(t *testing.T) {
	addr, _ := startMock(t, nil)
	g := startGateway(t, addr, false, nil)
	base := strings.Replace(strings.TrimSuffix(g.URL, "/ws"), "ws://", "http://", 1)
	get := func(p string) (int, string) {
		r, err := http.Get(base + p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var b bytes.Buffer
		b.ReadFrom(r.Body)
		return r.StatusCode, b.String()
	}
	if c, _ := get("/healthz"); c != 200 {
		t.Fatal("healthz")
	}
	if c, _ := get("/readyz"); c != 200 {
		t.Fatal("readyz")
	}
	cl := dial(t, g)
	cl.login("tester", "pw")
	if _, m := get("/metrics"); !strings.Contains(m, "tellwire_sessions_active 1") || !strings.Contains(m, "tellwire_bytes_down_total") {
		t.Fatalf("metrics:\n%s", m)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = g.Shutdown(ctx)
	if c, _ := get("/readyz"); c != 503 {
		t.Fatal("readyz should fail while draining")
	}
}

func TestClientIPTrustedProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	mk := func(remote string, xff ...string) *http.Request {
		r := httptest.NewRequest("GET", "/ws", nil)
		r.RemoteAddr = remote
		for _, x := range xff {
			r.Header.Add("X-Forwarded-For", x)
		}
		return r
	}
	cases := []struct {
		name string
		r    *http.Request
		want string
	}{
		{"direct, no proxies configured ignores XFF", mk("203.0.113.9:1", "1.2.3.4"), "203.0.113.9"},
		{"untrusted peer cannot spoof", mk("203.0.113.9:1", "10.0.0.1"), "203.0.113.9"},
		{"trusted proxy: believe XFF", mk("10.1.2.3:5", "198.51.100.7"), "198.51.100.7"},
		{"client-supplied prefix ignored (rightmost untrusted wins)", mk("10.1.2.3:5", "6.6.6.6, 198.51.100.7"), "198.51.100.7"},
		{"chain of trusted hops", mk("10.1.2.3:5", "198.51.100.7, 10.9.9.9"), "198.51.100.7"},
		{"no XFF from trusted peer", mk("10.1.2.3:5"), "10.1.2.3"},
	}
	for _, c := range cases {
		tr := trusted
		if strings.HasPrefix(c.name, "direct") {
			tr = nil
		}
		if got := clientIP(c.r, tr); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestLimiterAccounting(t *testing.T) {
	l := newLimiter(3, 2)
	r1, k := l.acquire("a")
	r2, _ := l.acquire("a")
	if _, k = l.acquire("a"); k != limitIP {
		t.Fatalf("kind=%v", k)
	}
	r3, _ := l.acquire("b")
	if _, k = l.acquire("c"); k != limitGlobal {
		t.Fatalf("kind=%v", k)
	}
	r1()
	r1() // double release must not double-decrement
	if g, _ := l.counts(); g != 2 {
		t.Fatalf("global=%d", g)
	}
	r2()
	r3()
	if g, ips := l.counts(); g != 0 || ips != 0 {
		t.Fatalf("leak: %d %d", g, ips)
	}
}
