package dht

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/agath/ddns/pkg/proto"
)

const bucketCount = 160

// routingTable holds 160 k-buckets.
type routingTable struct {
	self    proto.NodeID
	buckets [bucketCount]*kbucket
	mu      sync.RWMutex
}

func newRoutingTable(self proto.NodeID) *routingTable {
	rt := &routingTable{self: self}
	for i := range rt.buckets {
		rt.buckets[i] = &kbucket{}
	}
	return rt
}

// update inserts or refreshes a peer in the appropriate bucket.
// If the bucket is full and the head peer is unresponsive, it evicts and inserts.
func (rt *routingTable) update(peer proto.PeerInfo, n *Node) {
	if peer.ID == rt.self {
		return
	}
	idx := bucketIndex(rt.self, peer.ID)
	if idx < 0 {
		return
	}
	bucket := rt.buckets[idx]
	pingNeeded, candidate := bucket.update(peer)
	if !pingNeeded {
		return
	}
	// Ping least-recently-seen in background; evict if dead.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		env := proto.Envelope{
			Type:   proto.MsgPing,
			Sender: n.selfInfo(),
		}
		_, err := n.rpc.call(ctx, candidate.peer.Addr, env)
		if err != nil {
			// Head is dead — evict and insert new peer.
			bucket.evictAndInsert(candidate.peer.ID, peer)
		}
		// If alive, new peer is silently dropped (standard Kademlia behavior).
	}()
}

// closest returns the n peers closest to target across all buckets.
func (rt *routingTable) closest(target proto.NodeID, n int) []proto.PeerInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var all []proto.PeerInfo
	for _, b := range rt.buckets {
		all = append(all, b.all()...)
	}
	// Sort by XOR distance to target.
	sorted := make([]proto.PeerInfo, len(all))
	copy(sorted, all)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			di := xorDistance(target, sorted[j].ID)
			dj := xorDistance(target, sorted[j-1].ID)
			if xorLess(di, dj) {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			} else {
				break
			}
		}
	}
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

// size returns the total number of known peers.
func (rt *routingTable) size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	total := 0
	for _, b := range rt.buckets {
		total += b.len()
	}
	return total
}

// Node is a Kademlia DHT node.
type Node struct {
	ID    proto.NodeID
	Addr  string
	conn  *net.UDPConn
	table *routingTable
	store *LocalStore
	rpc   *rpcLayer
	quit  chan struct{}
	wg    sync.WaitGroup

	// PeerDiscovered is called when a new peer is found via external discovery (mDNS, broadcast).
	PeerDiscovered func(peer proto.PeerInfo)

	// ConflictResolve resolves conflicting name records. Set by registry package.
	ConflictResolve func(existing, incoming *proto.NameRecord) *proto.NameRecord
}

// NewNode creates a new DHT node with the given listen address.
// addr should be "host:port" or ":port" for all interfaces.
func NewNode(addr string) (*Node, error) {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("dht: listen %s: %w", addr, err)
	}
	udpConn := conn.(*net.UDPConn)

	id := generateNodeID()
	n := &Node{
		ID:    id,
		Addr:  udpConn.LocalAddr().String(),
		conn:  udpConn,
		table: newRoutingTable(id),
		store: newLocalStore(),
		rpc:   newRPCLayer(udpConn),
		quit:  make(chan struct{}),
	}
	return n, nil
}

// NewNodeWithID creates a node with a specific ID (useful for testing).
func NewNodeWithID(addr string, id proto.NodeID) (*Node, error) {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("dht: listen %s: %w", addr, err)
	}
	udpConn := conn.(*net.UDPConn)
	n := &Node{
		ID:    id,
		Addr:  udpConn.LocalAddr().String(),
		conn:  udpConn,
		table: newRoutingTable(id),
		store: newLocalStore(),
		rpc:   newRPCLayer(udpConn),
		quit:  make(chan struct{}),
	}
	return n, nil
}

// Start begins the UDP read loop. Must be called before any operations.
func (n *Node) Start() {
	n.wg.Add(1)
	go n.readLoop()
	n.wg.Add(1)
	go n.maintenanceLoop()
}

// Stop shuts down the node gracefully.
func (n *Node) Stop() {
	close(n.quit)
	n.conn.Close()
	n.wg.Wait()
}

// Bootstrap connects to seed peers and performs a self-lookup to populate the routing table.
func (n *Node) Bootstrap(ctx context.Context, seeds []string) {
	for _, seed := range seeds {
		if seed == n.Addr {
			continue
		}
		// Derive a placeholder NodeID from the address (will be corrected after PONG).
		id := addrToNodeID(seed)
		peer := proto.PeerInfo{ID: id, Addr: seed}
		pingCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
		resp, err := n.rpc.call(pingCtx, peer.Addr, proto.Envelope{
			Type:   proto.MsgPing,
			Sender: n.selfInfo(),
		})
		cancel()
		if err != nil {
			slog.Debug("bootstrap: seed unreachable", "addr", seed, "err", err)
			continue
		}
		n.table.update(resp.Sender, n)
		slog.Debug("bootstrap: connected to seed", "addr", seed, "id", hex.EncodeToString(resp.Sender.ID[:]))
	}
	// Self-lookup to populate routing table.
	n.lookupNodes(ctx, n.ID)
}

// Ping sends a ping to a peer and returns the response peer info.
func (n *Node) Ping(ctx context.Context, addr string) (proto.PeerInfo, error) {
	resp, err := n.rpc.call(ctx, addr, proto.Envelope{
		Type:   proto.MsgPing,
		Sender: n.selfInfo(),
	})
	if err != nil {
		return proto.PeerInfo{}, err
	}
	n.table.update(resp.Sender, n)
	return resp.Sender, nil
}

// Publish stores a NameRecord in the DHT at the k nodes closest to sha1(name).
func (n *Node) Publish(ctx context.Context, record *proto.NameRecord) error {
	key := sha1.Sum([]byte(record.Name))
	target := proto.NodeID(key)
	closest := n.lookupNodes(ctx, target)
	if len(closest) == 0 {
		// Store locally as fallback.
		n.store.putIfWins(key, record, n.resolveConflict)
		return nil
	}
	payload, err := marshalPayload(proto.StoreMsg{Key: key, Record: *record})
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, peer := range closest {
		wg.Add(1)
		go func(p proto.PeerInfo) {
			defer wg.Done()
			env := proto.Envelope{
				Type:    proto.MsgStore,
				Sender:  n.selfInfo(),
				Payload: payload,
			}
			_, err := n.rpc.call(ctx, p.Addr, env)
			if err != nil {
				slog.Debug("publish: store failed", "peer", p.Addr, "err", err)
			}
		}(peer)
	}
	wg.Wait()
	return nil
}

// Resolve looks up a name in the DHT and returns the record, or nil if not found.
func (n *Node) Resolve(ctx context.Context, name string) (*proto.NameRecord, error) {
	key := sha1.Sum([]byte(name))
	// Check local store first.
	if r := n.store.get(key); r != nil {
		return r, nil
	}
	record, _ := n.lookupValue(ctx, key)
	return record, nil
}

// PeerCount returns the number of known peers.
func (n *Node) PeerCount() int {
	return n.table.size()
}

// readLoop processes inbound UDP packets.
func (n *Node) readLoop() {
	defer n.wg.Done()
	buf := make([]byte, 65536)
	for {
		nr, addr, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-n.quit:
				return
			default:
				slog.Warn("dht: read error", "err", err)
				continue
			}
		}
		var env proto.Envelope
		if err := json.Unmarshal(buf[:nr], &env); err != nil {
			slog.Debug("dht: unmarshal error", "from", addr, "err", err)
			continue
		}
		// Canonicalize sender address using the observed UDP source addr.
		if env.Sender.Addr == "" {
			env.Sender.Addr = addr.String()
		}
		// Update routing table for every valid message received.
		if env.Sender.ID != (proto.NodeID{}) {
			n.table.update(env.Sender, n)
		}
		// If it's a response to a pending call, dispatch it.
		if n.rpc.dispatch(env) {
			continue
		}
		// Otherwise it's an inbound request — handle it.
		go n.handleRequest(env, addr)
	}
}

// handleRequest processes an inbound DHT request and sends a response.
func (n *Node) handleRequest(env proto.Envelope, from *net.UDPAddr) {
	switch env.Type {
	case proto.MsgPing:
		resp := proto.Envelope{
			Type:   proto.MsgPong,
			TxID:   env.TxID,
			Sender: n.selfInfo(),
		}
		n.rpc.send(from.String(), resp)

	case proto.MsgFindNode:
		var msg proto.FindNodeMsg
		if err := unmarshalPayload(env.Payload, &msg); err != nil {
			return
		}
		peers := n.table.closest(msg.Target, K)
		payload, _ := marshalPayload(proto.FoundNodeMsg{Peers: peers})
		resp := proto.Envelope{
			Type:    proto.MsgFoundNode,
			TxID:    env.TxID,
			Sender:  n.selfInfo(),
			Payload: payload,
		}
		n.rpc.send(from.String(), resp)

	case proto.MsgFindValue:
		var msg proto.FindValueMsg
		if err := unmarshalPayload(env.Payload, &msg); err != nil {
			return
		}
		var payload []byte
		if record := n.store.get(msg.Key); record != nil {
			payload, _ = marshalPayload(proto.FoundValueMsg{Found: true, Record: record})
		} else {
			target := proto.NodeID(msg.Key)
			peers := n.table.closest(target, K)
			payload, _ = marshalPayload(proto.FoundValueMsg{Found: false, Peers: peers})
		}
		resp := proto.Envelope{
			Type:    proto.MsgFoundValue,
			TxID:    env.TxID,
			Sender:  n.selfInfo(),
			Payload: payload,
		}
		n.rpc.send(from.String(), resp)

	case proto.MsgStore:
		var msg proto.StoreMsg
		if err := unmarshalPayload(env.Payload, &msg); err != nil {
			return
		}
		n.store.putIfWins(msg.Key, &msg.Record, n.resolveConflict)
		resp := proto.Envelope{
			Type:   proto.MsgStored,
			TxID:   env.TxID,
			Sender: n.selfInfo(),
		}
		n.rpc.send(from.String(), resp)
	}
}

// maintenanceLoop runs periodic store expiry.
func (n *Node) maintenanceLoop() {
	defer n.wg.Done()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			n.store.expire()
		case <-n.quit:
			return
		}
	}
}

// selfInfo returns this node's PeerInfo.
func (n *Node) selfInfo() proto.PeerInfo {
	return proto.PeerInfo{ID: n.ID, Addr: n.Addr}
}

// resolveConflict is the default conflict resolver (last-write-wins by UpdatedAt for same owner,
// oldest CreatedAt for different owners). Overridden by registry package.
func (n *Node) resolveConflict(existing, incoming *proto.NameRecord) *proto.NameRecord {
	if n.ConflictResolve != nil {
		return n.ConflictResolve(existing, incoming)
	}
	// Fallback: same owner → newer UpdatedAt wins; different owner → older CreatedAt wins.
	if string(existing.PublicKey) == string(incoming.PublicKey) {
		if incoming.UpdatedAt.After(existing.UpdatedAt) {
			return incoming
		}
		return existing
	}
	if incoming.CreatedAt.Before(existing.CreatedAt) {
		return incoming
	}
	return existing
}

// generateNodeID generates a random 20-byte node ID.
func generateNodeID() proto.NodeID {
	var id proto.NodeID
	rand.Read(id[:])
	return id
}

// addrToNodeID derives a placeholder node ID from an address string.
func addrToNodeID(addr string) proto.NodeID {
	return proto.NodeID(sha1.Sum([]byte(addr)))
}

// LoadSeedsFromText parses a seeds.txt content (lines of "ip:port", # = comment).
func LoadSeedsFromText(content string) []string {
	var seeds []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seeds = append(seeds, line)
	}
	return seeds
}
