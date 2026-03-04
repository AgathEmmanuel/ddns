package proto

import "time"

// NodeID is a 20-byte Kademlia node identifier.
type NodeID [20]byte

// NameRecord is the canonical record stored in the DHT for a .sidenet name.
type NameRecord struct {
	Name      string    `json:"n"`
	PublicKey []byte    `json:"pk"`
	Signature []byte    `json:"sig"`
	Addrs     []string  `json:"a"`
	TXT       []string  `json:"txt,omitempty"`
	CreatedAt time.Time `json:"ca"`
	UpdatedAt time.Time `json:"ua"`
	TTL       uint32    `json:"ttl"`
	PowNonce  uint64    `json:"nonce"`
	PowDiff   uint8     `json:"diff"`
}

// PeerInfo identifies a node in the network.
type PeerInfo struct {
	ID   NodeID `json:"id"`
	Addr string `json:"a"` // "ip:port"
}

// MessageType identifies the DHT message kind.
type MessageType uint8

const (
	MsgPing       MessageType = 1
	MsgPong       MessageType = 2
	MsgFindNode   MessageType = 3
	MsgFoundNode  MessageType = 4
	MsgFindValue  MessageType = 5
	MsgFoundValue MessageType = 6
	MsgStore      MessageType = 7
	MsgStored     MessageType = 8
)

// Envelope wraps every DHT UDP message.
type Envelope struct {
	Type    MessageType `json:"t"`
	TxID    [4]byte     `json:"id"`
	Sender  PeerInfo    `json:"s"`
	Payload []byte      `json:"p,omitempty"`
}

// FindNodeMsg requests the k closest nodes to Target.
type FindNodeMsg struct {
	Target NodeID `json:"t"`
}

// FoundNodeMsg responds with up to k closest peers.
type FoundNodeMsg struct {
	Peers []PeerInfo `json:"peers"`
}

// FindValueMsg requests the record for Key (SHA-1 of name).
type FindValueMsg struct {
	Key [20]byte `json:"k"`
}

// FoundValueMsg either returns the record or closer peers.
type FoundValueMsg struct {
	Found  bool        `json:"found"`
	Record *NameRecord `json:"r,omitempty"`
	Peers  []PeerInfo  `json:"peers,omitempty"`
}

// StoreMsg replicates a record to a peer.
type StoreMsg struct {
	Key    [20]byte   `json:"k"`
	Record NameRecord `json:"r"`
}
