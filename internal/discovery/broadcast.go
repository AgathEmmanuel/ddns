package discovery

import (
	"encoding/json"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/agath/ddns/pkg/proto"
)

const broadcastPort = 4243

// BroadcastAnnounce is the payload sent via UDP broadcast.
type BroadcastAnnounce struct {
	Version uint8        `json:"v"`
	NodeID  proto.NodeID `json:"id"`
	Port    uint16       `json:"port"`
}

// BroadcastDiscovery announces this node via UDP LAN broadcast and listens for peers.
type BroadcastDiscovery struct {
	nodeID  proto.NodeID
	dhtPort int
	onPeer  func(proto.PeerInfo)
	conn    *net.UDPConn
	quit    chan struct{}
}

// NewBroadcast creates a UDP broadcast discovery handler.
func NewBroadcast(nodeID proto.NodeID, dhtPort int, onPeer func(proto.PeerInfo)) *BroadcastDiscovery {
	return &BroadcastDiscovery{
		nodeID:  nodeID,
		dhtPort: dhtPort,
		onPeer:  onPeer,
		quit:    make(chan struct{}),
	}
}

// Start binds the broadcast port and begins announcing.
func (b *BroadcastDiscovery) Start() error {
	addr, err := net.ResolveUDPAddr("udp", ":"+strconv.Itoa(broadcastPort))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	b.conn = conn
	go b.listenLoop()
	go b.announceLoop()
	slog.Info("broadcast: LAN discovery started", "port", broadcastPort)
	return nil
}

// Stop shuts down the broadcast discovery.
func (b *BroadcastDiscovery) Stop() {
	close(b.quit)
	if b.conn != nil {
		b.conn.Close()
	}
}

func (b *BroadcastDiscovery) announceLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	b.announce()
	for {
		select {
		case <-ticker.C:
			b.announce()
		case <-b.quit:
			return
		}
	}
}

func (b *BroadcastDiscovery) announce() {
	payload := BroadcastAnnounce{
		Version: 1,
		NodeID:  b.nodeID,
		Port:    uint16(b.dhtPort),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	dst, _ := net.ResolveUDPAddr("udp", "255.255.255.255:"+strconv.Itoa(broadcastPort))
	b.conn.WriteTo(data, dst)
}

func (b *BroadcastDiscovery) listenLoop() {
	buf := make([]byte, 512)
	for {
		n, src, err := b.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-b.quit:
				return
			default:
				continue
			}
		}
		var ann BroadcastAnnounce
		if err := json.Unmarshal(buf[:n], &ann); err != nil {
			continue
		}
		if ann.NodeID == b.nodeID || ann.Version != 1 {
			continue
		}
		addr := src.IP.String() + ":" + strconv.Itoa(int(ann.Port))
		peer := proto.PeerInfo{ID: ann.NodeID, Addr: addr}
		slog.Debug("broadcast: discovered peer", "addr", addr)
		b.onPeer(peer)
	}
}
