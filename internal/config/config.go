package config

import (
	"os"
	"path/filepath"
	"time"
)

// Config holds all runtime configuration for the ddns node.
type Config struct {
	// Network
	ListenAddr string   `toml:"listen_addr"` // DHT UDP listen address, default ":4242"
	DNSAddr    string   `toml:"dns_addr"`    // DNS resolver listen address, default "127.0.0.1:53"
	SeedNodes  []string `toml:"seed_nodes"`

	// Storage
	DataDir string `toml:"data_dir"` // default ~/.ddns

	// Health monitor
	HealthProbeInterval time.Duration `toml:"health_probe_interval"` // default 30s
	HealthFailThreshold int           `toml:"health_fail_threshold"` // default 3

	// Upstream DNS (used when healthy)
	FallbackUpstream string `toml:"fallback_upstream"` // default "8.8.8.8:53"

	// Registration
	PowDifficulty uint8 `toml:"pow_difficulty"` // default 16
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		ListenAddr:          ":4242",
		DNSAddr:             "127.0.0.1:53",
		DataDir:             filepath.Join(home, ".ddns"),
		HealthProbeInterval: 30 * time.Second,
		HealthFailThreshold: 3,
		FallbackUpstream:    "8.8.8.8:53",
		PowDifficulty:       16,
	}
}
