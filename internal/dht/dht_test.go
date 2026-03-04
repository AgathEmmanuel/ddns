package dht

import (
	"context"
	"testing"
	"time"

	"github.com/agath/ddns/pkg/proto"
)

// TestXorDistance verifies XOR distance and bucket index calculations.
func TestXorDistance(t *testing.T) {
	var a, b proto.NodeID
	a[0] = 0xFF
	b[0] = 0x0F

	d := xorDistance(a, b)
	if d[0] != 0xF0 {
		t.Fatalf("expected 0xF0, got 0x%02X", d[0])
	}

	// Identical nodes have distance 0.
	d2 := xorDistance(a, a)
	for _, v := range d2 {
		if v != 0 {
			t.Fatal("identical nodes should have zero distance")
		}
	}
}

func TestBucketIndex(t *testing.T) {
	var self proto.NodeID
	var peer proto.NodeID
	peer[0] = 0x80 // highest bit set in byte 0 → bucket 159

	idx := bucketIndex(self, peer)
	if idx != 159 {
		t.Fatalf("expected bucket 159, got %d", idx)
	}

	peer2 := self
	peer2[19] = 0x01 // lowest bit set in last byte → bucket 0
	idx2 := bucketIndex(self, peer2)
	if idx2 != 0 {
		t.Fatalf("expected bucket 0, got %d", idx2)
	}
}

// TestKBucketEviction verifies that a full bucket evicts the least-recently-seen node
// when the head is unresponsive, and keeps it when responsive.
func TestKBucketEviction(t *testing.T) {
	b := &kbucket{}

	// Fill the bucket.
	for i := 0; i < K; i++ {
		var id proto.NodeID
		id[0] = byte(i + 1)
		b.update(proto.PeerInfo{ID: id, Addr: "127.0.0.1:0"})
	}
	if b.len() != K {
		t.Fatalf("expected %d entries, got %d", K, b.len())
	}

	// Add one more — bucket is full, should need a ping.
	var newID proto.NodeID
	newID[0] = 0xFF
	pingNeeded, candidate := b.update(proto.PeerInfo{ID: newID, Addr: "127.0.0.1:9999"})
	if !pingNeeded {
		t.Fatal("expected pingNeeded=true for full bucket")
	}
	if candidate == nil {
		t.Fatal("expected non-nil candidate")
	}

	// Evict the head and insert new peer.
	b.evictAndInsert(candidate.peer.ID, proto.PeerInfo{ID: newID, Addr: "127.0.0.1:9999"})
	if b.len() != K {
		t.Fatalf("after eviction: expected %d entries, got %d", K, b.len())
	}
}

// TestNodePingPong verifies that two nodes can exchange PING/PONG.
func TestNodePingPong(t *testing.T) {
	a, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a.Start()
	b.Start()
	defer a.Stop()
	defer b.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	peer, err := a.Ping(ctx, b.Addr)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	if peer.ID != b.ID {
		t.Fatalf("ping returned wrong ID: got %x, want %x", peer.ID, b.ID)
	}
}

// TestDHTClusterConvergence spins up 10 nodes and verifies they can find each other.
func TestDHTClusterConvergence(t *testing.T) {
	const n = 10
	nodes := make([]*Node, n)
	for i := range nodes {
		node, err := NewNode("127.0.0.1:0")
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
		nodes[i] = node
		node.Start()
	}
	defer func() {
		for _, node := range nodes {
			node.Stop()
		}
	}()

	// Bootstrap all nodes through nodes[0].
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 1; i < n; i++ {
		nodes[i].Bootstrap(ctx, []string{nodes[0].Addr})
	}

	time.Sleep(500 * time.Millisecond)

	// Each node should know at least half the others.
	for i, node := range nodes {
		count := node.PeerCount()
		if count < n/2 {
			t.Errorf("node %d: only knows %d peers (want >= %d)", i, count, n/2)
		}
	}
}

// TestNamePublishResolve verifies end-to-end name publish and resolution across nodes.
func TestNamePublishResolve(t *testing.T) {
	publisher, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewNode("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	publisher.Start()
	resolver.Start()
	defer publisher.Stop()
	defer resolver.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Bootstrap resolver through publisher.
	resolver.Bootstrap(ctx, []string{publisher.Addr})

	record := &proto.NameRecord{
		Name:      "alice.sidenet",
		PublicKey: make([]byte, 32),
		Addrs:     []string{"192.168.1.42"},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		TTL:       3600,
	}

	if err := publisher.Publish(ctx, record); err != nil {
		t.Fatalf("publish: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	result, err := resolver.Resolve(ctx, "alice.sidenet")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result == nil {
		t.Fatal("resolve: got nil record")
	}
	if result.Addrs[0] != "192.168.1.42" {
		t.Fatalf("resolve: expected 192.168.1.42, got %s", result.Addrs[0])
	}
}
