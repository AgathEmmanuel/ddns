package main

import (
	"context"
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/agath/ddns/internal/config"
	"github.com/agath/ddns/internal/dht"
	ddns "github.com/agath/ddns/internal/dns"
	"github.com/agath/ddns/internal/discovery"
	"github.com/agath/ddns/internal/health"
	"github.com/agath/ddns/internal/keystore"
	"github.com/agath/ddns/internal/registry"
	"github.com/agath/ddns/pkg/proto"
	"github.com/spf13/cobra"
)

//go:embed seeds.txt
var embeddedSeeds string

func main() {
	cfg := config.Default()

	root := &cobra.Command{
		Use:   "ddns",
		Short: "Decentralized DNS sidenet — peer-to-peer name resolution",
		Long: `ddns is a resilient DNS replacement that activates when global DNS infrastructure fails.
It uses a Kademlia DHT for name storage, Ed25519 keypairs for ownership, and automatic
LAN discovery via mDNS and UDP broadcast.`,
	}

	// Global flags
	root.PersistentFlags().StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "data directory for keys and routing table")
	root.PersistentFlags().StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "DHT UDP listen address")

	root.AddCommand(
		startCmd(cfg),
		registerCmd(cfg),
		resolveCmd(cfg),
		keygenCmd(cfg),
		statusCmd(cfg),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func startCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the DHT node and DNS resolver",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(true)

			// Crash recovery: restore resolv.conf if we died last time.
			ddns.RestoreResolvFromBackup(cfg.DataDir)

			// Start DHT node.
			node, err := dht.NewNode(cfg.ListenAddr)
			if err != nil {
				return fmt.Errorf("start: %w", err)
			}

			// Wire conflict resolver from registry package.
			node.ConflictResolve = registry.Resolve

			node.Start()
			slog.Info("dht: node started", "id", hex.EncodeToString(node.ID[:]), "addr", node.Addr)

			// Bootstrap from embedded seeds + CLI seeds.
			seeds := dht.LoadSeedsFromText(embeddedSeeds)
			seeds = append(seeds, cfg.SeedNodes...)
			if len(seeds) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				node.Bootstrap(ctx, seeds)
				cancel()
			}

			// Start LAN discovery.
			host, portStr := splitHostPort(node.Addr)
			_ = host
			dhtPort := 4242
			fmt.Sscanf(portStr, "%d", &dhtPort)

			onPeer := func(peer proto.PeerInfo) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				node.Ping(ctx, peer.Addr)
			}

			mdns := discovery.NewMDNS(node.ID, dhtPort, onPeer)
			if err := mdns.Start(); err != nil {
				slog.Warn("mdns: start failed (non-fatal)", "err", err)
			}

			bcast := discovery.NewBroadcast(node.ID, dhtPort, onPeer)
			if err := bcast.Start(); err != nil {
				slog.Warn("broadcast: start failed (non-fatal)", "err", err)
			}

			// Start health monitor.
			monitor := health.New(nil, cfg.HealthFailThreshold)
			monitor.Start(cfg.HealthProbeInterval)

			// Start DNS resolver.
			dnsServer := ddns.NewServer(cfg.DNSAddr, monitor, cfg.FallbackUpstream, node, cfg.DataDir)
			if err := dnsServer.Start(); err != nil {
				slog.Warn("dns: resolver start failed", "err", err)
			}

			slog.Info("ddns: node running", "dht", node.Addr, "dns", cfg.DNSAddr)
			slog.Info("ddns: press Ctrl+C to stop")

			// Wait for signal.
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			<-sig

			slog.Info("ddns: shutting down...")
			dnsServer.Stop()
			monitor.Stop()
			mdns.Stop()
			bcast.Stop()
			node.Stop()
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.DNSAddr, "dns-addr", cfg.DNSAddr, "DNS resolver listen address")
	cmd.Flags().StringSliceVar(&cfg.SeedNodes, "seeds", nil, "additional seed nodes (ip:port,...)")
	return cmd
}

func registerCmd(cfg *config.Config) *cobra.Command {
	var ttl uint32
	cmd := &cobra.Command{
		Use:   "register <name> <ip>",
		Short: "Register a .sidenet name",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(false)
			name := registry.NormalizeName(args[0])
			addr := args[1]

			ks := keystore.New(cfg.DataDir)
			pub, priv, err := ks.LoadOrGenerate()
			if err != nil {
				return fmt.Errorf("register: keystore: %w", err)
			}

			now := time.Now().UTC().Truncate(time.Second)
			record := &proto.NameRecord{
				Name:      name,
				PublicKey: pub,
				Addrs:     []string{addr},
				CreatedAt: now,
				UpdatedAt: now,
				TTL:       ttl,
				PowDiff:   cfg.PowDifficulty,
			}

			fmt.Printf("Computing proof-of-work (difficulty=%d)...\n", cfg.PowDifficulty)
			if err := registry.SignRecord(record, priv, cfg.PowDifficulty); err != nil {
				return fmt.Errorf("register: sign: %w", err)
			}
			fmt.Printf("Signed. Nonce=%d\n", record.PowNonce)

			// Connect to DHT and publish.
			node, err := dht.NewNode(":0")
			if err != nil {
				return err
			}
			node.Start()
			defer node.Stop()

			seeds := dht.LoadSeedsFromText(embeddedSeeds)
			seeds = append(seeds, cfg.SeedNodes...)
			if len(seeds) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				node.Bootstrap(ctx, seeds)
				cancel()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := node.Publish(ctx, record); err != nil {
				return fmt.Errorf("register: publish: %w", err)
			}

			key := sha1.Sum([]byte(name))
			fmt.Printf("Registered: %s -> %s\n", name, addr)
			fmt.Printf("DHT key: %s\n", hex.EncodeToString(key[:]))
			return nil
		},
	}
	cmd.Flags().Uint32Var(&ttl, "ttl", 3600, "record TTL in seconds")
	cmd.Flags().StringSliceVar(&cfg.SeedNodes, "seeds", nil, "seed nodes (ip:port,...)")
	return cmd
}

func resolveCmd(cfg *config.Config) *cobra.Command {
	var timeout int
	cmd := &cobra.Command{
		Use:   "resolve <name>",
		Short: "Resolve a .sidenet name via DHT",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(false)
			name := registry.NormalizeName(args[0])

			node, err := dht.NewNode(":0")
			if err != nil {
				return err
			}
			node.Start()
			defer node.Stop()

			seeds := dht.LoadSeedsFromText(embeddedSeeds)
			seeds = append(seeds, cfg.SeedNodes...)
			if len(seeds) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				node.Bootstrap(ctx, seeds)
				cancel()
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()
			record, err := node.Resolve(ctx, name)
			if err != nil {
				return fmt.Errorf("resolve: %w", err)
			}
			if record == nil {
				fmt.Printf("%s: not found\n", name)
				return nil
			}
			fmt.Printf("Name:    %s\n", record.Name)
			fmt.Printf("Addrs:   %s\n", strings.Join(record.Addrs, ", "))
			fmt.Printf("Owner:   %s\n", hex.EncodeToString(record.PublicKey))
			fmt.Printf("Created: %s\n", record.CreatedAt.Format(time.RFC3339))
			fmt.Printf("TTL:     %ds\n", record.TTL)
			return nil
		},
	}
	cmd.Flags().IntVar(&timeout, "timeout", 5, "query timeout in seconds")
	cmd.Flags().StringSliceVar(&cfg.SeedNodes, "seeds", nil, "seed nodes (ip:port,...)")
	return cmd
}

func keygenCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "keygen",
		Short: "Generate a new Ed25519 keypair",
		RunE: func(cmd *cobra.Command, args []string) error {
			ks := keystore.New(cfg.DataDir)
			pub, _, err := ks.Generate()
			if err != nil {
				return err
			}
			fmt.Printf("Generated keypair in %s\n", cfg.DataDir)
			fmt.Printf("Public key: %s\n", hex.EncodeToString(pub))
			return nil
		},
	}
}

func statusCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show node and network status",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(false)
			node, err := dht.NewNode(":0")
			if err != nil {
				return err
			}
			node.Start()
			defer node.Stop()

			seeds := dht.LoadSeedsFromText(embeddedSeeds)
			seeds = append(seeds, cfg.SeedNodes...)
			if len(seeds) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				node.Bootstrap(ctx, seeds)
				cancel()
			}

			monitor := health.New(nil, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			dnsOK := health.ProbeOnce(ctx, monitor.ProbeAddrs())
			healthStr := "degraded"
			if dnsOK {
				healthStr = "healthy"
			}

			ks := keystore.New(cfg.DataDir)
			pub, _, _ := ks.Load()
			pubStr := "(no keypair)"
			if pub != nil {
				pubStr = hex.EncodeToString(pub)
			}

			fmt.Printf("Node ID:    %s\n", hex.EncodeToString(node.ID[:]))
			fmt.Printf("Addr:       %s\n", node.Addr)
			fmt.Printf("Peers:      %d\n", node.PeerCount())
			fmt.Printf("DNS health: %s\n", healthStr)
			fmt.Printf("Identity:   %s\n", pubStr)
			fmt.Printf("Data dir:   %s\n", cfg.DataDir)
			return nil
		},
	}
}

func setupLogging(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func splitHostPort(addr string) (string, string) {
	// Simple host:port split — no error handling needed here.
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return addr, "4242"
	}
	return addr[:idx], addr[idx+1:]
}

// Ensure filepath import is used.
var _ = filepath.Join
