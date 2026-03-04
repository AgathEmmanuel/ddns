package registry

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/agath/ddns/pkg/proto"
)

func TestPowMeetsDifficulty(t *testing.T) {
	// hash with first 2 bytes zero meets difficulty 16.
	hash := make([]byte, 32)
	hash[0] = 0
	hash[1] = 0
	hash[2] = 0xFF
	if !MeetsDifficulty(hash, 16) {
		t.Error("expected difficulty 16 to be met")
	}
	// difficulty 17 requires first 17 bits to be 0: bytes 0,1 must be 0, byte 2 high bit must be 0.
	if MeetsDifficulty(hash, 17) {
		t.Error("hash[2]=0xFF should not meet difficulty 17")
	}

	hash[2] = 0x00
	if !MeetsDifficulty(hash, 17) {
		t.Error("expected difficulty 17 to be met with hash[2]=0x00")
	}
}

func TestPowCompute(t *testing.T) {
	payload := []byte("test-payload")
	nonce, err := ComputePow(payload, 8) // low difficulty for fast test
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPow(payload, nonce, 8) {
		t.Fatal("VerifyPow failed for computed nonce")
	}
	// Wrong nonce should fail.
	if VerifyPow(payload, nonce+1, 8) {
		// This could theoretically pass but is extremely unlikely.
		t.Log("nonce+1 also works (extremely unlikely but possible)")
	}
}

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	record := &proto.NameRecord{
		Name:      "test.sidenet",
		PublicKey: pub,
		Addrs:     []string{"10.0.0.1"},
		CreatedAt: now,
		UpdatedAt: now,
		TTL:       3600,
	}

	if err := SignRecord(record, priv, 8); err != nil {
		t.Fatalf("SignRecord: %v", err)
	}

	if !VerifyRecord(record) {
		t.Fatal("VerifyRecord: expected valid record")
	}

	// Mutate an address — signature should fail.
	record.Addrs[0] = "10.0.0.2"
	if VerifyRecord(record) {
		t.Fatal("VerifyRecord: expected invalid after mutation")
	}
}

func TestConflictResolution(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	makeRecord := func(addr string, created, updated time.Time, key []byte, pk ed25519.PublicKey, sk ed25519.PrivateKey) *proto.NameRecord {
		r := &proto.NameRecord{
			Name:      "conflict.sidenet",
			PublicKey: pk,
			Addrs:     []string{addr},
			CreatedAt: created,
			UpdatedAt: updated,
			TTL:       3600,
		}
		if sk != nil {
			SignRecord(r, sk, 8)
		}
		_ = key
		return r
	}

	// Same owner, newer UpdatedAt → incoming wins.
	existing := makeRecord("1.1.1.1", base, base, nil, pub, priv)
	incoming := makeRecord("2.2.2.2", base, base.Add(time.Hour), nil, pub, priv)
	winner := Resolve(existing, incoming)
	if winner.Addrs[0] != "2.2.2.2" {
		t.Errorf("same owner, newer update: expected 2.2.2.2, got %s", winner.Addrs[0])
	}

	// Same owner, older UpdatedAt → existing wins.
	older := makeRecord("3.3.3.3", base, base.Add(-time.Hour), nil, pub, priv)
	winner2 := Resolve(existing, older)
	if winner2.Addrs[0] != "1.1.1.1" {
		t.Errorf("same owner, older update: expected 1.1.1.1, got %s", winner2.Addrs[0])
	}

	// Different owners — oldest CreatedAt wins.
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	older2 := makeRecord("4.4.4.4", base.Add(-2*time.Minute), base.Add(-2*time.Minute), nil, pub2, priv2)
	winner3 := Resolve(existing, older2)
	if winner3.Addrs[0] != "4.4.4.4" {
		t.Errorf("different owner, older created: expected 4.4.4.4, got %s", winner3.Addrs[0])
	}

	// Invalid signature should be rejected.
	bad := &proto.NameRecord{
		Name:      "conflict.sidenet",
		PublicKey: pub2,
		Addrs:     []string{"5.5.5.5"},
		CreatedAt: base.Add(-time.Hour),
		UpdatedAt: base.Add(-time.Hour),
		TTL:       3600,
		Signature: make([]byte, 64), // all zeros — invalid
	}
	winner4 := Resolve(existing, bad)
	if winner4.Addrs[0] != "1.1.1.1" {
		t.Errorf("invalid signature: expected existing to win, got %s", winner4.Addrs[0])
	}
}

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"alice", "alice.sidenet"},
		{"alice.sidenet", "alice.sidenet"},
		{"ALICE", "alice.sidenet"},
		{"alice.sidenet.", "alice.sidenet"},
		{"  alice  ", "alice.sidenet"},
	}
	for _, c := range cases {
		got := NormalizeName(c.input)
		if got != c.expected {
			t.Errorf("NormalizeName(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}
