package domain

import (
	"context"
	"errors"
)

// Infrastructure fault sentinels. Adapters wrap the failures they detect in these so
// the transport edge can classify a fault without importing the adapter — preserving
// the dependency rule (project-structure §4) while keeping the §4 taxonomy total.
//
// These are faults, not domain answers: they are carried as a non-nil error from the
// Service, never as a Result. A refusal is a valid business "no"; a fault is the
// service failing to answer (transaction-semantics §4, the fault line).
var (
	// ErrDBTimeout reports that a database-layer bound fired: a lock wait exceeding
	// lock_timeout, a statement exceeding statement_timeout, or the transaction
	// exceeding its txn budget. All three mean the database layer was the innermost
	// responsible bound, so all three classify as timeout_db (transaction-semantics
	// §6).
	ErrDBTimeout = errors.New("domain: database timeout")
	// ErrCommitUnknown reports that a commit's acknowledgement was lost, so whether
	// the transaction committed is genuinely unknown. It maps to unknown_replayable
	// and is the one fault the client resolves by replaying the *same* idempotency
	// key: either the record exists (it committed → replay) or it does not (it never
	// committed → perform it fresh). Retrying with a new key would break the
	// one-key-one-mutation gate (transaction-semantics §5.4).
	ErrCommitUnknown = errors.New("domain: commit outcome unknown")
)

// ClassifyFault maps a fault returned by the Service to its measurement-contract §4
// terminal outcome. It is the adapter-independent half of the error→outcome mapping;
// the transport edge (PR4) applies it so every completed request carries exactly one
// terminal outcome and experiment totals reconcile.
//
// Ordering matters: the sentinels are checked before the context errors because an
// ambiguous commit or a database bound may itself wrap a deadline, and the more
// specific cause is the one worth reporting.
//
// A nil error is not a fault. Passing one is a caller bug, and it is reported as
// internal_failure rather than silently returning a zero Outcome that would escape
// the taxonomy.
func ClassifyFault(err error) Outcome {
	switch {
	case err == nil:
		return OutcomeInternalFailure
	case errors.Is(err, ErrCommitUnknown):
		return OutcomeUnknownReplayable
	case errors.Is(err, ErrDBTimeout):
		return OutcomeTimeoutDB
	case errors.Is(err, context.Canceled):
		// The request context is cancelled when the caller goes away, so this is the
		// client-disconnect leg of §4. A deliberate server-side cancel would also land
		// here; AG-M1 has no such path.
		return OutcomeTimeoutClient
	case errors.Is(err, context.DeadlineExceeded):
		// The outer per-request deadline. A database-layer bound would have fired
		// first and been wrapped as ErrDBTimeout above, so reaching here means the
		// server deadline was the binding one.
		return OutcomeTimeoutServer
	default:
		return OutcomeInternalFailure
	}
}
