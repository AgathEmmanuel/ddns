package registry

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const DefaultDifficulty uint8 = 16

// MeetsDifficulty checks whether the first `difficulty` bits of hash are zero.
func MeetsDifficulty(hash []byte, difficulty uint8) bool {
	fullBytes := difficulty / 8
	for i := uint8(0); i < fullBytes; i++ {
		if hash[i] != 0 {
			return false
		}
	}
	remainder := difficulty % 8
	if remainder > 0 {
		mask := byte(0xFF << (8 - remainder))
		return hash[fullBytes]&mask == 0
	}
	return true
}

// ComputePow finds a nonce such that SHA-256(payload || nonce) meets difficulty.
// payload is the SignedPayload of the record (without signature or nonce fields).
func ComputePow(payload []byte, difficulty uint8) (uint64, error) {
	nonceBytes := make([]byte, 8)
	var nonce uint64
	for {
		binary.BigEndian.PutUint64(nonceBytes, nonce)
		h := sha256.New()
		h.Write(payload)
		h.Write(nonceBytes)
		hash := h.Sum(nil)
		if MeetsDifficulty(hash, difficulty) {
			return nonce, nil
		}
		nonce++
		if nonce == 0 {
			return 0, errors.New("pow: nonce space exhausted")
		}
	}
}

// VerifyPow checks that the record's PowNonce satisfies PowDiff against its signed payload.
// The payload passed here should be SignedPayload computed with PowNonce already set.
func VerifyPow(payload []byte, nonce uint64, difficulty uint8) bool {
	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)
	h := sha256.New()
	h.Write(payload)
	h.Write(nonceBytes)
	hash := h.Sum(nil)
	return MeetsDifficulty(hash, difficulty)
}
