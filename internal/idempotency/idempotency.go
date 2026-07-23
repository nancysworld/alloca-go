// Package idempotency owns the idempotency scope construction, request hashing, and
// replay-vs-conflict resolution policy (docs/design/transaction-semantics.md §5). It
// is pure: it computes values and decisions over domain types and performs no I/O.
// The transactional orchestration that reads and writes records lives in
// internal/service; the persistence contract lives in internal/domain.
package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Scope builds the idempotency scope key (transaction-semantics §5.1). The target is
// deliberately absent — it lives only in the request hash.
func Scope(org domain.OrganisationID, user domain.UserID, op domain.Operation, key string) domain.ScopeKey {
	return domain.ScopeKey{OrganisationID: org, UserID: user, Operation: op, Key: key}
}

// canonical is the stable, ordered representation hashed to form the request hash.
// Struct field order is fixed (Go marshals struct fields in declaration order and
// never reorders), so json.Marshal yields a canonical byte sequence with stable keys
// and separators. It carries the semantically significant request fields only —
// server-generated values such as now and expires_at are excluded so ordinary
// retries of the same logical request hash identically (transaction-semantics §5.1).
type canonical struct {
	ContractVersion string `json:"contract_version"`
	Operation       string `json:"operation"`
	OrganisationID  string `json:"organisation_id"`
	UserID          string `json:"user_id"`
	TargetID        string `json:"target_id"`
	Body            []byte `json:"body"`
}

// RequestHash computes the request hash over the canonical representation of the
// request's semantically significant fields. AG-M1 mutation bodies are empty, but
// body stays in the contract so fields can be added later without changing the model.
func RequestHash(contractVersion string, op domain.Operation, org domain.OrganisationID, user domain.UserID, targetID string, body []byte) string {
	if body == nil {
		body = []byte{}
	}
	c := canonical{
		ContractVersion: contractVersion,
		Operation:       string(op),
		OrganisationID:  string(org),
		UserID:          string(user),
		TargetID:        targetID,
		Body:            body,
	}
	// json.Marshal of a struct with only string/[]byte fields cannot fail.
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Resolution is the decision for a request that matches an existing idempotency
// record's scoped key.
type Resolution int

const (
	// Replay: same scope and same request hash — return the recorded terminal outcome
	// with replay=true; run no mutation.
	Replay Resolution = iota
	// Conflict: same scope but a different request hash — the key was reused for a
	// different request; refuse with idempotency_conflict.
	Conflict
)

// Resolve compares an incoming request hash against a found record (§5.3 steps 2–3).
func Resolve(rec domain.IdempotencyRecord, incomingHash string) Resolution {
	if rec.RequestHash == incomingHash {
		return Replay
	}
	return Conflict
}
