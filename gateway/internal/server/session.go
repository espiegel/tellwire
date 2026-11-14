package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/espiegel/tellwire/gateway/internal/gmcp"
	"github.com/espiegel/tellwire/gateway/internal/protocol"
	"github.com/espiegel/tellwire/gateway/internal/telnet"
)

const (
	firstMessageTimeout = 10 * time.Second
	readBufSize         = 8 * 1024
	maxCols, maxRows    = 500, 200
)

type frame struct {
	typ  websocket.MessageType
	data []byte
	end  bool // sentinel: the writer stops after seeing this
}

// session bridges one browser WebSocket to one upstream Telnet connection.
// Sessions share nothing except the Server's immutable config and counters.
type session struct {
	srv *Server
	ws  *websocket.Conn
	ip  string
	id  string
	log interface {
		Info(msg string, args ...any)
		Warn(msg string, args ...any)
		Debug(msg string, args ...any)
	}

	out        chan frame
	sctx       context.Context // session lifetime; cancelled by end()
	scancel    context.CancelFunc
	endOnce    sync.Once
	endCode    string // protocol error code, or "" for a clean end
	endMessage string

	upMu     sync.Mutex // serialises writes to upstream
	upstream net.Conn
	tc       *telnet.Client
	norm     telnet.Normalizer // used only by the upstream reader goroutine

	bytesUp, bytesDown atomic.Int64 // per-session totals for the end-of-session log line
	idle               *time.Timer
}

func newSession(s *Server, ws *websocket.Conn, ip string) *session {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	id := hex.EncodeToString(raw[:])
	return &session{
		srv: s, ws: ws, ip: ip, id: id,
		log: s.log.With("session", id),
		out: make(chan frame, s.cfg.OutQueue),
	}
}

// end records why the session is ending (first call wins) and begins teardown.
func (se *session) end(code, msg string) {
	se.endOnce.Do(func() {
		se.endCode, se.endMessage = code, msg
		se.scancel()
	})
}

func (se *session) enqueue(f frame) bool {
	select {
	case se.out <- f:
		return true
	case <-se.sctx.Done():
		return false
	}
}

func (se *session) sendJSON(v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return se.enqueue(frame{typ: websocket.MessageText, data: b})
}

// enqueueFinal is used during teardown, when sctx is already cancelled: it
// still delivers, bounded by a short timeout.
func (se *session) enqueueFinal(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case se.out <- frame{typ: websocket.MessageText, data: b}:
	case <-time.After(2 * time.Second):
	}
}

func (se *session) run(parent context.Context) {
	start := time.Now()
	rctx, rcancel := context.WithCancel(context.Background()) // WebSocket I/O lives until the very end
	defer rcancel()
	se.sctx, se.scancel = context.WithCancel(rctx)
	defer se.scancel()

	// Server shutdown cancels parent; translate that into a clean session end.
	go func() {
		select {
		case <-parent.Done():
			se.end(protocol.ErrShuttingDown, "The gateway is restarting. Please reconnect in a moment.")
		case <-se.sctx.Done():
		}
	}()

	if se.ws.Subprotocol() != protocol.Subprotocol {
		se.ws.Close(websocket.StatusPolicyViolation, "subprotocol tellwire.v1 required")
		return
	}

	// Writer goroutine: the only place frames are written to the browser.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		se.writer(rctx)
	}()

	release, kind := se.srv.lim.acquire(se.ip)
	if kind != limitOK {
		code, msg := protocol.ErrLimitGlobal, "The server is at capacity. Please try again in a few minutes."
		if kind == limitIP {
			code, msg = protocol.ErrLimitIP, "Too many connections from your network. Close another session and retry."
		}
		se.srv.met.end(code)
		se.log.Info("session rejected", "reason", code)
		se.finish(writerDone, rcancel, code, msg, websocket.StatusTryAgainLater, nil)
		return
	}
	defer release()

	// Wait for session.connect (bounded).
	first, err := se.readFirst(rctx)
	if err != nil {
		se.srv.met.end(protocol.ErrBadRequest)
		se.finish(writerDone, rcancel, protocol.ErrBadRequest, err.Error(), websocket.StatusPolicyViolation, nil)
		return
	}
	up, ok := se.srv.cfg.Profile(first.Profile)
	if !ok {
		se.srv.met.end(protocol.ErrUnknownProfile)
		se.finish(writerDone, rcancel, protocol.ErrUnknownProfile, "Unknown server profile.", websocket.StatusPolicyViolation, nil)
		return
	}
	se.log.Info("session start", "profile", up.Name)
	se.sendJSON(protocol.NewStatus(protocol.StateConnecting, up.Name, ""))

	conn, err := se.srv.dial(se.sctx, up)
	if err != nil {
		code, msg := classifyDialError(err)
		se.log.Warn("upstream dial failed", "profile", up.Name, "code", code)
		se.srv.met.end(code)
		se.finish(writerDone, rcancel, code, msg, websocket.StatusInternalError, nil)
		return
	}
	se.upstream = conn
	se.setupTelnet(first)
	se.sendJSON(protocol.NewEcho(true))
	se.sendJSON(protocol.NewStatus(protocol.StateConnected, up.Name, ""))

	// Browser reader.
	browserDone := make(chan struct{})
	go func() {
		defer close(browserDone)
		se.browserLoop(rctx)
	}()
	// Upstream reader.
	upDone := make(chan struct{})
	go func() {
		defer close(upDone)
		se.upstreamLoop()
	}()
	// Keepalive pings.
	go se.pinger(rctx)

	<-se.sctx.Done()
	conn.Close() // unblocks upstreamLoop
	<-upDone

	code, msg := se.endCode, se.endMessage
	se.srv.met.end(orClean(code))
	dur := time.Since(start).Round(time.Millisecond)
	se.log.Info("session end", "profile", up.Name, "reason", orClean(code), "duration", dur.String(),
		"bytes_up", se.bytesUp.Load(), "bytes_down", se.bytesDown.Load())
	se.finish(writerDone, rcancel, code, msg, websocket.StatusNormalClosure, browserDone)
}

func orClean(c string) string {
	if c == "" {
		return "clean"
	}
	return c
}

// finish flushes final messages, stops the writer and closes the WebSocket.
func (se *session) finish(writerDone chan struct{}, rcancel context.CancelFunc, code, msg string, status websocket.StatusCode, browserDone chan struct{}) {
	if code != "" && code != protocol.ErrUpstreamClosed && code != "client_disconnect" {
		se.enqueueFinal(protocol.NewError(code, msg))
	}
	detail := msg
	if code == "client_disconnect" {
		detail = "Disconnected."
	}
	se.enqueueFinal(protocol.NewStatus(protocol.StateDisconnected, "", detail))
	select {
	case se.out <- frame{end: true}:
	case <-time.After(2 * time.Second):
	}
	select {
	case <-writerDone:
	case <-time.After(5 * time.Second):
	}
	// Close handshake; coder/websocket bounds this itself (5s).
	_ = se.ws.Close(status, "")
	rcancel()
	if browserDone != nil {
		<-browserDone
	}
}

func (se *session) writer(ctx context.Context) {
	failed := false
	for f := range se.out {
		if f.end {
			return
		}
		if failed {
			continue // keep draining so producers never wedge
		}
		wctx, cancel := context.WithTimeout(ctx, se.srv.cfg.WriteTimeout)
		err := se.ws.Write(wctx, f.typ, f.data)
		cancel()
		if err != nil {
			failed = true
			se.end(protocol.ErrInternal, "Lost connection to your browser.")
		}
	}
}

func (se *session) readFirst(ctx context.Context) (*protocol.ClientMessage, error) {
	rctx, cancel := context.WithTimeout(ctx, firstMessageTimeout)
	defer cancel()
	m, err := se.readMsg(rctx)
	if err != nil {
		return nil, errors.New("Expected session.connect.")
	}
	if m.Type != protocol.TypeSessionConnect {
		return nil, errors.New("First message must be session.connect.")
	}
	return m, nil
}

// readMsg reads and validates one JSON control message.
func (se *session) readMsg(ctx context.Context) (*protocol.ClientMessage, error) {
	typ, data, err := se.ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, errors.New("binary frames are not accepted")
	}
	var m protocol.ClientMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errors.New("invalid JSON")
	}
	if m.V != protocol.Version {
		return nil, errors.New("unsupported protocol version")
	}
	return &m, nil
}

func clampSize(cols, rows int) (uint16, uint16) {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	if cols > maxCols {
		cols = maxCols
	}
	if rows > maxRows {
		rows = maxRows
	}
	return uint16(cols), uint16(rows)
}

func (se *session) setupTelnet(first *protocol.ClientMessage) {
	cfg := se.srv.cfg
	cc := telnet.ClientConfig{
		TerminalTypes: telnet.DefaultTerminalTypes(cfg.ClientName),
		GMCP:          cfg.GMCP,
		GMCPSupports:  cfg.GMCPSupports,
	}
	if cfg.GMCPHello {
		cc.GMCPHello = map[string]string{"client": cfg.ClientName, "version": cfg.ClientVer}
	}
	se.tc = telnet.NewClient(cc, telnet.Callbacks{
		Write: se.writeUpstreamRaw,
		Text:  se.onText,
		Echo:  func(remote bool) { se.sendJSON(protocol.NewEcho(!remote)) },
		GMCP:  se.onGMCP,
	})
	se.tc.SetWindowSize(clampSize(first.Cols, first.Rows))
}

// writeUpstreamRaw writes to the MUD under the write lock and a deadline.
func (se *session) writeUpstreamRaw(b []byte) error {
	se.upMu.Lock()
	defer se.upMu.Unlock()
	_ = se.upstream.SetWriteDeadline(time.Now().Add(se.srv.cfg.WriteTimeout))
	_, err := se.upstream.Write(b)
	if err == nil {
		se.srv.met.bytesUp.Add(uint64(len(b)))
		se.bytesUp.Add(int64(len(b)))
	}
	return err
}

func (se *session) onText(p []byte) {
	// Normalise into a freshly allocated frame (owned by the writer from here on).
	buf := se.norm.Append(make([]byte, 0, len(p)+len(p)/8+2), p)
	if len(buf) == 0 {
		return
	}
	se.srv.met.bytesDown.Add(uint64(len(buf)))
	se.bytesDown.Add(int64(len(buf)))
	se.enqueue(frame{typ: websocket.MessageBinary, data: buf})
}

func (se *session) onGMCP(payload []byte) {
	m := gmcp.Parse(payload)
	se.srv.met.gmcpMessages.Add(1)
	if m.Malformed {
		se.srv.met.gmcpMalformed.Add(1)
	}
	se.sendJSON(protocol.NewGMCP(m))
}

func (se *session) upstreamLoop() {
	p := telnet.NewParser(se.srv.cfg.MaxSubnegBytes)
	buf := make([]byte, readBufSize)
	for {
		n, err := se.upstream.Read(buf)
		if n > 0 {
			p.Feed(buf[:n], se.tc)
			if werr := se.tc.Err(); werr != nil {
				se.end(protocol.ErrUpstreamClosed, "Lost connection to the game server.")
				return
			}
		}
		if err != nil {
			if se.sctx.Err() != nil {
				return // we closed it
			}
			if errors.Is(err, io.EOF) {
				se.end(protocol.ErrUpstreamClosed, "The game server closed the connection.")
			} else {
				se.end(protocol.ErrUpstreamClosed, "Lost connection to the game server.")
			}
			return
		}
	}
}

func (se *session) browserLoop(ctx context.Context) {
	cfg := se.srv.cfg
	bk := newBucket(cfg.InputLinesPerS, cfg.InputBurst)
	if cfg.IdleTimeout > 0 {
		se.idle = time.AfterFunc(cfg.IdleTimeout, func() {
			se.end(protocol.ErrIdleTimeout, "Disconnected for inactivity.")
		})
		defer se.idle.Stop()
	}
	for {
		m, err := se.readMsg(ctx)
		if err != nil {
			if se.sctx.Err() == nil {
				if websocket.CloseStatus(err) != -1 || errors.Is(err, io.EOF) || ctx.Err() != nil {
					se.end("client_disconnect", "")
				} else {
					se.end(protocol.ErrBadRequest, "Invalid message from browser: "+err.Error())
				}
			}
			return
		}
		switch m.Type {
		case protocol.TypeTerminalInput:
			if se.idle != nil {
				se.idle.Reset(cfg.IdleTimeout)
			}
			if !bk.allow() {
				se.sendJSON(protocol.NewError(protocol.ErrRateLimited, "You are sending commands too fast; the last input was dropped."))
				continue
			}
			if err := se.writeUpstreamRaw(telnet.EncodeLine(m.Data)); err != nil {
				se.end(protocol.ErrUpstreamClosed, "Lost connection to the game server.")
				return
			}
		case protocol.TypeTerminalResize:
			se.tc.SetWindowSize(clampSize(m.Cols, m.Rows))
		case protocol.TypeSessionDisconnect:
			se.end("client_disconnect", "")
			return
		default:
			se.end(protocol.ErrBadRequest, "Unknown message type.")
			return
		}
	}
}

func (se *session) pinger(ctx context.Context) {
	iv := se.srv.cfg.PingInterval
	if iv <= 0 {
		return
	}
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-se.sctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, iv)
			err := se.ws.Ping(pctx)
			cancel()
			if err != nil {
				se.end("client_disconnect", "")
				return
			}
		}
	}
}

// classifyDialError turns a dial error into a protocol code and a user-safe
// message. Raw error text is never forwarded (it can contain addresses).
func classifyDialError(err error) (string, string) {
	var (
		cve *tls.CertificateVerificationError
		uae x509.UnknownAuthorityError
		he  x509.HostnameError
		ci  x509.CertificateInvalidError
		rh  tls.RecordHeaderError
	)
	switch {
	case errors.As(err, &cve), errors.As(err, &uae), errors.As(err, &he), errors.As(err, &ci):
		return protocol.ErrUpstreamTLS, "The game server's TLS certificate could not be verified, so the gateway refused to connect."
	case errors.As(err, &rh):
		return protocol.ErrUpstreamTLS, "The game server did not speak TLS on this port."
	case errors.Is(err, context.Canceled):
		return protocol.ErrShuttingDown, "The gateway is restarting. Please reconnect in a moment."
	case errors.Is(err, context.DeadlineExceeded):
		return protocol.ErrUpstreamDial, "Timed out connecting to the game server."
	}
	return protocol.ErrUpstreamDial, "Could not connect to the game server. It may be down; try again shortly."
}
