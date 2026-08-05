package loadgen

import (
	"context"
	"sync"

	"github.com/nancysworld/alloca-go/internal/domain"
)

// Ambiguous is one mutation whose outcome the client does not know.
//
// `unknown_replayable` is not a terminal result. It says the commit may or may not have
// landed and the acknowledgement did not arrive, and the contract's only safe resolution
// is to replay the *same* idempotency key: the record either exists, in which case the
// replay returns the original outcome, or it does not, in which case the replay performs
// the mutation once (transaction-semantics §5.4).
//
// A run that ends with ambiguous mutations outstanding therefore cannot be reconciled —
// its client totals record an outcome that is, by definition, not yet a fact about
// persisted state. This is the register that makes the resolution pass possible.
type Ambiguous struct {
	// Operation, User and Key identify the logical mutation. Key is the whole point:
	// resolving with a *new* key would be a new request and could double-book.
	Operation string
	User      User
	Key       string
	// path is the exact URL the original request used, kept so the replay reissues that
	// request rather than one reconstructed from remembered parts. Reserve names its slot
	// in the path and confirm/cancel name their reservation, so rebuilding it would mean
	// storing a different shape per operation and getting each right.
	path string
}

// ambiguityRegister collects ambiguous mutations during a run.
//
// It is deliberately not part of Response or Total. Those describe what the run
// *observed*, and an ambiguous mutation is precisely the thing observation could not
// settle; folding it in would report an unresolved question as a measured answer.
type ambiguityRegister struct {
	mu      sync.Mutex
	entries []Ambiguous
}

func (r *ambiguityRegister) record(entry Ambiguous) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
}

func (r *ambiguityRegister) snapshot() []Ambiguous {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := make([]Ambiguous, len(r.entries))
	copy(snapshot, r.entries)
	return snapshot
}

// Ambiguous returns the mutations this client could not settle, in the order they
// occurred. Empty is the normal and expected result for a healthy run.
func (c *Client) Ambiguous() []Ambiguous {
	return c.ambiguous.snapshot()
}

// Resolution is what replaying one ambiguous mutation established.
type Resolution struct {
	Ambiguous Ambiguous
	// Response is the replay's answer. Replay=true means the original mutation had
	// committed and this returned its recorded outcome; Replay=false means it had not,
	// and this performed it. Either way exactly one logical mutation exists afterwards,
	// which is the property INV-21 asserts.
	Response Response
	// StillAmbiguous is set when the replay was itself ambiguous — the authority is
	// still unavailable, or became unavailable again. Such an entry is *not* resolved
	// and the caller must not treat the run as reconcilable.
	StillAmbiguous bool
}

// ResolveAmbiguous replays every ambiguous mutation under its own idempotency key and
// reports what each turned out to be.
//
// Call it after the affected authority is available again and before the correctness
// verdict (ag-sept-plan-new.md §6.5). Until it has run, an authority-failure run has
// mutations whose persisted state and whose client record genuinely disagree, and no
// reconciliation over them means anything.
//
// **This is not a retry control.** It does not run during load, it does not shape
// arrival, and it never amplifies: each entry is replayed exactly once, after the run,
// under the key the original used. The retry-on-timeout control that shapes load during
// a run is separate work and remains unassigned (ag-sept-plan-new.md §14, group A).
// Conflating the two is how that group creeps into a PR that cannot fund it.
func (c *Client) ResolveAmbiguous(ctx context.Context) []Resolution {
	entries := c.Ambiguous()
	resolutions := make([]Resolution, 0, len(entries))
	for _, entry := range entries {
		replayed := c.do(ctx, entry.Operation, entry.path, entry.User, entry.Key)
		resolutions = append(resolutions, Resolution{
			Ambiguous:      entry,
			Response:       replayed,
			StillAmbiguous: replayed.Outcome == domain.OutcomeUnknownReplayable,
		})
	}
	return resolutions
}

// Unresolved counts resolutions that are still ambiguous. A run with any of these is not
// reconcilable and must not be quoted.
func Unresolved(resolutions []Resolution) int {
	var stillAmbiguous int
	for _, resolution := range resolutions {
		if resolution.StillAmbiguous {
			stillAmbiguous++
		}
	}
	return stillAmbiguous
}
