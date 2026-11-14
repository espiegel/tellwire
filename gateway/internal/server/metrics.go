package server

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
)

// metrics are cheap process-wide counters, rendered in Prometheus text format.
type metrics struct {
	active        atomic.Int64
	total         atomic.Uint64
	bytesUp       atomic.Uint64 // browser -> MUD (after encoding)
	bytesDown     atomic.Uint64 // MUD -> browser (terminal output)
	gmcpMessages  atomic.Uint64
	gmcpMalformed atomic.Uint64
	mu            sync.Mutex
	ends          map[string]uint64
}

func (m *metrics) end(code string) {
	m.mu.Lock()
	if m.ends == nil {
		m.ends = map[string]uint64{}
	}
	m.ends[code]++
	m.mu.Unlock()
}

func (m *metrics) write(w io.Writer, globalLimit, ips int) {
	fmt.Fprintf(w, "# TYPE tellwire_sessions_active gauge\ntellwire_sessions_active %d\n", m.active.Load())
	fmt.Fprintf(w, "# TYPE tellwire_sessions_total counter\ntellwire_sessions_total %d\n", m.total.Load())
	fmt.Fprintf(w, "# TYPE tellwire_bytes_up_total counter\ntellwire_bytes_up_total %d\n", m.bytesUp.Load())
	fmt.Fprintf(w, "# TYPE tellwire_bytes_down_total counter\ntellwire_bytes_down_total %d\n", m.bytesDown.Load())
	fmt.Fprintf(w, "# TYPE tellwire_gmcp_messages_total counter\ntellwire_gmcp_messages_total %d\n", m.gmcpMessages.Load())
	fmt.Fprintf(w, "# TYPE tellwire_gmcp_malformed_total counter\ntellwire_gmcp_malformed_total %d\n", m.gmcpMalformed.Load())
	fmt.Fprintf(w, "# TYPE tellwire_distinct_ips gauge\ntellwire_distinct_ips %d\n", ips)
	fmt.Fprintf(w, "# TYPE tellwire_connection_limit gauge\ntellwire_connection_limit %d\n", globalLimit)
	m.mu.Lock()
	keys := make([]string, 0, len(m.ends))
	for k := range m.ends {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprint(w, "# TYPE tellwire_session_ends_total counter\n")
	for _, k := range keys {
		fmt.Fprintf(w, "tellwire_session_ends_total{reason=%q} %d\n", k, m.ends[k])
	}
	m.mu.Unlock()
}
