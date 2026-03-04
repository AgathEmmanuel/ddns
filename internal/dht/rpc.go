package dht

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/agath/ddns/pkg/proto"
)

const rpcTimeout = 2 * time.Second

var ErrTimeout = fmt.Errorf("dht: rpc timeout")

type pendingRPC struct {
	ch chan proto.Envelope
}

// rpcLayer manages outbound UDP RPCs with TxID-based request/response matching.
type rpcLayer struct {
	conn     *net.UDPConn
	inflight map[[4]byte]*pendingRPC
	mu       sync.Mutex
}

func newRPCLayer(conn *net.UDPConn) *rpcLayer {
	return &rpcLayer{
		conn:     conn,
		inflight: make(map[[4]byte]*pendingRPC),
	}
}

// send dispatches an outbound envelope, not expecting a response.
func (r *rpcLayer) send(addr string, env proto.Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	_, err = r.conn.WriteTo(data, udpAddr)
	return err
}

// call sends an envelope and waits for a response with matching TxID.
func (r *rpcLayer) call(ctx context.Context, addr string, env proto.Envelope) (proto.Envelope, error) {
	txID := randomTxID()
	env.TxID = txID

	pending := &pendingRPC{ch: make(chan proto.Envelope, 1)}
	r.mu.Lock()
	r.inflight[txID] = pending
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.inflight, txID)
		r.mu.Unlock()
	}()

	if err := r.send(addr, env); err != nil {
		return proto.Envelope{}, err
	}

	timer := time.NewTimer(rpcTimeout)
	defer timer.Stop()

	select {
	case resp := <-pending.ch:
		return resp, nil
	case <-timer.C:
		return proto.Envelope{}, ErrTimeout
	case <-ctx.Done():
		return proto.Envelope{}, ctx.Err()
	}
}

// dispatch routes an inbound envelope to a waiting call, if any.
func (r *rpcLayer) dispatch(env proto.Envelope) bool {
	r.mu.Lock()
	pending, ok := r.inflight[env.TxID]
	r.mu.Unlock()
	if ok {
		select {
		case pending.ch <- env:
		default:
		}
	}
	return ok
}

func randomTxID() [4]byte {
	var id [4]byte
	rand.Read(id[:])
	return id
}

// marshalPayload encodes a message to JSON bytes for embedding in Envelope.Payload.
func marshalPayload(v any) ([]byte, error) {
	return json.Marshal(v)
}

// unmarshalPayload decodes an Envelope.Payload into a concrete message struct.
func unmarshalPayload(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// nodeIDUint encodes a NodeID as a uint64 from the first 8 bytes (for sorting).
func nodeIDUint(id proto.NodeID) uint64 {
	return binary.BigEndian.Uint64(id[:8])
}
