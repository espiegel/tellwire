// Command mockmud runs the deterministic mock MUD (plain or TLS Telnet).
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/espiegel/tellwire/gateway/internal/mockmud"
)

func main() {
	addr := flag.String("listen", envOr("MOCKMUD_LISTEN", ":2323"), "listen address")
	useTLS := flag.Bool("tls", os.Getenv("MOCKMUD_TLS") == "true", "serve TLS with a self-signed certificate")
	caOut := flag.String("cert-out", "", "with -tls: write the self-signed certificate (PEM) here, for GATEWAY_UPSTREAM_CA_FILE")
	nl := flag.String("newline", "", `line terminator: "crlf" (default), "lfcr" or "lf"`)
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	if *useTLS {
		cert, err := selfSigned()
		if err != nil {
			log.Fatal(err)
		}
		if *caOut != "" {
			pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
			if err := os.WriteFile(*caOut, pemBytes, 0o644); err != nil {
				log.Fatal(err)
			}
		}
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	s := &mockmud.Server{}
	switch *nl {
	case "lfcr":
		s.Newline = "\n\r"
	case "lf":
		s.Newline = "\n"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("mockmud listening on %s (tls=%v)", ln.Addr(), *useTLS)
	if err := s.Serve(ctx, ln); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	t := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mockmud"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		DNSNames: []string{"localhost", "mockmud"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
