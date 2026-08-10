package reconcile

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// requestsTotal is the counter the server-side half of measurement-contract §12 is read from.
// It must stay equal to metrics.Namespace + "_requests_total"; TestParsesWhatTheRecorderEmits
// pins that by parsing the recorder's own exposition rather than a fixture, so a rename in the
// metrics package fails here instead of silently producing an empty scrape that reconciles
// with nothing.
const requestsTotal = "alloca_requests_total"

// ServerTotals is the service's own count of completed requests, read from a metrics
// scrape. It is the third of the three independent counts measurement-contract §12 requires
// to agree.
//
// A nil value means no scrape was supplied. That is not the same as a scrape reporting
// zero, and the two must not collapse into one another: a missing scrape leaves the gate
// unenforced, so it is a failure rather than a pass.
type ServerTotals []loadgen.Total

// ParseServerTotals reads the alloca_requests_total cells out of a Prometheus text
// exposition — the body of a /metrics scrape, usually saved by curl.
//
// It deliberately parses only the one counter. The scrape also carries Go runtime, process
// and pool collectors, and a parser that tried to be general would have to take a position
// on histograms and summaries it has no use for.
func ParseServerTotals(r io.Reader) (ServerTotals, error) {
	var out ServerTotals
	sc := bufio.NewScanner(r)
	// Exposition lines are short, but the default 64 KiB token limit is a silent
	// truncation if one ever is not, and a truncated scrape reconciles against nothing.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		name, rest, ok := strings.Cut(text, "{")
		if !ok || name != requestsTotal {
			continue
		}
		labelText, valueText, ok := strings.Cut(rest, "}")
		if !ok {
			return nil, fmt.Errorf("line %d: unterminated label set: %q", line, text)
		}
		labels, err := parseLabels(labelText)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		count, err := parseCount(strings.TrimSpace(valueText))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, loadgen.Total{
			Operation: labels["operation"],
			Outcome:   domain.Outcome(labels["outcome"]),
			Reason:    domain.Reason(labels["reason"]),
			Replay:    labels["replay"] == "true",
			Count:     count,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading scrape: %w", err)
	}
	// A scrape with no cells is returned as an empty non-nil slice: the service was
	// reachable and had served nothing, which is a real answer and a different one from
	// "no scrape was supplied".
	if out == nil {
		out = ServerTotals{}
	}
	return out, nil
}

func parseLabels(s string) (map[string]string, error) {
	labels := map[string]string{}
	for pair := range strings.SplitSeq(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("label %q is not key=value", pair)
		}
		unquoted, err := strconv.Unquote(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("label %q has an unparseable value: %w", k, err)
		}
		labels[strings.TrimSpace(k)] = unquoted
	}
	return labels, nil
}

// parseCount reads a counter sample, which the exposition format writes as a float even
// when it is a whole number. A non-integral value is an error rather than a truncation:
// this counter counts requests, so a fraction means the scrape is not what it claims.
func parseCount(s string) (int, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("sample %q is not a number: %w", s, err)
	}
	n := int(f)
	if float64(n) != f {
		return 0, fmt.Errorf("sample %q is not a whole number of requests", s)
	}
	return n, nil
}

// Scrapes are the server-side counters bracketing the measured phase.
//
// Baseline is optional and is what makes warm-up possible. `ag-sept-pr2.md` §5.4 runs
// warm-up traffic, resets the fixture, and leaves the service *running* — because restarting
// it would discard exactly what warm-up establishes. Prometheus counters are cumulative and
// only a process restart zeroes them, so After alone carries the warm-up requests too, while
// the client report describes the measured phase alone. Comparing those two would fail every
// warmed cell and report a correct service as unreconciled.
//
// A nil Baseline means the run began from a freshly started process, which is PR1's shape and
// stays valid.
type Scrapes struct {
	Baseline ServerTotals
	After    ServerTotals
}

// measured returns the counts attributable to the measured phase, and says why it cannot when
// the two scrapes do not describe one continuous process.
func (s Scrapes) measured() (ServerTotals, error) {
	if s.Baseline == nil {
		return s.After, nil
	}

	before := map[cellKey]int{}
	for _, t := range s.Baseline {
		before[keyOf(t)] += t.Count
	}

	out := make(ServerTotals, 0, len(s.After))
	for _, t := range s.After {
		delta := t.Count - before[keyOf(t)]
		if delta < 0 {
			// A counter went backwards, which a monotonic counter cannot do within one
			// process. The service restarted between the two scrapes — so the warm state the
			// baseline was taken to preserve is gone, and the binary that served the measured
			// phase may not even be the one the manifest names (DEBT-3).
			return nil, fmt.Errorf("cell %s counted %d after the run but %d before it: the "+
				"counter went backwards, so the service restarted mid-cell and the measured "+
				"phase did not run against the process the baseline describes",
				describe(keyOf(t)), t.Count, before[keyOf(t)])
		}
		if delta > 0 {
			nt := t
			nt.Count = delta
			out = append(out, nt)
		}
	}
	return out, nil
}

// Sum totals every cell, which is the count comparable to the client's completed requests.
func (t ServerTotals) Sum() int {
	n := 0
	for _, c := range t {
		n += c.Count
	}
	return n
}

// serverTotalsCheck is the server-side third of measurement-contract §12: the service's own
// count against the client's. It is the comparison the operator guide previously left to the
// eye, and the one that catches dropped, duplicated or misclassified observations — a client
// and a database can agree perfectly while the service's own telemetry says something else,
// and a capacity number read off that telemetry would then be wrong in a way nothing else
// detects.
//
// Counters are cumulative and nothing resets them short of a process restart. With no
// baseline the scrape is assumed to start from zero, which a restart before the run gives, and
// a scrape taken across two runs reports both and fails here — correctly, since the client
// report describes only one of them. With a baseline the comparison is the delta across the
// measured phase, which is what lets a cell keep a warmed process (see Scrapes).
//
// Ambiguous outcomes are not special-cased. If the client recorded a timeout while the
// service recorded a completion, the cells disagree and this check fails; that is deliberate,
// because such a run needs a person to look at it before any number is quoted from it.
func serverTotalsCheck(s loadgen.Summary, scrapes Scrapes) Check {
	server, err := scrapes.measured()
	if err != nil {
		return Check{Name: "server totals vs client totals", Detail: err.Error()}
	}
	return serverTotalsCheckFromMeasured(s, server)
}

// serverTotalsCheckFromMeasured compares already-differenced server counters with the
// client's totals.
//
// It is separate from serverTotalsCheck because a multi-authority run must difference each
// unit's own before/after pair *before* summing them (measurement-contract §12). Differencing
// the sums would let one unit restarting mid-run — its counters resetting to zero — be masked
// by another unit's increase, and the restart is exactly what the differencing exists to
// catch.
func serverTotalsCheckFromMeasured(s loadgen.Summary, server ServerTotals) Check {
	c := Check{Name: "server totals vs client totals"}

	if server == nil {
		c.Detail = "no metrics scrape was supplied, so the server's own count did not " +
			"participate in this verdict; measurement-contract §12 requires client, " +
			"server and persisted " +
			"state to reconcile, and two of three is not the gate"
		return c
	}

	// Warm-up discards are the one legitimate asymmetry: the client drops those responses
	// from its totals, but the service completed the requests and counted them.
	clientTotal := s.Completed + s.WarmUpDiscarded
	if got := server.Sum(); got != clientTotal {
		c.Detail = fmt.Sprintf("server counted %d completed requests, client reports %d "+
			"(%d completed + %d discarded by warm-up): the two disagree about how much "+
			"work happened. A scrape taken without restarting the service carries earlier "+
			"runs as well, which looks exactly like this",
			got, clientTotal, s.Completed, s.WarmUpDiscarded)
		return c
	}

	if s.WarmUpDiscarded > 0 {
		c.OK = true
		c.Detail = fmt.Sprintf("%d requests, totals agree; per-cell comparison skipped "+
			"because %d warm-up discards are not retained per outcome",
			clientTotal, s.WarmUpDiscarded)
		return c
	}

	if diff := firstCellDisagreement(s.Totals, server); diff != "" {
		c.Detail = diff
		return c
	}

	c.OK = true
	c.Detail = fmt.Sprintf("%d requests, every operation/outcome/reason/replay cell agrees "+
		"with the client", clientTotal)
	return c
}

// cellKey identifies one reported cell. It is the counter's full label set, so a
// misclassification shows up as two disagreeing cells rather than as a matching sum.
type cellKey struct {
	operation string
	outcome   domain.Outcome
	reason    domain.Reason
	replay    bool
}

func keyOf(t loadgen.Total) cellKey {
	return cellKey{t.Operation, t.Outcome, t.Reason, t.Replay}
}

// firstCellDisagreement returns a description of one disagreeing cell, or "" when every
// cell matches. One is enough: the operator needs to know the totals are not the same
// shape, and the full scrape is on disk for the rest.
//
// Only the client's cells are walked. A server cell the client never reported cannot
// survive to here — it would make the sums differ, and the sum comparison runs first — so
// scanning for one would be a branch no input can reach.
func firstCellDisagreement(client []loadgen.Total, server ServerTotals) string {
	byKey := map[cellKey]int{}
	for _, t := range server {
		byKey[keyOf(t)] += t.Count
	}
	for _, t := range client {
		if got := byKey[keyOf(t)]; got != t.Count {
			return fmt.Sprintf("cell %s reported %d by the client and %d by the server",
				describe(keyOf(t)), t.Count, got)
		}
	}
	return ""
}

func describe(k cellKey) string {
	s := fmt.Sprintf("%s/%s", k.operation, k.outcome)
	if k.reason != "" {
		s += "/" + string(k.reason)
	}
	if k.replay {
		s += " (replay)"
	}
	return s
}
