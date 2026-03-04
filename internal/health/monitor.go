package health

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the health of the upstream DNS infrastructure.
type State int32

const (
	StateHealthy  State = 0
	StateDegraded State = 1
	StateOffline  State = 2
)

func (s State) String() string {
	switch s {
	case StateHealthy:
		return "healthy"
	case StateDegraded:
		return "degraded"
	case StateOffline:
		return "offline"
	default:
		return "unknown"
	}
}

// rootServerProbes are well-known root server IPs probed directly (no DNS needed).
var rootServerProbes = []string{
	"198.41.0.4:53",    // a.root-servers.net
	"199.9.14.201:53",  // b.root-servers.net
	"192.112.36.4:53",  // g.root-servers.net
	"193.0.14.129:53",  // k.root-servers.net
}

// Monitor probes root DNS servers and tracks health state.
type Monitor struct {
	probes       []string
	failThreshold int
	state        atomic.Int32
	failCount    int
	mu           sync.Mutex
	subscribers  []chan State
	quit         chan struct{}
	wg           sync.WaitGroup
}

// New creates a new Monitor. If probes is nil, default root server IPs are used.
func New(probes []string, failThreshold int) *Monitor {
	if len(probes) == 0 {
		probes = rootServerProbes
	}
	if failThreshold <= 0 {
		failThreshold = 3
	}
	return &Monitor{
		probes:        probes,
		failThreshold: failThreshold,
		quit:          make(chan struct{}),
	}
}

// Start begins the background probe loop.
func (m *Monitor) Start(interval time.Duration) {
	m.wg.Add(1)
	go m.loop(interval)
}

// Stop shuts down the monitor.
func (m *Monitor) Stop() {
	close(m.quit)
	m.wg.Wait()
}

// State returns the current health state.
func (m *Monitor) State() State {
	return State(m.state.Load())
}

// Subscribe returns a channel that receives state changes.
func (m *Monitor) Subscribe() <-chan State {
	ch := make(chan State, 4)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch
}

func (m *Monitor) loop(baseInterval time.Duration) {
	defer m.wg.Done()
	ticker := time.NewTicker(baseInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ok := m.probe()
			m.recordResult(ok)
			// In degraded mode, probe more aggressively.
			if m.State() != StateHealthy {
				ticker.Reset(5 * time.Second)
			} else {
				ticker.Reset(baseInterval)
			}
		case <-m.quit:
			return
		}
	}
}

// probe sends a minimal DNS query (root NS) to a root server IP.
// Returns true if any server responds with a valid DNS response.
func (m *Monitor) probe() bool {
	for _, addr := range m.probes {
		if probeOne(addr) {
			return true
		}
	}
	return false
}

// probeOne sends a raw DNS query for `. NS` to addr over UDP.
func probeOne(addr string) bool {
	// Minimal DNS query: ID=0xDEAD, flags=RD, QDCOUNT=1, query for "." type NS
	query := buildRootNSQuery()
	conn, err := net.DialTimeout("udp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(query); err != nil {
		return false
	}
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil || n < 12 {
		return false
	}
	// Check that the response has QR bit set (bit 15 of flags, bytes 2-3).
	flags := binary.BigEndian.Uint16(buf[2:4])
	return flags&0x8000 != 0
}

// buildRootNSQuery constructs a minimal DNS query for the root zone NS records.
func buildRootNSQuery() []byte {
	return []byte{
		0xDE, 0xAD, // ID
		0x01, 0x00, // Flags: standard query, recursion desired
		0x00, 0x01, // QDCOUNT: 1 question
		0x00, 0x00, // ANCOUNT
		0x00, 0x00, // NSCOUNT
		0x00, 0x00, // ARCOUNT
		0x00,       // QNAME: root (empty label)
		0x00, 0x02, // QTYPE: NS
		0x00, 0x01, // QCLASS: IN
	}
}

func (m *Monitor) recordResult(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prev := State(m.state.Load())
	if ok {
		m.failCount = 0
		if prev != StateHealthy {
			m.state.Store(int32(StateHealthy))
			slog.Info("health: DNS infrastructure recovered")
			m.notify(StateHealthy)
		}
	} else {
		m.failCount++
		slog.Debug("health: probe failed", "consecutive", m.failCount)
		if m.failCount >= m.failThreshold && prev == StateHealthy {
			m.state.Store(int32(StateDegraded))
			slog.Warn("health: DNS infrastructure degraded — switching to DHT resolution")
			m.notify(StateDegraded)
		}
	}
}

func (m *Monitor) notify(s State) {
	for _, ch := range m.subscribers {
		select {
		case ch <- s:
		default:
		}
	}
}

// ForceProbe runs a single probe immediately (useful for testing).
func (m *Monitor) ForceProbe() bool {
	ok := m.probe()
	m.recordResult(ok)
	return ok
}

// SetStateForTest overrides the state directly (testing only).
func (m *Monitor) SetStateForTest(s State) {
	m.state.Store(int32(s))
}

// ProbeAddrs returns the list of probe addresses (for status display).
func (m *Monitor) ProbeAddrs() []string {
	return append([]string{}, m.probes...)
}

// ProbeOnce runs a probe without updating state (for connectivity checks).
func ProbeOnce(ctx context.Context, addrs []string) bool {
	done := make(chan bool, 1)
	go func() {
		for _, addr := range addrs {
			if probeOne(addr) {
				done <- true
				return
			}
		}
		done <- false
	}()
	select {
	case ok := <-done:
		return ok
	case <-ctx.Done():
		return false
	}
}
