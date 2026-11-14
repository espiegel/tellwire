// Package mockmud is a small, deterministic fake MUD used for automated tests
// and for the Docker demo. It speaks real Telnet (TTYPE, NAWS, ECHO, GMCP) so
// the gateway and browser are exercised against realistic protocol traffic
// without ever touching the live DuneMUD server.
//
// It never logs anything a client sends, including passwords.
package mockmud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/espiegel/tellwire/gateway/internal/telnet"
)

// Server is a mock MUD.
type Server struct {
	// Newline is the line terminator the server emits (default "\r\n"). Set
	// "\n\r" or "\n" to exercise line-ending normalisation.
	Newline string
	// NoGMCP disables the GMCP offer.
	NoGMCP bool
	// NoLogin skips the name/password prompts.
	NoLogin bool

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	// Accepted counts accepted connections (for tests).
	accepted int
}

// Active returns the number of currently open connections.
func (s *Server) Active() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.conns) }

// Accepted returns the number of connections accepted so far.
func (s *Server) Accepted() int { s.mu.Lock(); defer s.mu.Unlock(); return s.accepted }

type room struct {
	name, desc string
	exits      map[string]string
}

var world = map[string]room{
	"landing": {
		"Arrakeen Shuttle Landing Port",
		"The \x1b[33mgolden sand\x1b[0m of Arrakis stretches beyond the \x1b[1;37mlanding pads\x1b[0m. Heat shimmers\r\nabove the ferrocrete and the \x1b[36mdistant ridge\x1b[0m of the Shield Wall.",
		map[string]string{"north": "market", "east": "cantina"},
	},
	"market": {
		"Arrakeen Market",
		"Merchants hawk \x1b[1;35mspice\x1b[0m, \x1b[1;34mstillsuits\x1b[0m and crysknives under striped awnings.",
		map[string]string{"south": "landing"},
	},
	"cantina": {
		"The Sietch Cantina",
		"A dim \x1b[38;5;208mamber\x1b[0m glow falls across low tables. A \x1b[1;32mfremen\x1b[0m scout eyes you quietly. 🏜️",
		map[string]string{"west": "landing"},
	},
}

// Serve accepts connections on ln until ctx is cancelled or ln fails.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.mu.Lock()
	if s.conns == nil {
		s.conns = map[net.Conn]struct{}{}
	}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		ln.Close()
		s.mu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.accepted++
		s.mu.Unlock()
		go func() {
			defer func() {
				c.Close()
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
			}()
			s.handle(c)
		}()
	}
}

type phase int

const (
	phName phase = iota
	phPassword
	phPlaying
)

type sess struct {
	s   *Server
	c   net.Conn
	wmu sync.Mutex
	nl  string

	ph       phase
	name     string
	room     string
	hp, maxh int

	ttypes  []string
	cols    int
	rows    int
	gmcpOn  bool
	gmcpSup bool
	line    bytes.Buffer
	done    bool
}

func (s *Server) handle(c net.Conn) {
	nl := s.Newline
	if nl == "" {
		nl = "\r\n"
	}
	m := &sess{s: s, c: c, nl: nl, room: "landing", hp: 380, maxh: 412, cols: 80, rows: 24}
	m.send(telnet.Negotiation(telnet.DO, telnet.OptTType))
	m.send(telnet.Negotiation(telnet.DO, telnet.OptNAWS))
	if !s.NoGMCP {
		m.send(telnet.Negotiation(telnet.WILL, telnet.OptGMCP))
	}
	m.say("\x1b[1;33m=== Tellwire Mock Server ===\x1b[0m")
	m.say("A deterministic fake MUD for testing web clients. \x1b[2mType 'help' once inside.\x1b[0m")
	m.say("")
	if s.NoLogin {
		m.name, m.ph = "guest", phPlaying
		m.enter()
	} else {
		m.say("Any name and password will do. Passwords are masked and never stored.")
		m.raw("By what name are you known? ")
	}

	p := telnet.NewParser(0)
	buf := make([]byte, 4096)
	for !m.done {
		n, err := c.Read(buf)
		if n > 0 {
			p.Feed(buf[:n], m)
		}
		if err != nil {
			return
		}
	}
}

func (m *sess) send(b []byte) {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	_, _ = m.c.Write(b)
}

func (m *sess) raw(s string) { m.send([]byte(s)) }

// say writes text followed by the configured newline. Embedded "\r\n" are
// rewritten to the configured newline.
func (m *sess) say(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	m.raw(strings.ReplaceAll(s, "\n", m.nl) + m.nl)
}

func (m *sess) gmcp(pkg string, v any) {
	if !m.gmcpOn || !m.gmcpSup {
		return
	}
	b, _ := json.Marshal(v)
	m.send(telnet.Subnegotiation(telnet.OptGMCP, []byte(pkg+" "+string(b))))
}

// ---- telnet.Handler ----

func (m *sess) Negotiate(verb, opt byte) {
	switch {
	case verb == telnet.WILL && opt == telnet.OptTType:
		m.send(telnet.Subnegotiation(telnet.OptTType, []byte{telnet.TTypeSend}))
	case verb == telnet.DO && opt == telnet.OptGMCP:
		m.gmcpOn = true
	case verb == telnet.DONT && opt == telnet.OptGMCP:
		m.gmcpOn = false
	}
}

func (m *sess) Subnegotiation(opt byte, p []byte) {
	switch opt {
	case telnet.OptTType:
		if len(p) > 1 && p[0] == telnet.TTypeIs {
			m.ttypes = append(m.ttypes, string(p[1:]))
			if len(m.ttypes) < 3 { // ask for the next one, like MTTS servers do
				m.send(telnet.Subnegotiation(telnet.OptTType, []byte{telnet.TTypeSend}))
			}
		}
	case telnet.OptNAWS:
		if len(p) == 4 {
			m.cols, m.rows = int(p[0])<<8|int(p[1]), int(p[2])<<8|int(p[3])
		}
	case telnet.OptGMCP:
		pkg, _, _ := strings.Cut(string(p), " ")
		if pkg == "Core.Supports.Set" || pkg == "Core.Supports.Add" {
			m.gmcpSup = true
			if m.ph == phPlaying {
				m.sendState()
			}
		}
	}
}

func (m *sess) Command(byte) {}

func (m *sess) Data(p []byte) {
	for _, b := range p {
		switch b {
		case '\n':
			line := strings.TrimRight(m.line.String(), "\r")
			m.line.Reset()
			m.onLine(line)
			if m.done {
				return
			}
		case 0:
		default:
			if m.line.Len() < 1024 {
				m.line.WriteByte(b)
			}
		}
	}
}

// ---- game ----

func (m *sess) onLine(line string) {
	switch m.ph {
	case phName:
		name := strings.TrimSpace(line)
		if name == "" {
			m.raw("By what name are you known? ")
			return
		}
		m.name = name
		m.ph = phPassword
		m.send(telnet.Negotiation(telnet.WILL, telnet.OptEcho)) // server echoes (i.e. nobody does): mask input
		m.raw("Password: ")
	case phPassword:
		// The password is intentionally discarded and never logged.
		m.send(telnet.Negotiation(telnet.WONT, telnet.OptEcho))
		m.say("")
		m.ph = phPlaying
		m.enter()
	default:
		m.command(strings.TrimSpace(line))
	}
}

func (m *sess) enter() {
	m.say(fmt.Sprintf("\x1b[1;32mWelcome, %s.\x1b[0m", m.name))
	m.sendState()
	m.look()
	m.prompt()
}

func (m *sess) sendState() {
	m.gmcp("Char.Name", map[string]any{"name": strings.ToLower(m.name), "fullname": m.name + " the Wanderer", "guild": "none"})
	m.gmcp("Char.Vitals", map[string]any{"hp": m.hp, "maxhp": m.maxh, "sp": 120, "maxsp": 150, "string": fmt.Sprintf("H:%d/%d M:120/150", m.hp, m.maxh)})
	m.gmcp("Char.Status", map[string]any{"level": 12, "money": 1500, "xp": 84231, "guild": "none"})
	m.gmcp("Comm.Channel.List", []map[string]any{{"channel": "gossip", "name": "gossip", "command": "gossip", "enabled": 1}})
	m.roomInfo()
}

func (m *sess) roomInfo() {
	r := world[m.room]
	exits := map[string]string{}
	for d, to := range r.exits {
		exits[d] = to
	}
	m.gmcp("Room.Info", map[string]any{"num": m.room, "name": r.name, "area": "arrakis", "environment": "outdoors", "exits": exits})
}

func (m *sess) look() {
	r := world[m.room]
	m.say("\x1b[1;36m" + r.name + "\x1b[0m")
	m.say(r.desc)
	var ex []string
	for _, d := range []string{"north", "east", "south", "west"} {
		if _, ok := r.exits[d]; ok {
			ex = append(ex, d)
		}
	}
	m.say("\x1b[32mExits:\x1b[0m " + strings.Join(ex, ", "))
}

func (m *sess) prompt() {
	m.raw(fmt.Sprintf("%s\x1b[1;31mHP: [%d/%d]\x1b[0m \x1b[1;34mCP: [120/150]\x1b[0m > ", m.nl, m.hp, m.maxh))
}

var dirs = map[string]string{"n": "north", "e": "east", "s": "south", "w": "west"}

func (m *sess) command(line string) {
	verb, arg, _ := strings.Cut(line, " ")
	verb = strings.ToLower(verb)
	if d, ok := dirs[verb]; ok {
		verb = d
	}
	switch verb {
	case "":
	case "look", "l":
		m.look()
	case "north", "east", "south", "west":
		if to, ok := world[m.room].exits[verb]; ok {
			m.room = to
			m.say("You walk " + verb + ".")
			m.look()
			m.roomInfo()
		} else {
			m.say("You can't go that way.")
		}
	case "say":
		m.say(fmt.Sprintf("You say: \x1b[3m%s\x1b[0m", arg))
	case "who":
		m.say("\x1b[1mPlayers online:\x1b[0m\n  \x1b[33m" + m.name + "\x1b[0m the Wanderer\n  \x1b[35mPaul\x1b[0m Muad'Dib [BOT]\n2 players.")
	case "hit":
		m.hp -= 25
		if m.hp < 1 {
			m.hp = m.maxh
		}
		m.say("You swing your crysknife at a \x1b[31msand sprite\x1b[0m!")
		m.gmcp("Char.Vitals", map[string]any{"hp": m.hp, "string": fmt.Sprintf("H:%d/%d M:120/150", m.hp, m.maxh)}) // delta
	case "colors":
		var sb strings.Builder
		for i := 30; i <= 37; i++ {
			fmt.Fprintf(&sb, "\x1b[%dm color%d\x1b[0m", i, i)
		}
		sb.WriteString("\n")
		for i := 16; i < 232; i += 6 {
			fmt.Fprintf(&sb, "\x1b[48;5;%dm  \x1b[0m", i)
		}
		sb.WriteString("\n\x1b[38;2;255;140;0mtruecolor orange\x1b[0m \x1b[1mbold\x1b[0m \x1b[4munderline\x1b[0m \x1b[7mreverse\x1b[0m")
		m.say(sb.String())
	case "unicode":
		m.say("Sandworm: 🪱  Spice: ✨  Fremen: 🏜️  Box: ╔═══╗  Accents: café naïve")
	case "term":
		m.say(fmt.Sprintf("TTYPE: %s | size: %dx%d", strings.Join(m.ttypes, ", "), m.cols, m.rows))
	case "gmcp":
		m.say(fmt.Sprintf("GMCP offered=%v client-opted-in=%v", m.gmcpOn, m.gmcpSup))
	case "flood": // lots of output, to exercise backpressure
		n := 2000
		if v, err := strconv.Atoi(arg); err == nil && v > 0 && v <= 1_000_000 {
			n = v
		}
		for i := 1; i <= n; i++ {
			m.say(fmt.Sprintf("\x1b[2mflood line %04d\x1b[0m the quick brown fox jumps over the lazy dog", i))
		}
	case "echo":
		m.say(arg)
	case "crash": // drop the connection without a goodbye
		m.done = true
		m.c.Close()
		return
	case "help":
		m.say("Commands: look, n/e/s/w, say <text>, who, hit, colors, unicode, term, gmcp, flood, echo <text>, crash, quit")
	case "quit":
		m.say("Farewell. May the spice flow.")
		m.done = true
		return
	default:
		m.say(fmt.Sprintf("Huh? '%s' is not a command. Try 'help'.", verb))
	}
	m.prompt()
}

// CloseAll force-closes every open connection (used by tests and shutdown).
func (s *Server) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.Close()
	}
}
