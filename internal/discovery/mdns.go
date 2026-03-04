package discovery

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/agath/ddns/pkg/proto"
	"github.com/grandcat/zeroconf"
)

const (
	mdnsService = "_ddns._udp"
	mdssDomain  = "local."
)

// MDNSDiscovery announces this node via mDNS and browses for peers.
type MDNSDiscovery struct {
	nodeID   proto.NodeID
	dhtPort  int
	server   *zeroconf.Server
	onPeer   func(proto.PeerInfo)
	quit     chan struct{}
}

// NewMDNS creates an mDNS discovery handler.
// onPeer is called for each discovered peer.
func NewMDNS(nodeID proto.NodeID, dhtPort int, onPeer func(proto.PeerInfo)) *MDNSDiscovery {
	return &MDNSDiscovery{
		nodeID:  nodeID,
		dhtPort: dhtPort,
		onPeer:  onPeer,
		quit:    make(chan struct{}),
	}
}

// Start registers this node and begins browsing.
func (m *MDNSDiscovery) Start() error {
	instance := "ddns-" + hex.EncodeToString(m.nodeID[:4])
	txt := []string{
		"v=1",
		"id=" + hex.EncodeToString(m.nodeID[:]),
	}
	server, err := zeroconf.Register(instance, mdnsService, mdssDomain, m.dhtPort, txt, nil)
	if err != nil {
		return fmt.Errorf("mdns: register: %w", err)
	}
	m.server = server
	slog.Info("mdns: registered", "instance", instance, "port", m.dhtPort)

	go m.browse()
	return nil
}

// Stop deregisters this node.
func (m *MDNSDiscovery) Stop() {
	close(m.quit)
	if m.server != nil {
		m.server.Shutdown()
	}
}

func (m *MDNSDiscovery) browse() {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		slog.Error("mdns: browse resolver error", "err", err)
		return
	}

	entries := make(chan *zeroconf.ServiceEntry, 16)
	go func() {
		for entry := range entries {
			m.handleEntry(entry)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-m.quit
		cancel()
	}()

	for {
		if err := resolver.Browse(ctx, mdnsService, mdssDomain, entries); err != nil {
			select {
			case <-m.quit:
				return
			default:
				slog.Debug("mdns: browse error, retrying", "err", err)
				time.Sleep(5 * time.Second)
			}
		}
		select {
		case <-m.quit:
			return
		default:
			time.Sleep(30 * time.Second)
		}
	}
}

func (m *MDNSDiscovery) handleEntry(entry *zeroconf.ServiceEntry) {
	// Parse node ID from TXT records.
	var nodeIDHex string
	for _, txt := range entry.Text {
		if strings.HasPrefix(txt, "id=") {
			nodeIDHex = strings.TrimPrefix(txt, "id=")
		}
	}
	if nodeIDHex == "" {
		return
	}
	idBytes, err := hex.DecodeString(nodeIDHex)
	if err != nil || len(idBytes) != 20 {
		return
	}
	var nodeID proto.NodeID
	copy(nodeID[:], idBytes)

	// Skip ourselves.
	if nodeID == m.nodeID {
		return
	}

	// Use the first IPv4 address reported.
	for _, ip := range entry.AddrIPv4 {
		addr := ip.String() + ":" + strconv.Itoa(entry.Port)
		peer := proto.PeerInfo{ID: nodeID, Addr: addr}
		slog.Debug("mdns: discovered peer", "addr", addr)
		m.onPeer(peer)
		return
	}
	for _, ip := range entry.AddrIPv6 {
		addr := "[" + ip.String() + "]:" + strconv.Itoa(entry.Port)
		peer := proto.PeerInfo{ID: nodeID, Addr: addr}
		slog.Debug("mdns: discovered peer (ipv6)", "addr", addr)
		m.onPeer(peer)
		return
	}
}
