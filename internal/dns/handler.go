package dns

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/agath/ddns/internal/health"
	"github.com/agath/ddns/pkg/proto"
	"github.com/miekg/dns"
)

// Resolver is the interface the handler uses to resolve .sidenet names.
type Resolver interface {
	Resolve(ctx context.Context, name string) (*proto.NameRecord, error)
}

// Handler implements miekg/dns.Handler for split-horizon DNS resolution.
type Handler struct {
	monitor  *health.Monitor
	upstream *upstreamForwarder
	resolver Resolver
}

// NewHandler creates a split-horizon DNS handler.
func NewHandler(monitor *health.Monitor, upstream *upstreamForwarder, resolver Resolver) *Handler {
	return &Handler{
		monitor:  monitor,
		upstream: upstream,
		resolver: resolver,
	}
}

// ServeDNS implements dns.Handler.
func (h *Handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	if len(r.Question) == 0 {
		return
	}
	q := r.Question[0]
	name := strings.ToLower(q.Name)

	if strings.HasSuffix(name, ".sidenet.") {
		h.handleSidenet(w, r, name, q)
		return
	}

	switch h.monitor.State() {
	case health.StateHealthy:
		resp, err := h.upstream.forward(r)
		if err != nil {
			slog.Debug("dns: upstream forward failed", "name", name, "err", err)
			m := new(dns.Msg)
			m.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(m)
			return
		}
		w.WriteMsg(resp)

	default:
		// Root servers unreachable — can't resolve non-sidenet names.
		slog.Debug("dns: dropping non-sidenet query in degraded mode", "name", name)
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		w.WriteMsg(m)
	}
}

func (h *Handler) handleSidenet(w dns.ResponseWriter, r *dns.Msg, name string, q dns.Question) {
	// Strip trailing dot: "alice.sidenet." -> "alice.sidenet"
	lookupName := strings.TrimSuffix(name, ".")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	record, err := h.resolver.Resolve(ctx, lookupName)
	if err != nil || record == nil {
		slog.Debug("dns: sidenet NXDOMAIN", "name", lookupName)
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(m)
		return
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	for _, addr := range record.Addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		hdr := dns.RR_Header{
			Name:   q.Name,
			Class:  dns.ClassINET,
			Ttl:    record.TTL,
		}
		if ip4 := ip.To4(); ip4 != nil {
			if q.Qtype == dns.TypeA || q.Qtype == dns.TypeANY {
				hdr.Rrtype = dns.TypeA
				m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: ip4})
			}
		} else {
			if q.Qtype == dns.TypeAAAA || q.Qtype == dns.TypeANY {
				hdr.Rrtype = dns.TypeAAAA
				m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: ip})
			}
		}
	}

	if len(m.Answer) == 0 {
		// Name exists but no matching record type.
		m.SetRcode(r, dns.RcodeSuccess)
	}
	w.WriteMsg(m)
}
