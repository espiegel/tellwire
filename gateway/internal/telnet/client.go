package telnet

import (
	"encoding/json"
	"strings"
	"sync"
)

// ClientConfig configures a Client.
type ClientConfig struct {
	// TerminalTypes is the TTYPE cycle (MTTS style). After the last entry is
	// sent it is repeated, which tells the server the list is exhausted.
	TerminalTypes []string
	// GMCP enables the GMCP option (201) when the server offers it.
	GMCP bool
	// GMCPHello, if non-nil, is sent as Core.Hello once GMCP is active.
	GMCPHello map[string]string
	// GMCPSupports is sent as Core.Supports.Set once GMCP is active.
	GMCPSupports []string
}

// Callbacks are invoked from Feed, i.e. on the goroutine that reads upstream.
type Callbacks struct {
	// Write sends raw bytes upstream. Required.
	Write func([]byte) error
	// Text receives decoded, un-normalised application bytes. Required.
	Text func([]byte)
	// Echo reports a change in who echoes: remote=true means the server sent
	// WILL ECHO (it echoes, so the client must not and should mask input).
	Echo func(remote bool)
	// GMCP receives a raw GMCP payload ("Package.Name json").
	GMCP func(payload []byte)
	// Prompt is called for IAC GA / IAC EOR (end of prompt). Optional.
	Prompt func()
}

// Client implements the client half of Telnet option negotiation on top of
// Parser. It implements Handler; feed it via Parser.Feed(buf, client).
type Client struct {
	mu  sync.Mutex // guards everything below; callbacks that may block run outside it
	cfg ClientConfig
	cb  Callbacks

	him     [256]bool // server-side options currently enabled (we sent DO)
	us      [256]bool // local options currently enabled (we sent WILL)
	refused [256][2]bool

	cols, rows uint16
	ttypeIdx   int
	err        error
}

// NewClient returns a Client with the given configuration and callbacks.
func NewClient(cfg ClientConfig, cb Callbacks) *Client {
	if len(cfg.TerminalTypes) == 0 {
		cfg.TerminalTypes = []string{"XTERM-256COLOR"}
	}
	return &Client{cfg: cfg, cb: cb, cols: 80, rows: 24}
}

// Err returns the first write error encountered, if any.
func (c *Client) Err() error { c.mu.Lock(); defer c.mu.Unlock(); return c.err }

// RemoteEnabled reports whether the server-side option is enabled.
func (c *Client) RemoteEnabled(opt byte) bool { c.mu.Lock(); defer c.mu.Unlock(); return c.him[opt] }

// LocalEnabled reports whether the client-side option is enabled.
func (c *Client) LocalEnabled(opt byte) bool { c.mu.Lock(); defer c.mu.Unlock(); return c.us[opt] }

func (c *Client) write(b []byte) {
	if c.err != nil {
		return
	}
	if err := c.cb.Write(b); err != nil {
		c.err = err
	}
}

func (c *Client) supportsRemote(opt byte) bool {
	switch opt {
	case OptEcho, OptSGA, OptEOR:
		return true
	case OptGMCP:
		return c.cfg.GMCP
	}
	return false
}

func (c *Client) supportsLocal(opt byte) bool { return opt == OptTType || opt == OptNAWS }

// Data implements Handler.
func (c *Client) Data(p []byte) { c.cb.Text(p) }

// Command implements Handler.
func (c *Client) Command(cmd byte) {
	if (cmd == GA || cmd == EOR) && c.cb.Prompt != nil {
		c.cb.Prompt()
	}
}

// Negotiate implements Handler.
func (c *Client) Negotiate(verb, opt byte) {
	var echoChange *bool
	c.mu.Lock()
	switch verb {
	case WILL:
		switch {
		case c.him[opt]:
		case c.supportsRemote(opt):
			c.him[opt] = true
			c.write(Negotiation(DO, opt))
			echoChange = c.remoteChanged(opt, true)
		case !c.refused[opt][0]:
			c.refused[opt][0] = true
			c.write(Negotiation(DONT, opt))
		}
	case WONT:
		if c.him[opt] {
			c.him[opt] = false
			c.write(Negotiation(DONT, opt))
			echoChange = c.remoteChanged(opt, false)
		}
	case DO:
		switch {
		case c.us[opt]:
		case c.supportsLocal(opt):
			c.us[opt] = true
			c.write(Negotiation(WILL, opt))
			if opt == OptNAWS {
				c.write(NAWS(c.cols, c.rows))
			}
		case !c.refused[opt][1]:
			c.refused[opt][1] = true
			c.write(Negotiation(WONT, opt))
		}
	case DONT:
		if c.us[opt] {
			c.us[opt] = false
			c.write(Negotiation(WONT, opt))
		}
	}
	c.mu.Unlock()
	if echoChange != nil && c.cb.Echo != nil {
		c.cb.Echo(*echoChange)
	}
}

// remoteChanged runs with c.mu held. It returns a non-nil pointer when the
// Echo callback must be invoked (after the lock is released).
func (c *Client) remoteChanged(opt byte, on bool) *bool {
	switch opt {
	case OptEcho:
		return &on
	case OptGMCP:
		if on {
			c.sendGMCPHandshake()
		}
	}
	return nil
}

func (c *Client) sendGMCPHandshake() {
	if len(c.cfg.GMCPHello) > 0 {
		if b, err := json.Marshal(c.cfg.GMCPHello); err == nil {
			c.sendGMCP("Core.Hello", b)
		}
	}
	if len(c.cfg.GMCPSupports) > 0 {
		if b, err := json.Marshal(c.cfg.GMCPSupports); err == nil {
			c.sendGMCP("Core.Supports.Set", b)
		}
	}
}

// SendGMCP sends "pkg json" if GMCP is enabled. It reports whether it sent.
func (c *Client) SendGMCP(pkg string, data []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendGMCP(pkg, data)
}

func (c *Client) sendGMCP(pkg string, data []byte) bool {
	if !c.him[OptGMCP] {
		return false
	}
	payload := pkg
	if len(data) > 0 {
		payload += " " + string(data)
	}
	c.write(Subnegotiation(OptGMCP, []byte(payload)))
	return true
}

// Subnegotiation implements Handler.
func (c *Client) Subnegotiation(opt byte, p []byte) {
	switch opt {
	case OptTType:
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.us[OptTType] && len(p) >= 1 && p[0] == TTypeSend {
			name := c.cfg.TerminalTypes[c.ttypeIdx]
			if c.ttypeIdx < len(c.cfg.TerminalTypes)-1 {
				c.ttypeIdx++
			}
			c.write(TTypeIsReply(name))
		}
	case OptGMCP:
		c.mu.Lock()
		on := c.him[OptGMCP]
		c.mu.Unlock()
		if on && c.cb.GMCP != nil {
			c.cb.GMCP(p)
		}
	}
}

// SetWindowSize records the terminal size and, if NAWS is active, reports it.
func (c *Client) SetWindowSize(cols, rows uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cols == 0 || rows == 0 {
		return
	}
	if cols == c.cols && rows == c.rows {
		return
	}
	c.cols, c.rows = cols, rows
	if c.us[OptNAWS] {
		c.write(NAWS(cols, rows))
	}
}

// DefaultTerminalTypes returns the MTTS-style TTYPE cycle. The final entry
// advertises ANSI(1) + UTF-8(4) + 256 colours(8) = 13.
func DefaultTerminalTypes(client string) []string {
	return []string{strings.ToUpper(client), "XTERM-256COLOR", "MTTS 13"}
}
