package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestDefaultsRequireOrigin(t *testing.T) {
	_, err := Load(env(nil))
	if err == nil || !strings.Contains(err.Error(), "GATEWAY_ALLOWED_ORIGINS") {
		t.Fatalf("want origin error, got %v", err)
	}
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"GATEWAY_ALLOWED_ORIGINS": "https://play.dunemud.net"}))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := c.Profile("")
	if u.Host != "dunemud.net" || u.Port != 6788 || !u.TLS {
		t.Fatalf("default upstream %+v", u)
	}
	if c.MaxConnections != 500 || !c.GMCP {
		t.Fatalf("%+v", c)
	}
}

func TestRejectsWildcardAndPathOrigins(t *testing.T) {
	for _, o := range []string{"*", "https://x/", "play.dunemud.net"} {
		if _, err := Load(env(map[string]string{"GATEWAY_ALLOWED_ORIGINS": o})); err == nil {
			t.Errorf("origin %q accepted", o)
		}
	}
}

func TestUpstreams(t *testing.T) {
	ok, err := ParseUpstreams("default=dunemud.net:6788:tls, dev=dunemud.net:4241:tls,mock=mock:2323:plain")
	if err != nil || len(ok) != 3 || ok[2].TLS || ok[2].Addr() != "mock:2323" {
		t.Fatalf("%+v %v", ok, err)
	}
	for _, bad := range []string{"", "x", "a=host:1", "a=host:99999:tls", "a=host:1:ssl", "a=:1:tls", "a=h:1:tls,a=h:2:tls", "=h:1:tls", "a=h:x:tls"} {
		if _, err := ParseUpstreams(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestProfileLookupIsAllowlist(t *testing.T) {
	c, _ := Load(env(map[string]string{"GATEWAY_ALLOWED_ORIGINS": "https://a.b"}))
	if _, ok := c.Profile("evil.com:22"); ok {
		t.Fatal("arbitrary profile resolved")
	}
}

func TestBadValuesReported(t *testing.T) {
	_, err := Load(env(map[string]string{
		"GATEWAY_ALLOWED_ORIGINS": "https://a.b", "GATEWAY_MAX_CONNECTIONS": "lots",
		"GATEWAY_IDLE_TIMEOUT": "soon", "GATEWAY_TRUSTED_PROXIES": "nope",
	}))
	if err == nil {
		t.Fatal("want errors")
	}
	for _, k := range []string{"MAX_CONNECTIONS", "IDLE_TIMEOUT", "TRUSTED_PROXIES"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("missing %s in %v", k, err)
		}
	}
}
