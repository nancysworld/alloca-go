package domain

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestClassifyFault pins the error→outcome half of the §4 taxonomy. Each case is a
// fault an adapter can actually raise; the mapping is what keeps every completed
// request inside exactly one terminal outcome so experiment totals reconcile.
func TestClassifyFault(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Outcome
	}{
		{"lock or statement timeout", ErrDBTimeout, OutcomeTimeoutDB},
		{"wrapped db timeout", fmt.Errorf("postgres: lock wait: %w", ErrDBTimeout), OutcomeTimeoutDB},
		{"lost commit acknowledgement", ErrCommitUnknown, OutcomeUnknownReplayable},
		{"wrapped unknown commit", fmt.Errorf("postgres: %w", ErrCommitUnknown), OutcomeUnknownReplayable},
		{"client disconnected", context.Canceled, OutcomeTimeoutClient},
		{"server deadline", context.DeadlineExceeded, OutcomeTimeoutServer},
		{"invariant violation", errors.New("service: commit invariant violated"), OutcomeInternalFailure},
		{"time not established", ErrTimeNotEstablished, OutcomeInternalFailure},

		// A nil error is not a fault. Classifying it as internal_failure rather than
		// returning a zero Outcome keeps a caller bug inside the taxonomy instead of
		// producing a request with no terminal outcome at all.
		{"nil is a caller bug", nil, OutcomeInternalFailure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFault(tc.err); got != tc.want {
				t.Errorf("ClassifyFault(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestClassifyFaultPrefersSpecificCause covers the ordering rule. An ambiguous commit
// or a database bound will often wrap a deadline, and reporting the outer deadline
// would lose the distinction that matters: unknown_replayable tells a client to replay
// the same key, while timeout_server does not.
func TestClassifyFaultPrefersSpecificCause(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Outcome
	}{
		{
			name: "unknown commit wrapping a deadline",
			err:  fmt.Errorf("commit lost: %w: %w", ErrCommitUnknown, context.DeadlineExceeded),
			want: OutcomeUnknownReplayable,
		},
		{
			name: "db timeout wrapping a cancellation",
			err:  fmt.Errorf("lock wait: %w: %w", ErrDBTimeout, context.Canceled),
			want: OutcomeTimeoutDB,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFault(tc.err); got != tc.want {
				t.Errorf("ClassifyFault(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestClassifyFaultNeverRefuses guards the fault line: a fault must never be reported
// as a domain answer. business_refusal and invalid_request are outcomes the domain
// produces from valid requests, and no infrastructure error may be laundered into one.
func TestClassifyFaultNeverRefuses(t *testing.T) {
	faults := []error{
		ErrDBTimeout, ErrCommitUnknown, ErrTimeNotEstablished,
		context.Canceled, context.DeadlineExceeded,
		ErrNotFound, ErrConflict,
		errors.New("anything unexpected"),
	}
	for _, err := range faults {
		got := ClassifyFault(err)
		if got == OutcomeBusinessRefusal || got == OutcomeInvalidRequest || got == OutcomeAdmittedSuccess {
			t.Errorf("ClassifyFault(%v) = %q: a fault must never classify as a domain answer", err, got)
		}
	}
}
