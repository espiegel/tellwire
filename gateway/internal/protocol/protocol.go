// Package protocol defines the versioned browser <-> gateway WebSocket
// protocol. See docs/protocol.md for the normative description.
//
// Framing:
//   - Binary frames (gateway -> browser) carry raw terminal output: UTF-8 text
//     with ANSI escapes, Telnet commands removed, line endings normalised to
//     CR LF. The frame has no header; its payload is written straight to the
//     terminal. A multi-byte UTF-8 sequence may straddle two frames.
//   - Text frames carry one JSON object each ("control messages").
//   - Binary frames from the browser are rejected.
package protocol

import (
	"encoding/json"

	"github.com/espiegel/tellwire/gateway/internal/gmcp"
)

// Version is the protocol version carried in every JSON message ("v").
const Version = 1

// Subprotocol is the Sec-WebSocket-Protocol value negotiated by clients.
const Subprotocol = "tellwire.v1"

// Message types, browser -> gateway.
const (
	TypeSessionConnect    = "session.connect"
	TypeSessionDisconnect = "session.disconnect"
	TypeTerminalInput     = "terminal.input"
	TypeTerminalResize    = "terminal.resize"
)

// Message types, gateway -> browser. terminal.output is the binary frame.
const (
	TypeTerminalOutput   = "terminal.output" // binary frames only; named for docs
	TypeGMCPMessage      = "gmcp.message"
	TypeConnectionStatus = "connection.status"
	TypeConnectionError  = "connection.error"
	TypeSessionEcho      = "session.echo"
)

// Connection states for connection.status.
const (
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateDisconnected = "disconnected"
)

// Error codes for connection.error.
const (
	ErrBadRequest       = "bad_request"
	ErrUnsupportedVer   = "unsupported_version"
	ErrUnknownProfile   = "unknown_profile"
	ErrLimitGlobal      = "limit_global"
	ErrLimitIP          = "limit_ip"
	ErrUpstreamDial     = "upstream_unreachable"
	ErrUpstreamTLS      = "upstream_tls"
	ErrUpstreamClosed   = "upstream_closed"
	ErrUpstreamProtocol = "upstream_protocol"
	ErrIdleTimeout      = "idle_timeout"
	ErrRateLimited      = "rate_limited"
	ErrShuttingDown     = "shutting_down"
	ErrInternal         = "internal"
)

// Envelope is the common header of every JSON message.
type Envelope struct {
	V    int    `json:"v"`
	Type string `json:"type"`
}

// ClientMessage is the union of all browser -> gateway JSON messages.
type ClientMessage struct {
	Envelope
	Profile string `json:"profile,omitempty"` // session.connect
	Data    string `json:"data,omitempty"`    // terminal.input
	Cols    int    `json:"cols,omitempty"`    // session.connect, terminal.resize
	Rows    int    `json:"rows,omitempty"`
}

// ConnectionStatus is sent whenever the upstream state changes.
type ConnectionStatus struct {
	Envelope
	State   string `json:"state"`
	Profile string `json:"profile,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// ConnectionError reports a failure. Message is safe to show to users.
type ConnectionError struct {
	Envelope
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SessionEcho reports who is echoing. Enabled=false means the server is
// echoing (WILL ECHO), so the client must not echo and must mask input.
type SessionEcho struct {
	Envelope
	Enabled bool `json:"enabled"`
}

// GMCPMessage wraps a parsed GMCP message.
type GMCPMessage struct {
	Envelope
	Package   string          `json:"package"`
	Data      json.RawMessage `json:"data,omitempty"`
	Malformed bool            `json:"malformed,omitempty"`
	Error     string          `json:"error,omitempty"`
	Raw       string          `json:"raw,omitempty"`
}

func env(t string) Envelope { return Envelope{V: Version, Type: t} }

// NewStatus builds a connection.status message.
func NewStatus(state, profile, detail string) ConnectionStatus {
	return ConnectionStatus{env(TypeConnectionStatus), state, profile, detail}
}

// NewError builds a connection.error message.
func NewError(code, msg string) ConnectionError {
	return ConnectionError{env(TypeConnectionError), code, msg}
}

// NewEcho builds a session.echo message.
func NewEcho(enabled bool) SessionEcho { return SessionEcho{env(TypeSessionEcho), enabled} }

// NewGMCP builds a gmcp.message from a parsed GMCP message.
func NewGMCP(m gmcp.Message) GMCPMessage {
	return GMCPMessage{env(TypeGMCPMessage), m.Package, m.Data, m.Malformed, m.Error, m.Raw}
}
