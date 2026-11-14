package telnet

// Handler receives the events produced by Parser.Feed. Slices passed to the
// handler are only valid for the duration of the call; copy them to retain.
type Handler interface {
	// Data is called with application bytes (IAC escapes already removed).
	Data(p []byte)
	// Negotiate is called for IAC WILL/WONT/DO/DONT <option>.
	Negotiate(verb, option byte)
	// Subnegotiation is called for IAC SB <option> <payload> IAC SE.
	// The payload has IAC IAC collapsed to a single 0xFF.
	Subnegotiation(option byte, payload []byte)
	// Command is called for the remaining two-byte commands (NOP, GA, EOR...).
	Command(cmd byte)
}

type pstate uint8

const (
	stData pstate = iota
	stIAC
	stVerb     // after IAC WILL/WONT/DO/DONT, waiting for the option byte
	stSBOption // after IAC SB, waiting for the option byte
	stSBData   // inside a subnegotiation
	stSBIAC    // inside a subnegotiation, just saw IAC
)

// DefaultMaxSubnegotiation bounds a subnegotiation payload (bytes).
const DefaultMaxSubnegotiation = 64 * 1024

// Parser is an incremental Telnet decoder. A sequence split across any number
// of Feed calls decodes identically to the same bytes delivered at once.
// A Parser is not safe for concurrent use.
type Parser struct {
	state   pstate
	verb    byte
	sbOpt   byte
	sb      []byte
	max     int
	dropped bool // current subnegotiation exceeded max and is being discarded

	// Overflows counts subnegotiations discarded for exceeding the limit.
	Overflows int
}

// NewParser returns a Parser whose subnegotiation payloads are capped at
// maxSub bytes (DefaultMaxSubnegotiation if maxSub <= 0). Payloads larger than
// the cap are discarded and counted in Overflows; the stream stays in sync.
func NewParser(maxSub int) *Parser {
	if maxSub <= 0 {
		maxSub = DefaultMaxSubnegotiation
	}
	return &Parser{max: maxSub}
}

// Feed decodes p, invoking h for every event. Data is delivered in as few
// calls as possible (one per run of plain bytes).
func (ps *Parser) Feed(p []byte, h Handler) {
	start := -1 // start of the current run of plain data in p, or -1
	flush := func(end int) {
		if start >= 0 && end > start {
			h.Data(p[start:end])
		}
		start = -1
	}
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch ps.state {
		case stData:
			if b == IAC {
				flush(i)
				ps.state = stIAC
			} else if start < 0 {
				start = i
			}
		case stIAC:
			switch b {
			case IAC:
				// Escaped 0xFF data byte. Deliver as its own tiny run.
				h.Data(p[i : i+1])
				ps.state = stData
			case WILL, WONT, DO, DONT:
				ps.verb = b
				ps.state = stVerb
			case SB:
				ps.state = stSBOption
			case SE:
				// Stray SE outside a subnegotiation: ignore.
				ps.state = stData
			default:
				h.Command(b)
				ps.state = stData
			}
		case stVerb:
			h.Negotiate(ps.verb, b)
			ps.state = stData
		case stSBOption:
			ps.sbOpt = b
			ps.sb = ps.sb[:0]
			ps.dropped = false
			ps.state = stSBData
		case stSBData:
			if b == IAC {
				ps.state = stSBIAC
			} else {
				ps.sbAppend(b)
			}
		case stSBIAC:
			switch b {
			case IAC:
				ps.sbAppend(IAC)
				ps.state = stSBData
			case SE:
				ps.endSB(h)
			default:
				// Malformed: any other command inside SB aborts it (RFC 855
				// leaves this undefined; Postel says resync, don't wedge).
				ps.sb = ps.sb[:0]
				ps.state = stData
				if b == WILL || b == WONT || b == DO || b == DONT {
					ps.verb = b
					ps.state = stVerb
				} else {
					h.Command(b)
				}
			}
		}
	}
	if ps.state == stData {
		flush(len(p))
	}
}

func (ps *Parser) sbAppend(b byte) {
	if ps.dropped {
		return
	}
	if len(ps.sb) >= ps.max {
		ps.dropped = true
		ps.sb = ps.sb[:0]
		return
	}
	ps.sb = append(ps.sb, b)
}

func (ps *Parser) endSB(h Handler) {
	ps.state = stData
	if ps.dropped {
		ps.Overflows++
		return
	}
	h.Subnegotiation(ps.sbOpt, ps.sb)
}
