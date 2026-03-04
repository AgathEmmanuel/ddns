package keystore

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	privKeyFile = "identity.key"
	pubKeyFile  = "identity.pub"
)

// Keystore manages an Ed25519 keypair stored in a directory.
type Keystore struct {
	dir string
}

// New returns a Keystore that uses dir for key storage.
func New(dir string) *Keystore {
	return &Keystore{dir: dir}
}

// Load loads the existing keypair. Returns an error if none exists.
func (ks *Keystore) Load() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	privPath := filepath.Join(ks.dir, privKeyFile)
	data, err := os.ReadFile(privPath)
	if err != nil {
		return nil, nil, fmt.Errorf("keystore: %w", err)
	}
	hexStr := strings.TrimSpace(string(data))
	privBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, nil, fmt.Errorf("keystore: decode private key: %w", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		return nil, nil, errors.New("keystore: invalid private key length")
	}
	priv := ed25519.PrivateKey(privBytes)
	pub := priv.Public().(ed25519.PublicKey)
	return pub, priv, nil
}

// Generate creates a new keypair and saves it to disk. Fails if a keypair already exists.
func (ks *Keystore) Generate() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	privPath := filepath.Join(ks.dir, privKeyFile)
	if _, err := os.Stat(privPath); err == nil {
		return nil, nil, errors.New("keystore: keypair already exists; delete identity.key to regenerate")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("keystore: generate: %w", err)
	}
	if err := os.MkdirAll(ks.dir, 0700); err != nil {
		return nil, nil, fmt.Errorf("keystore: mkdir: %w", err)
	}
	privHex := hex.EncodeToString(priv)
	if err := os.WriteFile(privPath, []byte(privHex+"\n"), 0600); err != nil {
		return nil, nil, fmt.Errorf("keystore: write private key: %w", err)
	}
	pubHex := hex.EncodeToString(pub)
	pubPath := filepath.Join(ks.dir, pubKeyFile)
	if err := os.WriteFile(pubPath, []byte(pubHex+"\n"), 0644); err != nil {
		return nil, nil, fmt.Errorf("keystore: write public key: %w", err)
	}
	return pub, priv, nil
}

// LoadOrGenerate loads the keypair if it exists, or generates a new one.
func (ks *Keystore) LoadOrGenerate() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ks.Load()
	if err == nil {
		return pub, priv, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return ks.Generate()
	}
	return nil, nil, err
}
