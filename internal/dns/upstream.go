package dns

import (
	"net"
	"time"

	"github.com/miekg/dns"
)

// upstreamForwarder forwards DNS queries to an upstream resolver.
type upstreamForwarder struct {
	addr   string
	client *dns.Client
}

func newUpstreamForwarder(addr string) *upstreamForwarder {
	return &upstreamForwarder{
		addr: addr,
		client: &dns.Client{
			Net:     "udp",
			Timeout: 5 * time.Second,
		},
	}
}

// forward sends msg to the upstream resolver and returns the response.
func (u *upstreamForwarder) forward(msg *dns.Msg) (*dns.Msg, error) {
	resp, _, err := u.client.Exchange(msg, u.addr)
	if err != nil {
		// Try TCP fallback (some upstreams require it for large responses).
		tcpClient := &dns.Client{Net: "tcp", Timeout: 5 * time.Second}
		resp, _, err = tcpClient.Exchange(msg, u.addr)
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// isPrivateIP checks if an IP is in a private/loopback range.
func isPrivateIP(ip net.IP) bool {
	private := []net.IPNet{
		{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
		{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)},
		{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)},
		{IP: net.ParseIP("127.0.0.0"), Mask: net.CIDRMask(8, 32)},
	}
	for _, net := range private {
		if net.Contains(ip) {
			return true
		}
	}
	return false
}
