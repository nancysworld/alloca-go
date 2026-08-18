package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// The pool collector is the only view of connection acquisition this deployment has, and a
// missing series here is invisible: the scrape still succeeds, every panel still renders, and the
// populated-series gate still passes for the metrics that *are* present. AG-Sept PR4a §3.13.1 is
// the worked example — a degraded cell could not be diagnosed because the six exported metrics
// could not distinguish "no connection was free" from "the acquire call itself was slow", while
// pgxpool had been exposing the deciding counters all along.
//
// So this asserts the exported set by name. It needs no database: pgxpool builds its pool without
// connecting, and Stat() reports zeroes until something does.
func poolCollectorForTest(t *testing.T) *poolCollector {
	t.Helper()

	cfg, err := pgxpool.ParseConfig("postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return newPoolCollector(pool)
}

// Every pgxpool.Stat method this project has a use for, as exported metric names.
func wantedPoolMetrics() []string {
	return []string{
		"alloca_db_pool_acquired_connections",
		"alloca_db_pool_acquires_total",
		"alloca_db_pool_acquire_wait_seconds_total",
		"alloca_db_pool_canceled_acquires_total",
		"alloca_db_pool_constructing_connections",
		"alloca_db_pool_empty_acquire_wait_seconds_total",
		"alloca_db_pool_empty_acquires_total",
		"alloca_db_pool_idle_connections",
		"alloca_db_pool_max_connections",
		"alloca_db_pool_max_idle_destroys_total",
		"alloca_db_pool_max_lifetime_destroys_total",
		"alloca_db_pool_new_connections_total",
		"alloca_db_pool_total_connections",
	}
}

func TestPoolCollectorDescribesEveryMetricItCollects(t *testing.T) {
	c := poolCollectorForTest(t)

	described := make(chan *prometheus.Desc, 64)
	c.Describe(described)
	close(described)

	var got []string
	for d := range described {
		got = append(got, nameFromDesc(d.String()))
	}
	sort.Strings(got)

	assertSameNames(t, "Describe", wantedPoolMetrics(), got)
}

// Describe and Collect are separate methods over the same field set, so a metric can be declared
// and never emitted — which reads as a permanently absent series rather than as an error.
func TestPoolCollectorCollectsEveryMetricItDescribes(t *testing.T) {
	c := poolCollectorForTest(t)

	collected := make(chan prometheus.Metric, 64)
	c.Collect(collected)
	close(collected)

	var got []string
	for m := range collected {
		got = append(got, nameFromDesc(m.Desc().String()))
	}
	sort.Strings(got)

	assertSameNames(t, "Collect", wantedPoolMetrics(), got)
}

// **Neither timing metric measures blocked waiting, and both are named as though they might.**
//
// pgxpool's AcquireDuration times the whole Acquire() call. EmptyAcquireWaitTime is narrower but
// still not pure contention: puddle accumulates it on acquires that found no idle resource, which
// covers waiting for a release *and* constructing a new connection — and on that path the clock
// is read after the constructor returns, so full construction time lands in it
// (puddle v2.2.2 pool.go). An earlier version of this test asserted the opposite, that
// empty_acquire_wait was "the metric that IS wait time"; it is not, and a test that says so
// re-teaches the misreading it was written to prevent.
//
// Neither name can be fixed without breaking queries against already-retained cells, so the
// disclaimers travel in the help text and this pins them there.
func TestNeitherTimingMetricClaimsToBePureWaiting(t *testing.T) {
	c := poolCollectorForTest(t)

	for _, tc := range []struct {
		metric string
		help   string
		musts  []string
	}{
		{
			metric: "acquire_wait_seconds_total",
			help:   c.acquireWait.String(),
			// It times every successful acquire, including ones that never waited at all.
			musts: []string{"NOT blocked-waiting time", "including connection construction"},
		},
		{
			metric: "empty_acquire_wait_seconds_total",
			help:   c.emptyAcquireWait.String(),
			// It conflates waiting for a release with constructing a connection, and only
			// new_connections_total tells them apart.
			musts: []string{"constructing a new one", "new_connections_total"},
		},
	} {
		for _, must := range tc.musts {
			if !strings.Contains(tc.help, must) {
				t.Errorf("%s help must say %q, got: %s", tc.metric, must, tc.help)
			}
		}
	}
}

// Construction is where PostgreSQL enters the acquire path — pgxpool's constructor calls
// pgx.ConnectConfig — which is why this counter is what keeps "the symptom is in the acquire
// path" from being read as "PostgreSQL is excluded".
func TestNewConnectionsHelpNamesPostgresAsTheCost(t *testing.T) {
	c := poolCollectorForTest(t)
	help := c.newConns.String()

	if !strings.Contains(help, "PostgreSQL") {
		t.Errorf("new_connections_total help must say construction reaches PostgreSQL, got: %s", help)
	}
}

func assertSameNames(t *testing.T, method string, want, got []string) {
	t.Helper()

	missing := difference(want, got)
	extra := difference(got, want)

	for _, m := range missing {
		t.Errorf("%s omits %s", method, m)
	}
	for _, m := range extra {
		t.Errorf("%s emits unexpected %s (add it to wantedPoolMetrics if intended)", method, m)
	}
}

func difference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if !inB[s] {
			out = append(out, s)
		}
	}
	return out
}

// prometheus.Desc has no accessor for its fully-qualified name; String() renders it as
// `Desc{fqName: "x", help: ...}`, so the name is read back out of that.
func nameFromDesc(s string) string {
	_, rest, found := strings.Cut(s, `fqName: "`)
	if !found {
		return s
	}
	name, _, found := strings.Cut(rest, `"`)
	if !found {
		return s
	}
	return name
}
