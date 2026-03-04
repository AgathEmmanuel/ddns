package registry

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/agath/ddns/pkg/proto"
)

// ContentPayload returns the canonical byte sequence used for PoW computation.
// It covers all record fields EXCEPT PowNonce and Signature.
// Format: name|hex(pubkey)|addr1,addr2,...|txt1,txt2,...|created_unix|updated_unix|ttl|pow_diff
func ContentPayload(r *proto.NameRecord) []byte {
	addrs := make([]string, len(r.Addrs))
	copy(addrs, r.Addrs)
	sort.Strings(addrs)

	txts := make([]string, len(r.TXT))
	copy(txts, r.TXT)
	sort.Strings(txts)

	parts := []string{
		r.Name,
		hex.EncodeToString(r.PublicKey),
		strings.Join(addrs, ","),
		strings.Join(txts, ","),
		strconv.FormatInt(r.CreatedAt.Unix(), 10),
		strconv.FormatInt(r.UpdatedAt.Unix(), 10),
		strconv.FormatUint(uint64(r.TTL), 10),
		strconv.FormatUint(uint64(r.PowDiff), 10),
	}
	return []byte(strings.Join(parts, "|"))
}

// SignedPayload returns the canonical byte sequence covered by the record's Signature.
// It extends ContentPayload with the PowNonce, so the signature commits to the PoW work.
// Format: <ContentPayload>|pow_nonce
func SignedPayload(r *proto.NameRecord) []byte {
	content := ContentPayload(r)
	return []byte(string(content) + "|" + strconv.FormatUint(r.PowNonce, 10))
}

// Validate checks that a record has valid structure (does not verify signature or PoW).
func Validate(r *proto.NameRecord) error {
	if r.Name == "" {
		return fmt.Errorf("registry: empty name")
	}
	if !strings.HasSuffix(r.Name, ".sidenet") {
		return fmt.Errorf("registry: name must end in .sidenet")
	}
	if len(r.PublicKey) != 32 {
		return fmt.Errorf("registry: public key must be 32 bytes")
	}
	if len(r.Addrs) == 0 {
		return fmt.Errorf("registry: at least one address required")
	}
	return nil
}

// NormalizeName lowercases a name and appends .sidenet if missing.
func NormalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".")
	if !strings.HasSuffix(name, ".sidenet") {
		name = name + ".sidenet"
	}
	return name
}
