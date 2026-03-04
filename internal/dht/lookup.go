package dht

import (
	"context"
	"sort"
	"sync"

	"github.com/agath/ddns/pkg/proto"
)

const alpha = 3 // parallel lookup concurrency

// sortedCandidates is a list of peers sorted by XOR distance to a target, capped at K.
type sortedCandidates struct {
	target  proto.NodeID
	entries []proto.PeerInfo
	seen    map[proto.NodeID]bool
	mu      sync.Mutex
}

func newSortedCandidates(target proto.NodeID) *sortedCandidates {
	return &sortedCandidates{
		target: target,
		seen:   make(map[proto.NodeID]bool),
	}
}

// add inserts a peer if not already seen. Returns true if the sorted list improved.
func (s *sortedCandidates) add(p proto.PeerInfo) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[p.ID] {
		return false
	}
	s.seen[p.ID] = true
	s.entries = append(s.entries, p)
	sort.Slice(s.entries, func(i, j int) bool {
		di := xorDistance(s.target, s.entries[i].ID)
		dj := xorDistance(s.target, s.entries[j].ID)
		return xorLess(di, dj)
	})
	if len(s.entries) > K {
		s.entries = s.entries[:K]
	}
	return true
}

// addAll adds multiple peers, returns true if any improved the set.
func (s *sortedCandidates) addAll(peers []proto.PeerInfo) bool {
	improved := false
	for _, p := range peers {
		if s.add(p) {
			improved = true
		}
	}
	return improved
}

// unqueried returns up to n peers not yet in the queried set.
func (s *sortedCandidates) unqueried(queried map[proto.NodeID]bool, n int) []proto.PeerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]proto.PeerInfo, 0, n)
	for _, p := range s.entries {
		if !queried[p.ID] {
			result = append(result, p)
			if len(result) == n {
				break
			}
		}
	}
	return result
}

// top returns up to n closest peers.
func (s *sortedCandidates) top(n int) []proto.PeerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) < n {
		return append([]proto.PeerInfo{}, s.entries...)
	}
	return append([]proto.PeerInfo{}, s.entries[:n]...)
}

// lookupNodes performs an iterative Kademlia FIND_NODE for target.
func (n *Node) lookupNodes(ctx context.Context, target proto.NodeID) []proto.PeerInfo {
	candidates := newSortedCandidates(target)
	candidates.addAll(n.table.closest(target, alpha))

	queried := make(map[proto.NodeID]bool)

	for {
		batch := candidates.unqueried(queried, alpha)
		if len(batch) == 0 {
			break
		}

		newPeers := n.parallelFindNode(ctx, batch, target)
		improved := candidates.addAll(newPeers)
		for _, p := range batch {
			queried[p.ID] = true
		}

		if !improved {
			// Final round: query all unqueried from top-K
			finalBatch := candidates.unqueried(queried, K)
			if len(finalBatch) > 0 {
				extra := n.parallelFindNode(ctx, finalBatch, target)
				candidates.addAll(extra)
			}
			break
		}
	}
	return candidates.top(K)
}

// lookupValue performs an iterative Kademlia FIND_VALUE for key.
// Returns the record if found, otherwise nil (with the k closest nodes populated).
func (n *Node) lookupValue(ctx context.Context, key [20]byte) (*proto.NameRecord, []proto.PeerInfo) {
	target := proto.NodeID(key)
	candidates := newSortedCandidates(target)
	candidates.addAll(n.table.closest(target, alpha))

	queried := make(map[proto.NodeID]bool)

	for {
		batch := candidates.unqueried(queried, alpha)
		if len(batch) == 0 {
			break
		}

		record, newPeers := n.parallelFindValue(ctx, batch, key)
		if record != nil {
			return record, nil
		}
		improved := candidates.addAll(newPeers)
		for _, p := range batch {
			queried[p.ID] = true
		}
		if !improved {
			break
		}
	}
	return nil, candidates.top(K)
}

// parallelFindNode sends FIND_NODE to batch peers concurrently and collects new peers.
func (n *Node) parallelFindNode(ctx context.Context, batch []proto.PeerInfo, target proto.NodeID) []proto.PeerInfo {
	type result struct {
		peers []proto.PeerInfo
	}
	ch := make(chan result, len(batch))

	payload, _ := marshalPayload(proto.FindNodeMsg{Target: target})

	for _, peer := range batch {
		go func(p proto.PeerInfo) {
			env := proto.Envelope{
				Type:   proto.MsgFindNode,
				Sender: n.selfInfo(),
				Payload: payload,
			}
			resp, err := n.rpc.call(ctx, p.Addr, env)
			if err != nil {
				ch <- result{}
				return
			}
			n.table.update(p, n)
			var msg proto.FoundNodeMsg
			if err := unmarshalPayload(resp.Payload, &msg); err != nil {
				ch <- result{}
				return
			}
			ch <- result{peers: msg.Peers}
		}(peer)
	}

	var all []proto.PeerInfo
	for range batch {
		r := <-ch
		all = append(all, r.peers...)
	}
	return all
}

// parallelFindValue sends FIND_VALUE to batch peers concurrently.
// Returns the first record found, or collected peers.
func (n *Node) parallelFindValue(ctx context.Context, batch []proto.PeerInfo, key [20]byte) (*proto.NameRecord, []proto.PeerInfo) {
	type result struct {
		record *proto.NameRecord
		peers  []proto.PeerInfo
	}
	ch := make(chan result, len(batch))

	payload, _ := marshalPayload(proto.FindValueMsg{Key: key})

	for _, peer := range batch {
		go func(p proto.PeerInfo) {
			env := proto.Envelope{
				Type:    proto.MsgFindValue,
				Sender:  n.selfInfo(),
				Payload: payload,
			}
			resp, err := n.rpc.call(ctx, p.Addr, env)
			if err != nil {
				ch <- result{}
				return
			}
			n.table.update(p, n)
			var msg proto.FoundValueMsg
			if err := unmarshalPayload(resp.Payload, &msg); err != nil {
				ch <- result{}
				return
			}
			if msg.Found && msg.Record != nil {
				ch <- result{record: msg.Record}
			} else {
				ch <- result{peers: msg.Peers}
			}
		}(peer)
	}

	var allPeers []proto.PeerInfo
	for range batch {
		r := <-ch
		if r.record != nil {
			// Drain remaining goroutines
			go func() {
				for i := 1; i < len(batch); i++ {
					<-ch
				}
			}()
			return r.record, nil
		}
		allPeers = append(allPeers, r.peers...)
	}
	return nil, allPeers
}

// xorDistance returns the XOR of two NodeIDs.
func xorDistance(a, b proto.NodeID) proto.NodeID {
	var d proto.NodeID
	for i := range a {
		d[i] = a[i] ^ b[i]
	}
	return d
}

// xorLess returns true if a is closer (smaller XOR) than b.
func xorLess(a, b proto.NodeID) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// bucketIndex returns which k-bucket (0-159) a peer belongs to,
// defined as the index of the highest set bit in the XOR distance.
func bucketIndex(self, peer proto.NodeID) int {
	d := xorDistance(self, peer)
	for i := 0; i < 20; i++ {
		if d[i] == 0 {
			continue
		}
		for bit := 7; bit >= 0; bit-- {
			if d[i]&(1<<uint(bit)) != 0 {
				return (19-i)*8 + bit
			}
		}
	}
	return -1 // identical IDs
}

