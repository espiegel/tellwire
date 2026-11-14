package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// limiter enforces global and per-IP concurrent-connection caps.
type limiter struct {
	mu       sync.Mutex
	global   int
	perIP    map[string]int
	maxTotal int
	maxIP    int
}

type limitKind int

const (
	limitOK limitKind = iota
	limitGlobal
	limitIP
)

func newLimiter(maxTotal, maxIP int) *limiter {
	return &limiter{perIP: map[string]int{}, maxTotal: maxTotal, maxIP: maxIP}
}

// acquire reserves a slot. A zero limit means unlimited. The returned release
// func must be called exactly once when ok.
func (l *limiter) acquire(ip string) (release func(), kind limitKind) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxTotal > 0 && l.global >= l.maxTotal {
		return nil, limitGlobal
	}
	if l.maxIP > 0 && l.perIP[ip] >= l.maxIP {
		return nil, limitIP
	}
	l.global++
	l.perIP[ip]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.global--
			if l.perIP[ip]--; l.perIP[ip] <= 0 {
				delete(l.perIP, ip)
			}
		})
	}, limitOK
}

func (l *limiter) counts() (global, distinctIPs int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.global, len(l.perIP)
}

// clientIP determines the client address. X-Forwarded-For is honoured only
// when the direct peer is a configured trusted proxy, and then only the
// rightmost entry not itself a trusted proxy is believed (earlier entries are
// client-controlled).
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if len(trusted) == 0 || !isTrusted(peer, trusted) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		if h == "" {
			continue
		}
		if !isTrusted(h, trusted) {
			return h
		}
	}
	return peer
}

func isTrusted(s string, trusted []netip.Prefix) bool {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// bucket is a small token bucket (not goroutine-safe; one per session reader).
type bucket struct {
	tokens, burst, rate float64
	last                time.Time
}

func newBucket(rate float64, burst int) *bucket {
	return &bucket{tokens: float64(burst), burst: float64(burst), rate: rate, last: time.Now()}
}

// allow consumes one token. rate<=0 disables limiting.
func (b *bucket) allow() bool {
	if b.rate <= 0 {
		return true
	}
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
