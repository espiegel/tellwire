package telnet

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// recorder flattens parser events into comparable strings.
type recorder struct {
	data   bytes.Buffer
	events []string
}

func (r *recorder) flushData() {
	if r.data.Len() > 0 {
		r.events = append(r.events, "D:"+r.data.String())
		r.data.Reset()
	}
}
func (r *recorder) Data(p []byte) { r.data.Write(p) }
func (r *recorder) Negotiate(v, o byte) {
	r.flushData()
	r.events = append(r.events, fmt.Sprintf("N:%d/%d", v, o))
}
func (r *recorder) Subnegotiation(o byte, p []byte) {
	r.flushData()
	r.events = append(r.events, fmt.Sprintf("S:%d:%q", o, p))
}
func (r *recorder) Command(c byte) {
	r.flushData()
	r.events = append(r.events, fmt.Sprintf("C:%d", c))
}

func parseChunks(chunks ...[]byte) []string {
	r := &recorder{}
	ps := NewParser(0)
	for _, c := range chunks {
		ps.Feed(c, r)
	}
	r.flushData()
	return r.events
}

// A stream with every construct, including ANSI colour and a UTF-8 emoji.
var mixed = []byte("\x1b[1;31mHello\x1b[0m \xf0\x9f\x8f\x9c\r\n" +
	"\xff\xfb\x01" + // WILL ECHO
	"pass\xff\xff" + "word" + // escaped IAC in data
	"\xff\xfa\xc9Room.Info {\"name\":\"a\xff\xffb\"}\xff\xf0" + // GMCP with escaped IAC
	"\xff\xfd\x1f" + // DO NAWS
	"\xff\xf9" + // GA
	"tail")

func TestParserWholeStream(t *testing.T) {
	got := parseChunks(mixed)
	want := []string{
		"D:\x1b[1;31mHello\x1b[0m \xf0\x9f\x8f\x9c\r\n",
		"N:251/1",
		"D:pass\xffword",
		"S:201:\"Room.Info {\\\"name\\\":\\\"a\\xffb\\\"}\"",
		"N:253/31",
		"C:249",
		"D:tail",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestParserFragmentedAtEveryBoundary(t *testing.T) {
	want := parseChunks(mixed)
	for i := 0; i <= len(mixed); i++ {
		got := parseChunks(mixed[:i], mixed[i:])
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("split at %d:\n got  %q\n want %q", i, got, want)
		}
	}
}

func TestParserByteAtATime(t *testing.T) {
	chunks := make([][]byte, len(mixed))
	for i := range mixed {
		chunks[i] = mixed[i : i+1]
	}
	if got, want := parseChunks(chunks...), parseChunks(mixed); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParserRandomChunking(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	want := parseChunks(mixed)
	for n := 0; n < 500; n++ {
		var chunks [][]byte
		for rest := mixed; len(rest) > 0; {
			k := 1 + rng.Intn(len(rest))
			chunks = append(chunks, rest[:k])
			rest = rest[k:]
		}
		if got := parseChunks(chunks...); !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d chunks %q\n got %q\nwant %q", n, chunks, got, want)
		}
	}
}

func TestNoControlBytesLeakIntoData(t *testing.T) {
	r := &recorder{}
	NewParser(0).Feed([]byte("a\xff\xfb\x01b\xff\xfa\x18\x01\xff\xf0c\xff\xf1d"), r)
	r.flushData()
	want := []string{"D:a", "N:251/1", "D:b", `S:24:"\x01"`, "D:c", "C:241", "D:d"}
	if !reflect.DeepEqual(r.events, want) {
		t.Fatalf("got %q want %q", r.events, want)
	}
}

func TestSubnegotiationOverflowIsDiscardedAndStreamResyncs(t *testing.T) {
	r := &recorder{}
	ps := NewParser(8)
	huge := append([]byte{IAC, SB, OptGMCP}, bytes.Repeat([]byte("x"), 1000)...)
	huge = append(huge, IAC, SE)
	ps.Feed(huge, r)
	ps.Feed([]byte("ok"), r)
	r.flushData()
	if ps.Overflows != 1 {
		t.Fatalf("Overflows=%d", ps.Overflows)
	}
	if !reflect.DeepEqual(r.events, []string{"D:ok"}) {
		t.Fatalf("events=%q", r.events)
	}
}

func TestMalformedSubnegotiationDoesNotWedgeParser(t *testing.T) {
	// IAC GA inside SB aborts it; following text must come through.
	got := parseChunks([]byte("\xff\xfa\xc9abc\xff\xf9hello"))
	want := []string{"C:249", "D:hello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestEncode(t *testing.T) {
	if got := AppendEscaped(nil, []byte{1, 0xff, 2}); !bytes.Equal(got, []byte{1, 0xff, 0xff, 2}) {
		t.Fatalf("%v", got)
	}
	if got := NAWS(300, 40); !bytes.Equal(got, []byte{IAC, SB, OptNAWS, 1, 44, 0, 40, IAC, SE}) {
		t.Fatalf("%v", got)
	}
	// NAWS width 255 must be IAC-escaped.
	if got := NAWS(255, 24); !bytes.Equal(got, []byte{IAC, SB, OptNAWS, 0, IAC, IAC, 0, 24, IAC, SE}) {
		t.Fatalf("%v", got)
	}
	cases := map[string]string{
		"look":       "look",
		"a\nb":       "a\r\nb",
		"a\r\nb":     "a\r\nb",
		"a\rb":       "a\r\nb",
		"a\x00b":     "ab",
		"x\n":        "x\r\n",
		"\xffq":      "\xff\xffq",
		"\n\n":       "\r\n\r\n",
		"a\r\n\r\nb": "a\r\n\r\nb",
	}
	for in, want := range cases {
		if got := string(EncodeLine(in)); got != want {
			t.Errorf("EncodeLine(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizer(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"a\nb"}, "a\r\nb"},
		{[]string{"a\r\nb"}, "a\r\nb"},
		{[]string{"a\n\rb"}, "a\r\nb"},
		{[]string{"a\r\x00b"}, "a\rb"},
		{[]string{"a\n\r\n\rb"}, "a\r\n\r\nb"},
		{[]string{"a\r", "\nb"}, "a\r\nb"},             // CR LF split across reads
		{[]string{"a\n", "\rb"}, "a\r\nb"},             // LF CR split across reads
		{[]string{"a\r", "\x00b"}, "a\rb"},             // CR NUL split across reads
		{[]string{"HP: [10/10] > "}, "HP: [10/10] > "}, // prompt: emitted immediately
		{[]string{"\x1b[31mred\x1b[0m\n"}, "\x1b[31mred\x1b[0m\r\n"},
	}
	for _, c := range cases {
		var n Normalizer
		var out []byte
		for _, chunk := range c.in {
			out = n.Append(out, []byte(chunk))
		}
		if string(out) != c.want {
			t.Errorf("%q => %q want %q", c.in, out, c.want)
		}
	}
}

// ---- Client / negotiation ----

type harness struct {
	t       *testing.T
	c       *Client
	p       *Parser
	sent    bytes.Buffer
	text    bytes.Buffer
	echo    []bool
	gmcp    []string
	prompts int
}

func newHarness(t *testing.T, cfg ClientConfig) *harness {
	h := &harness{t: t, p: NewParser(0)}
	h.c = NewClient(cfg, Callbacks{
		Write:  func(b []byte) error { h.sent.Write(b); return nil },
		Text:   func(b []byte) { h.text.Write(b) },
		Echo:   func(r bool) { h.echo = append(h.echo, r) },
		GMCP:   func(b []byte) { h.gmcp = append(h.gmcp, string(b)) },
		Prompt: func() { h.prompts++ },
	})
	return h
}
func (h *harness) feed(b ...byte) { h.p.Feed(b, h.c) }
func (h *harness) take() []byte {
	b := append([]byte(nil), h.sent.Bytes()...)
	h.sent.Reset()
	return b
}
func (h *harness) expect(want ...byte) {
	h.t.Helper()
	if got := h.take(); !bytes.Equal(got, want) {
		h.t.Fatalf("sent %v want %v", got, want)
	}
}

func TestNegotiateEchoAndSGA(t *testing.T) {
	h := newHarness(t, ClientConfig{})
	h.feed(IAC, WILL, OptEcho)
	h.expect(IAC, DO, OptEcho)
	if !reflect.DeepEqual(h.echo, []bool{true}) {
		t.Fatalf("echo=%v", h.echo)
	}
	h.feed(IAC, WILL, OptEcho) // repeat: must not answer again (loop avoidance)
	h.expect()
	h.feed(IAC, WONT, OptEcho)
	h.expect(IAC, DONT, OptEcho)
	if !reflect.DeepEqual(h.echo, []bool{true, false}) {
		t.Fatalf("echo=%v", h.echo)
	}
	h.feed(IAC, WILL, OptSGA)
	h.expect(IAC, DO, OptSGA)
}

func TestUnsupportedOptionsRefusedOnce(t *testing.T) {
	h := newHarness(t, ClientConfig{})
	h.feed(IAC, WILL, OptMCCP2)
	h.expect(IAC, DONT, OptMCCP2)
	h.feed(IAC, WILL, OptMCCP2)
	h.expect() // no ping-pong
	h.feed(IAC, DO, OptCharset)
	h.expect(IAC, WONT, OptCharset)
	h.feed(IAC, DO, OptCharset)
	h.expect()
	h.feed(IAC, WONT, OptMCCP2) // we never enabled it: stay silent
	h.expect()
	h.feed(IAC, DONT, OptCharset)
	h.expect()
}

func TestGMCPDisabledIsRefused(t *testing.T) {
	h := newHarness(t, ClientConfig{GMCP: false})
	h.feed(IAC, WILL, OptGMCP)
	h.expect(IAC, DONT, OptGMCP)
}

func TestNAWS(t *testing.T) {
	h := newHarness(t, ClientConfig{})
	h.c.SetWindowSize(100, 30) // before negotiation: only recorded
	h.expect()
	h.feed(IAC, DO, OptNAWS)
	h.expect(append([]byte{IAC, WILL, OptNAWS}, NAWS(100, 30)...)...)
	h.c.SetWindowSize(120, 40)
	h.expect(NAWS(120, 40)...)
	h.c.SetWindowSize(120, 40) // unchanged: nothing
	h.expect()
	h.c.SetWindowSize(0, 0) // invalid: ignored
	h.expect()
	h.feed(IAC, DONT, OptNAWS)
	h.expect(IAC, WONT, OptNAWS)
	h.c.SetWindowSize(90, 20)
	h.expect()
}

func TestTTypeCycle(t *testing.T) {
	h := newHarness(t, ClientConfig{TerminalTypes: []string{"A", "B", "MTTS 13"}})
	h.feed(IAC, SB, OptTType, TTypeSend, IAC, SE) // before WILL: ignored
	h.expect()
	h.feed(IAC, DO, OptTType)
	h.expect(IAC, WILL, OptTType)
	for _, want := range []string{"A", "B", "MTTS 13", "MTTS 13"} {
		h.feed(IAC, SB, OptTType, TTypeSend, IAC, SE)
		h.expect(TTypeIsReply(want)...)
	}
}

func TestGMCPHandshakeAndMessages(t *testing.T) {
	h := newHarness(t, ClientConfig{
		GMCP:         true,
		GMCPHello:    map[string]string{"client": "Tellwire", "version": "1"},
		GMCPSupports: []string{"Char 1", "Room 1"},
	})
	h.feed(IAC, SB, OptGMCP, 'X', IAC, SE) // before enabled: dropped
	if len(h.gmcp) != 0 {
		t.Fatal("GMCP delivered before negotiation")
	}
	h.feed(IAC, WILL, OptGMCP)
	want := append([]byte{IAC, DO, OptGMCP}, Subnegotiation(OptGMCP, []byte(`Core.Hello {"client":"Tellwire","version":"1"}`))...)
	want = append(want, Subnegotiation(OptGMCP, []byte(`Core.Supports.Set ["Char 1","Room 1"]`))...)
	h.expect(want...)
	h.feed(IAC, WILL, OptGMCP) // a server re-offering must not repeat the handshake
	h.expect()
	h.feed(append(append([]byte{IAC, SB, OptGMCP}, `Char.Vitals {"hp":5}`...), IAC, SE)...)
	if !reflect.DeepEqual(h.gmcp, []string{`Char.Vitals {"hp":5}`}) {
		t.Fatalf("gmcp=%q", h.gmcp)
	}
	if !h.c.SendGMCP("Core.Ping", nil) {
		t.Fatal("SendGMCP refused")
	}
	h.expect(Subnegotiation(OptGMCP, []byte("Core.Ping"))...)
}

func TestPromptMarkers(t *testing.T) {
	h := newHarness(t, ClientConfig{})
	h.feed('>', IAC, GA, IAC, EOR)
	if h.prompts != 2 || h.text.String() != ">" {
		t.Fatalf("prompts=%d text=%q", h.prompts, h.text.String())
	}
}

func TestWriteErrorIsSticky(t *testing.T) {
	c := NewClient(ClientConfig{}, Callbacks{
		Write: func([]byte) error { return fmt.Errorf("boom") },
		Text:  func([]byte) {},
	})
	NewParser(0).Feed([]byte{IAC, WILL, OptEcho}, c)
	if c.Err() == nil {
		t.Fatal("expected error")
	}
}
