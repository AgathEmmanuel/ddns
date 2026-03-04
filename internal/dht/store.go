package dht

import (
	"sync"
	"time"

	"github.com/agath/ddns/pkg/proto"
)

const recordExpiry = 25 * time.Hour

// LocalStore holds name records this node is responsible for.
type LocalStore struct {
	records map[[20]byte]*proto.NameRecord
	expiry  map[[20]byte]time.Time
	mu      sync.RWMutex
}

func newLocalStore() *LocalStore {
	return &LocalStore{
		records: make(map[[20]byte]*proto.NameRecord),
		expiry:  make(map[[20]byte]time.Time),
	}
}

// put stores a record. If a conflicting record exists, conflict resolution is applied.
func (s *LocalStore) put(key [20]byte, record *proto.NameRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.records[key]
	if ok {
		// Conflict resolution is handled externally; caller passes the winner.
		_ = existing
	}
	s.records[key] = record
	s.expiry[key] = time.Now().Add(recordExpiry)
}

// putIfWins stores the record only if it wins conflict resolution against any existing record.
// Returns the stored record (winner).
func (s *LocalStore) putIfWins(key [20]byte, incoming *proto.NameRecord, resolve func(existing, incoming *proto.NameRecord) *proto.NameRecord) *proto.NameRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.records[key]
	var winner *proto.NameRecord
	if ok {
		winner = resolve(existing, incoming)
	} else {
		winner = incoming
	}
	s.records[key] = winner
	s.expiry[key] = time.Now().Add(recordExpiry)
	return winner
}

// get retrieves a record by key. Returns nil if not found or expired.
func (s *LocalStore) get(key [20]byte) *proto.NameRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.expiry[key]
	if !ok || time.Now().After(exp) {
		return nil
	}
	return s.records[key]
}

// all returns all non-expired records and their keys.
func (s *LocalStore) all() map[[20]byte]*proto.NameRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make(map[[20]byte]*proto.NameRecord, len(s.records))
	for k, r := range s.records {
		if !now.After(s.expiry[k]) {
			out[k] = r
		}
	}
	return out
}

// expire removes stale records. Should be called periodically.
func (s *LocalStore) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.expiry {
		if now.After(exp) {
			delete(s.records, k)
			delete(s.expiry, k)
		}
	}
}
