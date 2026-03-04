# DDNS Sidenet

A resilient, peer-to-peer DNS replacement that runs silently alongside the normal internet and activates automatically when global DNS infrastructure fails.

---

## The Problem

The internet's naming layer has a single point of failure: 13 logical root DNS servers controlled by a handful of organizations. If those servers — or the BGP routes pointing to them — are taken offline (cyber attack, kinetic strike, coordinated disruption), every hostname lookup fails. Browsers break. Services become unreachable by name. The underlying TCP/IP routing fabric may be perfectly intact, but nobody can find anything.

**This is not hypothetical.** CISA has documented root DNS infrastructure as critical national infrastructure. Real-world BGP hijacking events have already caused partial DNS outages.

DDNS Sidenet solves this by replacing the name resolution layer with a fully distributed system that:

- Runs **on top of existing IP routing** — no new hardware, no mesh required
- Operates **silently in the background** when DNS is healthy, forwarding queries normally
- **Switches automatically** to peer-to-peer resolution when root servers become unreachable
- Requires **zero external services** — no STUN servers, no cloud bootstrap nodes, no blockchain

```
Normal mode:    app → ddns resolver → upstream DNS (8.8.8.8) → answer
Degraded mode:  app → ddns resolver → Kademlia DHT → peer → answer
```

### What This Is Not

- Not a replacement for the full DNS namespace (`.com`, `.org` remain handled by normal DNS when available)
- Not a blockchain or token-incentivized system
- Not dependent on the internet being up — LAN/mesh networks work too
- Not a privacy tool (though it can be used alongside one)

---

## How It Works

Each node runs a local DNS resolver on `127.0.0.1:53`. It intercepts all DNS queries and routes them based on health:

| Query type | Healthy | Degraded |
|---|---|---|
| `something.sidenet` | DHT lookup (always) | DHT lookup |
| `google.com`, etc. | Forward to upstream | SERVFAIL (can't reach root) |

Names in the `.sidenet` namespace are registered by generating an Ed25519 keypair, performing a proof-of-work computation, signing the record, and publishing it into the Kademlia DHT. **Your keypair is your ownership.** No registrar, no ICANN, no fee.

Peers discover each other via:
1. **mDNS** — zero-config LAN discovery (`_ddns._udp.local.`)
2. **UDP broadcast** — fallback for networks where mDNS is blocked
3. **Seed IPs** — hardcoded into the binary or passed via `--seeds`

---

## Quick Start

### Prerequisites

- Go 1.24+
- Linux/macOS (Windows: DNS redirect requires manual config)
- Root access for port 53 (or use `--dns-addr` for an alternate port)

### Build

```bash
git clone https://github.com/agath/ddns
cd ddns
make build
# Binary: bin/ddns
```

### Generate Your Identity

```bash
./bin/ddns keygen
# Generated keypair in /home/you/.ddns
# Public key: a3f9b2...
```

Your keypair is stored in `~/.ddns/identity.key` (private, mode 0600) and `~/.ddns/identity.pub`. This keypair owns any names you register. **Back it up.**

### Start a Node

```bash
# With DNS interception (requires root — redirects /etc/resolv.conf)
sudo ./bin/ddns start

# Without root — use a non-privileged port and configure manually
./bin/ddns start --dns-addr 127.0.0.1:5353
```

The node will:
1. Start the DHT listener on `:4242`
2. Start the DNS resolver on `127.0.0.1:53`
3. Begin mDNS and broadcast discovery
4. Start probing root servers every 30 seconds

### Register a Name

```bash
./bin/ddns register myserver 192.168.1.100
# Computing proof-of-work (difficulty=16)...
# Signed. Nonce=43821
# Registered: myserver.sidenet -> 192.168.1.100
# DHT key: a3f9b2c1...

# Custom TTL
./bin/ddns register myserver 192.168.1.100 --ttl 7200

# With explicit seed nodes (if no LAN peers yet)
./bin/ddns register myserver 192.168.1.100 --seeds 10.0.0.5:4242
```

> Names are automatically suffixed with `.sidenet` if not present.

### Resolve a Name

```bash
./bin/ddns resolve myserver
# Name:    myserver.sidenet
# Addrs:   192.168.1.100
# Owner:   a3f9b2...
# Created: 2024-01-15T10:30:00Z
# TTL:     3600s

# Or just use the OS resolver (when ddns is running as your DNS):
ping myserver.sidenet
dig myserver.sidenet @127.0.0.1
```

### Check Status

```bash
./bin/ddns status
# Node ID:    7f3a9c...
# Addr:       [::]:4242
# Peers:      12
# DNS health: healthy
# Identity:   a3f9b2...
# Data dir:   /home/you/.ddns
```

---

## Two-Node Local Test

```bash
# Terminal 1 — first node (no seeds needed)
make node-a
# Starts on :4242, DNS on 127.0.0.1:5353

# Terminal 2 — second node, bootstraps from first
make node-b
# Starts on :4243, DNS on 127.0.0.1:5354, seeds=127.0.0.1:4242

# Terminal 3 — register from node-a
./bin/ddns register hello 10.0.0.1 --seeds 127.0.0.1:4242

# Resolve from node-b
./bin/ddns resolve hello --seeds 127.0.0.1:4243
# → hello.sidenet -> 10.0.0.1
```

---

## Running Tests

```bash
# All tests
make test

# Verbose output
make test-verbose

# Specific package
go test ./internal/dht/... -v
go test ./internal/registry/... -v
```

### Test Coverage

| Package | Tests |
|---|---|
| `internal/dht` | XOR distance, bucket index, k-bucket eviction, PING/PONG, 10-node cluster convergence, publish/resolve end-to-end |
| `internal/registry` | PoW difficulty, PoW compute+verify, Ed25519 sign/verify, conflict resolution (4 cases), name normalization |

---

## CLI Reference

```
ddns [--data-dir DIR] [--listen ADDR] <command>

Global Flags:
  --data-dir string   data directory for keys and state  (default ~/.ddns)
  --listen string     DHT UDP listen address             (default :4242)

Commands:
  start               Start DHT node + DNS resolver
    --dns-addr ADDR   DNS listen address    (default 127.0.0.1:53)
    --seeds LIST      Seed node ip:port,... (comma separated)

  register NAME IP    Register a .sidenet name
    --ttl SECONDS     Record TTL            (default 3600)
    --seeds LIST      Seed nodes

  resolve NAME        Resolve a .sidenet name via DHT
    --timeout SECS    Query timeout         (default 5)
    --seeds LIST      Seed nodes

  keygen              Generate a new Ed25519 keypair

  status              Show node, peer count, and DNS health
    --seeds LIST      Seed nodes
```

---

## Configuration

All settings can be controlled via CLI flags. A config file (`~/.ddns/config.toml`) is planned for future releases.

| Setting | Default | Description |
|---|---|---|
| `--listen` | `:4242` | DHT UDP listen address |
| `--dns-addr` | `127.0.0.1:53` | DNS resolver listen address |
| `--data-dir` | `~/.ddns` | Keys, routing table, state |
| `--seeds` | (none) | Additional bootstrap peers |

### Port 53 Without Root

```bash
# Grant capability (Linux) — run once, persists across reboots
sudo setcap cap_net_bind_service+ep bin/ddns

# Then run without sudo
./bin/ddns start
```

---

## Security Model

**Name ownership** is based purely on Ed25519 keypairs. A record is valid if and only if:
1. The signature verifies against the `PublicKey` field using the canonical payload
2. The proof-of-work nonce satisfies the difficulty requirement (first 16 bits of SHA-256 hash are zero)

**Sybil resistance**: Registration requires ~65,000 SHA-256 operations (~6ms on modern hardware). Enough to deter bulk squatting while being instant for legitimate users.

**Conflict resolution**: When two network partitions merge and both registered the same name, the oldest `CreatedAt` timestamp wins. The timestamp is immutable — it's covered by the signature, so it cannot be backdated. A 60-second tie window exists to handle clock skew.

**Crash recovery**: If the process dies without restoring `/etc/resolv.conf`, the next startup detects the leftover backup and restores it before doing anything else.

---

## Build Targets

```bash
make build          # Build for current platform -> bin/ddns
make install        # Install to $GOPATH/bin
make test           # Run all tests
make test-verbose   # Run tests with -v
make lint           # go vet + staticcheck
make release        # Cross-compile: linux/darwin/windows amd64+arm64
make clean          # Remove bin/
make node-a         # Run first local test node (:4242, dns :5353)
make node-b         # Run second local test node (:4243, dns :5354)
```

---

## Roadmap

- [ ] Persistent routing table (survive restarts without re-bootstrapping)
- [ ] Record update flow (re-register with same keypair, newer `UpdatedAt`)
- [ ] `ddns peers` command (list known peers with RTT)
- [ ] Unix socket IPC (so CLI commands work against a running `ddns start` daemon)
- [ ] NAT hole punching for strict-NAT peers
- [ ] Multi-address records (failover, load balancing)
- [ ] `import-seed` / `export-seed` commands
- [ ] Config file (`~/.ddns/config.toml`)
- [ ] Systemd unit file
- [ ] TXT record support (arbitrary metadata)
- [ ] Record revocation (signed tombstone)

---

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/miekg/dns` | DNS wire format server/client |
| `github.com/spf13/cobra` | CLI subcommands |
| `github.com/grandcat/zeroconf` | mDNS (RFC 6762) |
| Go stdlib only for everything else | crypto/ed25519, crypto/sha256, net, log/slog |

All crypto primitives (Ed25519, SHA-256) are from the Go standard library.
