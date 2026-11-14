// Package config loads gateway settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Upstream is one allowlisted destination. Clients select one by name only;
// they can never supply a host or port.
type Upstream struct {
	Name       string
	Host       string
	Port       int
	TLS        bool
	ServerName string // TLS server name for verification; defaults to Host
}

// Addr returns host:port.
func (u Upstream) Addr() string { return net.JoinHostPort(u.Host, strconv.Itoa(u.Port)) }

// Config is the complete gateway configuration.
type Config struct {
	Listen          string
	AllowedOrigins  []string
	Upstreams       []Upstream // first entry is the default profile
	UpstreamCAFile  string     // optional extra root CAs (PEM) for upstream TLS
	TrustedProxies  []netip.Prefix
	MaxConnections  int
	MaxPerIP        int
	IdleTimeout     time.Duration
	DialTimeout     time.Duration
	WriteTimeout    time.Duration
	PingInterval    time.Duration
	ShutdownTimeout time.Duration
	MaxMessageBytes int64
	MaxSubnegBytes  int
	OutQueue        int // buffered frames between upstream reader and WebSocket writer
	InputLinesPerS  float64
	InputBurst      int

	GMCP         bool
	GMCPHello    bool
	GMCPSupports []string
	ClientName   string
	ClientVer    string

	LogLevel  string
	LogFormat string
}

// Lookup resolves an environment variable.
type Lookup func(key string) (string, bool)

// Load builds a Config from the environment, applying defaults and validating.
func Load(get Lookup) (*Config, error) {
	var errs []error
	str := func(k, def string) string {
		if v, ok := get(k); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	num := func(k string, def int) int {
		s := str(k, "")
		if s == "" {
			return def
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("%s: %q is not a non-negative integer", k, s))
			return def
		}
		return n
	}
	dur := func(k string, def time.Duration) time.Duration {
		s := str(k, "")
		if s == "" {
			return def
		}
		d, err := time.ParseDuration(s)
		if err != nil || d < 0 {
			errs = append(errs, fmt.Errorf("%s: %q is not a valid duration", k, s))
			return def
		}
		return d
	}
	boolean := func(k string, def bool) bool {
		s := str(k, "")
		if s == "" {
			return def
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %q is not a boolean", k, s))
			return def
		}
		return b
	}
	list := func(k, def string) []string {
		var out []string
		for _, p := range strings.Split(str(k, def), ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}

	c := &Config{
		Listen:          str("GATEWAY_LISTEN", ":8080"),
		AllowedOrigins:  list("GATEWAY_ALLOWED_ORIGINS", ""),
		UpstreamCAFile:  str("GATEWAY_UPSTREAM_CA_FILE", ""),
		MaxConnections:  num("GATEWAY_MAX_CONNECTIONS", 500),
		MaxPerIP:        num("GATEWAY_MAX_CONNECTIONS_PER_IP", 10),
		IdleTimeout:     dur("GATEWAY_IDLE_TIMEOUT", 2*time.Hour),
		DialTimeout:     dur("GATEWAY_DIAL_TIMEOUT", 10*time.Second),
		WriteTimeout:    dur("GATEWAY_WRITE_TIMEOUT", 15*time.Second),
		PingInterval:    dur("GATEWAY_PING_INTERVAL", 30*time.Second),
		ShutdownTimeout: dur("GATEWAY_SHUTDOWN_TIMEOUT", 15*time.Second),
		MaxMessageBytes: int64(num("GATEWAY_MAX_MESSAGE_BYTES", 16*1024)),
		MaxSubnegBytes:  num("GATEWAY_MAX_SUBNEG_BYTES", 64*1024),
		OutQueue:        num("GATEWAY_OUT_QUEUE", 64),
		InputBurst:      num("GATEWAY_INPUT_BURST", 40),
		GMCP:            boolean("GATEWAY_GMCP", true),
		GMCPHello:       boolean("GATEWAY_GMCP_HELLO", true),
		GMCPSupports:    list("GATEWAY_GMCP_SUPPORTS", "Char 1,Room 1,Comm.Channel 1"),
		ClientName:      str("GATEWAY_CLIENT_NAME", "Tellwire"),
		ClientVer:       str("GATEWAY_CLIENT_VERSION", "0.1.0"),
		LogLevel:        str("GATEWAY_LOG_LEVEL", "info"),
		LogFormat:       str("GATEWAY_LOG_FORMAT", "json"),
	}
	if f, err := strconv.ParseFloat(str("GATEWAY_INPUT_LINES_PER_SEC", "10"), 64); err == nil && f >= 0 {
		c.InputLinesPerS = f
	} else {
		errs = append(errs, errors.New("GATEWAY_INPUT_LINES_PER_SEC: not a non-negative number"))
	}

	var err error
	if c.Upstreams, err = ParseUpstreams(str("GATEWAY_UPSTREAMS", "default=dunemud.net:6788:tls")); err != nil {
		errs = append(errs, err)
	}
	for _, p := range list("GATEWAY_TRUSTED_PROXIES", "") {
		if pf, err := netip.ParsePrefix(p); err == nil {
			c.TrustedProxies = append(c.TrustedProxies, pf)
		} else if a, err2 := netip.ParseAddr(p); err2 == nil {
			c.TrustedProxies = append(c.TrustedProxies, netip.PrefixFrom(a, a.BitLen()))
		} else {
			errs = append(errs, fmt.Errorf("GATEWAY_TRUSTED_PROXIES: %q is not an IP or CIDR", p))
		}
	}
	if len(c.AllowedOrigins) == 0 {
		errs = append(errs, errors.New("GATEWAY_ALLOWED_ORIGINS must list at least one origin, e.g. https://play.dunemud.net"))
	}
	for _, o := range c.AllowedOrigins {
		if !strings.Contains(o, "://") || strings.HasSuffix(o, "/") || o == "*" {
			errs = append(errs, fmt.Errorf("GATEWAY_ALLOWED_ORIGINS: %q must be an exact origin like https://host[:port] (no wildcard, no path)", o))
		}
	}
	if c.MaxMessageBytes < 256 {
		errs = append(errs, errors.New("GATEWAY_MAX_MESSAGE_BYTES must be at least 256"))
	}
	if c.OutQueue < 1 {
		errs = append(errs, errors.New("GATEWAY_OUT_QUEUE must be at least 1"))
	}
	return c, errors.Join(errs...)
}

// ParseUpstreams parses "name=host:port:tls|plain,name2=...".
func ParseUpstreams(s string) ([]Upstream, error) {
	var out []Upstream
	seen := map[string]bool{}
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, spec, ok := strings.Cut(item, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("GATEWAY_UPSTREAMS: %q must look like name=host:port:tls", item)
		}
		i := strings.LastIndex(spec, ":")
		if i < 0 {
			return nil, fmt.Errorf("GATEWAY_UPSTREAMS: %q is missing :tls or :plain", item)
		}
		hostport, mode := spec[:i], spec[i+1:]
		var useTLS bool
		switch mode {
		case "tls":
			useTLS = true
		case "plain":
		default:
			return nil, fmt.Errorf("GATEWAY_UPSTREAMS: %q: last field must be tls or plain", item)
		}
		host, portStr, err := net.SplitHostPort(hostport)
		if err != nil || host == "" {
			return nil, fmt.Errorf("GATEWAY_UPSTREAMS: %q: want host:port (%v)", item, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("GATEWAY_UPSTREAMS: %q: bad port", item)
		}
		if seen[name] {
			return nil, fmt.Errorf("GATEWAY_UPSTREAMS: duplicate profile %q", name)
		}
		seen[name] = true
		out = append(out, Upstream{Name: name, Host: host, Port: port, TLS: useTLS})
	}
	if len(out) == 0 {
		return nil, errors.New("GATEWAY_UPSTREAMS: at least one upstream is required")
	}
	return out, nil
}

// Profile returns the named upstream; an empty name selects the default.
func (c *Config) Profile(name string) (Upstream, bool) {
	if name == "" {
		return c.Upstreams[0], true
	}
	for _, u := range c.Upstreams {
		if u.Name == name {
			return u, true
		}
	}
	return Upstream{}, false
}
