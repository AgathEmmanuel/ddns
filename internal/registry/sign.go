package registry

import (
	"crypto/ed25519"
	"fmt"

	"github.com/agath/ddns/pkg/proto"
)

// SignRecord computes the PoW nonce and signs the record with privKey.
// The record must have all fields set except PowNonce and Signature.
// Difficulty defaults to DefaultDifficulty if PowDiff is 0.
func SignRecord(r *proto.NameRecord, privKey ed25519.PrivateKey, difficulty uint8) error {
	if difficulty == 0 {
		difficulty = DefaultDifficulty
	}
	r.PowDiff = difficulty

	// Compute PoW over ContentPayload (excludes PowNonce).
	contentPayload := ContentPayload(r)
	nonce, err := ComputePow(contentPayload, difficulty)
	if err != nil {
		return fmt.Errorf("registry: pow: %w", err)
	}
	r.PowNonce = nonce

	// Sign over the full payload (includes PowNonce so signature commits to the PoW work).
	fullPayload := SignedPayload(r)
	r.Signature = ed25519.Sign(privKey, fullPayload)
	return nil
}

// VerifyRecord verifies the PoW and signature on a record.
func VerifyRecord(r *proto.NameRecord) bool {
	if len(r.PublicKey) != ed25519.PublicKeySize {
		return false
	}
	if len(r.Signature) != ed25519.SignatureSize {
		return false
	}
	// Verify PoW against ContentPayload (same as what was used during signing).
	contentPayload := ContentPayload(r)
	if !VerifyPow(contentPayload, r.PowNonce, r.PowDiff) {
		return false
	}
	// Verify signature against the full payload (includes PowNonce).
	fullPayload := SignedPayload(r)
	return ed25519.Verify(ed25519.PublicKey(r.PublicKey), fullPayload, r.Signature)
}
