// Package gmcp parses GMCP (Generic MUD Communication Protocol) payloads.
//
// The parser is intentionally generic: it knows nothing about specific
// packages. A payload is "<Package.Name>[ <JSON>]"; unknown packages are
// forwarded untouched and malformed payloads are reported, never dropped
// silently and never fatal.
package gmcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// MaxPackageLen bounds a package name.
const MaxPackageLen = 128

// MaxRawLen bounds how much of a malformed payload is echoed back for debugging.
const MaxRawLen = 2048

// Message is a parsed GMCP message.
type Message struct {
	// Package is the dotted package name, e.g. "Char.Vitals". Empty if the
	// payload was so malformed that no name could be recovered.
	Package string `json:"package"`
	// Data is the JSON payload exactly as sent (compacted), or nil if absent.
	Data json.RawMessage `json:"data,omitempty"`
	// Malformed is set when the package name or JSON were invalid.
	Malformed bool `json:"malformed,omitempty"`
	// Error describes why the message is malformed.
	Error string `json:"error,omitempty"`
	// Raw carries the (truncated) original payload for malformed messages.
	Raw string `json:"raw,omitempty"`
}

// Parse parses a GMCP subnegotiation payload.
func Parse(payload []byte) Message {
	name, rest := payload, []byte(nil)
	if i := bytes.IndexAny(payload, " \t"); i >= 0 {
		name, rest = payload[:i], bytes.TrimSpace(payload[i+1:])
	}
	if !validPackage(name) {
		return malformed(string(truncate(name, MaxPackageLen)), "invalid package name", payload)
	}
	m := Message{Package: string(name)}
	if len(rest) == 0 {
		return m
	}
	if !json.Valid(rest) {
		m.Malformed, m.Error, m.Raw = true, "invalid JSON payload", rawString(rest)
		return m
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, rest); err != nil { // cannot fail after Valid, but be safe
		m.Malformed, m.Error, m.Raw = true, "invalid JSON payload", rawString(rest)
		return m
	}
	m.Data = buf.Bytes()
	return m
}

func malformed(pkg, why string, payload []byte) Message {
	if !utf8.ValidString(pkg) {
		pkg = ""
	}
	return Message{Package: pkg, Malformed: true, Error: why, Raw: rawString(payload)}
}

func validPackage(b []byte) bool {
	if len(b) == 0 || len(b) > MaxPackageLen {
		return false
	}
	for _, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return b[0] != '.' && b[len(b)-1] != '.' && !bytes.Contains(b, []byte(".."))
}

func truncate(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

func rawString(b []byte) string { return strings.ToValidUTF8(string(truncate(b, MaxRawLen)), "�") }
