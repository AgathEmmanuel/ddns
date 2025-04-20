
# 🌐 Decentralized Dynamic DNS (DDNS)

A **fully decentralized, peer-to-peer DNS system** where nodes dynamically register, resolve, and maintain domains using DHT, NAT traversal, gossip, and blockchain. Designed to operate independently or alongside the existing internet, enabling open and resilient global domain resolution.

---

## 🧠 Project Vision

- No central DNS authority
- Dynamic node discovery and domain registration
- Domain ownership via blockchain (Polkadot/Substrate)
- NAT traversal using hole punching (STUN/TURN)
- IPv4/IPv6 support with public/private peer roles
- Self-healing, scalable mesh network via Gossip + DHT

---

## 📁 Project Structure

```bash
ddns/
├── cmd/
│   ├── peer/               # Peer CLI app
│   ├── seed/               # Seed node starter
│   ├── signaling-server/   # Signaling server for hole punching
├── internal/
│   ├── dht/                # DHT logic (store, publish, resolve)
│   ├── domain/             # Domain registration & resolution
│   ├── gossip/             # Gossip protocol handler
│   ├── nat/                # NAT traversal utils (STUN, TURN, hole punching)
│   ├── blockchain/         # Domain ownership + token logic via Substrate
│   ├── net/                # Peer connection, encryption, heartbeats
├── config/
│   └── config.yaml         # Network, blockchain and peer config
├── scripts/                # Setup, STUN/TURN config, IP tools
├── README.md
└── go.mod
```

---

## ⚙️ Key Components

### 🧩 1. **Seed Node**
- Known nodes, bootstraps new peers
- Maintains stable public IP
- Facilitates initial DHT gossip + hole punching
- Eligible to mine and earn DDNS tokens

### 🧩 2. **Peer Node**
- Auto-discovers peers via gossip
- Performs NAT traversal to connect to other peers
- Participates in resolution and data exchange
- Registers/resolves domains via DHT and blockchain

### 🧩 3. **Signaling Server**
- Helps NATed peers establish direct P2P connections
- Optional TURN fallback if hole punching fails
- Rewarded in tokens when aiding successful connections

### 🧩 4. **Blockchain Layer**
- Built with Substrate (Polkadot)
- Handles domain ownership, registration time windows
- Nodes pay tokens to register/renew domains
- Seeds/validators mine DDNS tokens

---

## 🛠️ How It Works

### ➕ New Node Joining
1. Downloads DDNS software
2. Connects to a known **seed node**
3. Performs hole punching to other peers
4. Gets peer list via gossip
5. Joins DHT ring and starts syncing

### 🌐 Registering a Domain
1. Pay DDNS tokens to register domain for `T` period
2. Domain stored in blockchain with associated node key
3. DHT entry published with node endpoint

### 🧭 Resolving a Domain
1. Peer queries DHT
2. Gets peer key and endpoint of domain owner
3. Tries direct connection (hole punching / TURN fallback)

### 🔁 Self-Healing via Gossip
- Each peer syncs peer list periodically
- DHT keys re-published in intervals
- Lost domains re-announced when owner rejoins

---

## 📡 Network Architecture

- Supports both IPv4 (with hole punching) and IPv6 (direct)
- Seed nodes = high uptime, public IP
- Peers = behind NAT, ephemeral
- All nodes run:
  - DHT client
  - NAT traversal module
  - Peer server

---

## 🧪 Example Commands

```bash
# Start as peer
go run cmd/peer/main.go --config=config/config.yaml

# Start a seed node
go run cmd/seed/main.go

# Register a domain
curl -X POST http://localhost:8000/register -d 'name=mydomain.ddns'

# Resolve a domain
curl http://localhost:8000/resolve?name=mydomain.ddns
```

---

## 🚀 Roadmap

- [x] P2P networking layer (gossip, hole punching)
- [x] DHT protocol (store, resolve, TTL, update)
- [x] Substrate blockchain setup (domain ownership)
- [x] Signaling server with STUN/TURN support
- [ ] Web UI for domain management
- [ ] DNS proxy daemon to route `.ddns` domains on local machine

---

## 🔐 Tokenomics (DDNS Token)

- Domain registration/renewal = costs tokens
- Seeds, validators, and signaling servers = rewarded tokens
- Token supply + emission via on-chain governance

---

## 👥 Contribution Guidelines

1. Fork the repo
2. Add a module or improve docs/tests
3. Submit PR with clear description

Please open issues for bugs, design discussions, or proposals.

---

## 📖 References

- Kademlia DHT
- libp2p hole punching (WebRTC/QUIC)
- Substrate blockchain
- STUN/TURN (RFC 5389, 5766)
