package dht

import (
	"sync"
	"time"

	"github.com/agath/ddns/pkg/proto"
)

const K = 20 // bucket size

// entry is a peer entry in a k-bucket.
type entry struct {
	peer     proto.PeerInfo
	lastSeen time.Time
}

// kbucket holds up to K peers, ordered least-recently-seen (head) to most-recently-seen (tail).
type kbucket struct {
	entries []*entry
	mu      sync.Mutex
}

// update inserts or refreshes a peer. Returns true if a ping is needed (bucket full, head is stale candidate).
// If pingNeeded is true, candidate is the least-recently-seen peer that should be probed.
func (b *kbucket) update(peer proto.PeerInfo) (pingNeeded bool, candidate *entry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// If already present, move to tail.
	for i, e := range b.entries {
		if e.peer.ID == peer.ID {
			e.peer = peer
			e.lastSeen = time.Now()
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			b.entries = append(b.entries, e)
			return false, nil
		}
	}

	// Not present — append if space available.
	if len(b.entries) < K {
		b.entries = append(b.entries, &entry{peer: peer, lastSeen: time.Now()})
		return false, nil
	}

	// Bucket full — caller must ping head; if dead, evict and insert new peer.
	return true, b.entries[0]
}

// evictAndInsert removes the head (stale) peer and appends the new peer.
func (b *kbucket) evictAndInsert(head proto.NodeID, newPeer proto.PeerInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) > 0 && b.entries[0].peer.ID == head {
		b.entries = b.entries[1:]
	}
	b.entries = append(b.entries, &entry{peer: newPeer, lastSeen: time.Now()})
}

// closest returns the n most-recently-seen peers from this bucket.
func (b *kbucket) closest(n int) []proto.PeerInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]proto.PeerInfo, 0, n)
	for i := len(b.entries) - 1; i >= 0 && len(result) < n; i-- {
		result = append(result, b.entries[i].peer)
	}
	return result
}

// len returns the number of entries in the bucket.
func (b *kbucket) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// all returns a copy of all peers.
func (b *kbucket) all() []proto.PeerInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]proto.PeerInfo, len(b.entries))
	for i, e := range b.entries {
		out[i] = e.peer
	}
	return out
}
