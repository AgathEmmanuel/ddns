package registry

import (
	"bytes"
	"log/slog"
	"time"

	"github.com/agath/ddns/pkg/proto"
)

const conflictTieWindow = 60 * time.Second

// Resolve determines which record wins a conflict between existing and incoming.
// It verifies the incoming record's signature before accepting it.
// Returns the winner.
func Resolve(existing, incoming *proto.NameRecord) *proto.NameRecord {
	if !VerifyRecord(incoming) {
		slog.Debug("registry: rejected incoming record with invalid signature", "name", incoming.Name)
		return existing
	}

	// Same owner — accept if UpdatedAt is newer.
	if bytes.Equal(existing.PublicKey, incoming.PublicKey) {
		if incoming.UpdatedAt.After(existing.UpdatedAt) {
			return incoming
		}
		return existing
	}

	// Different owners — oldest CreatedAt wins (partition reconciliation).
	diff := existing.CreatedAt.Sub(incoming.CreatedAt)
	if diff < 0 {
		diff = -diff
	}
	if diff <= conflictTieWindow {
		// Timestamps are within the tie window — log a warning, keep existing.
		slog.Warn("registry: name conflict within tie window",
			"name", existing.Name,
			"existing_created", existing.CreatedAt,
			"incoming_created", incoming.CreatedAt,
		)
		return existing
	}

	if incoming.CreatedAt.Before(existing.CreatedAt) {
		return incoming
	}
	return existing
}
