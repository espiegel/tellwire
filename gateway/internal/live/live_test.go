// Package live holds the OPT-IN probe against the real DuneMUD server.
//
// It is skipped unless TELLWIRE_LIVE=1. When enabled it connects, verifies the
// TLS certificate against the system roots, answers Telnet negotiation exactly
// like the gateway does, prints what the server offers, and disconnects. It
// never logs in and never sends a game command or any text.
//
//	TELLWIRE_LIVE=1 go test -v -run TestLive ./internal/live/
//	TELLWIRE_LIVE=1 LIVE_ADDR=dunemud.net:6788 LIVE_SERVER_NAME=dunemud.net go test -v ./internal/live/
package live

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/espiegel/tellwire/gateway/internal/gmcp"
	"github.com/espiegel/tellwire/gateway/internal/telnet"
)

type recorder struct {
	*telnet.Client
	offers []string
	text   bytes.Buffer
}

func (r *recorder) Negotiate(verb, opt byte) {
	name := map[byte]string{telnet.WILL: "WILL", telnet.WONT: "WONT", telnet.DO: "DO", telnet.DONT: "DONT"}[verb]
	r.offers = append(r.offers, fmt.Sprintf("%s %d", name, opt))
	r.Client.Negotiate(verb, opt)
}

func TestLive(t *testing.T) {
	if os.Getenv("TELLWIRE_LIVE") != "1" {
		t.Skip("live probe disabled; set TELLWIRE_LIVE=1 to run it against the real server")
	}
	addr := os.Getenv("LIVE_ADDR")
	if addr == "" {
		addr = "dunemud.net:6788"
	}
	host, _, _ := net.SplitHostPort(addr)
	if sn := os.Getenv("LIVE_SERVER_NAME"); sn != "" {
		host = sn
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}
	raw, err := d.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("TLS connect/verify to %s failed (certificate NOT trusted or host unreachable): %v", addr, err)
	}
	defer raw.Close()
	st := raw.(*tls.Conn).ConnectionState()
	leaf := st.PeerCertificates[0]
	t.Logf("TLS ok: version=%x cipher=%s subject=%q issuer=%q names=%v notAfter=%s",
		st.Version, tls.CipherSuiteName(st.CipherSuite), leaf.Subject.CommonName, leaf.Issuer.CommonName, leaf.DNSNames, leaf.NotAfter.Format(time.RFC3339))

	rec := &recorder{}
	var gm []string
	rec.Client = telnet.NewClient(telnet.ClientConfig{
		TerminalTypes: telnet.DefaultTerminalTypes("Tellwire"), GMCP: true,
		GMCPHello: map[string]string{"client": "Tellwire", "version": "probe"}, GMCPSupports: []string{"Char 1", "Room 1", "Comm.Channel 1"},
	}, telnet.Callbacks{
		Write: func(b []byte) error {
			_ = raw.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, e := raw.Write(b)
			return e
		},
		Text: func(b []byte) { rec.text.Write(b) },
		GMCP: func(p []byte) { gm = append(gm, gmcp.Parse(p).Package) },
	})
	p := telnet.NewParser(0)
	buf := make([]byte, 4096)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		_ = raw.SetReadDeadline(deadline)
		n, err := raw.Read(buf)
		if n > 0 {
			p.Feed(buf[:n], rec)
		}
		if err != nil {
			break
		}
	}
	t.Logf("server negotiation: %s", strings.Join(rec.offers, ", "))
	t.Logf("GMCP enabled by server: %v; packages seen before login: %v", rec.RemoteEnabled(telnet.OptGMCP), gm)
	t.Logf("banner (%d bytes, first 300): %q", rec.text.Len(), truncate(rec.text.String(), 300))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
