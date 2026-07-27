package config

import (
	"math"
	"testing"
	"time"
)

// lookupFrom builds a lookup function backed by a map, for deterministic tests.
func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadDefaultsWhenEmpty(t *testing.T) {
	cfg, err := Load(lookupFrom(nil))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg != Default() {
		t.Fatalf("Load with no env = %+v, want defaults %+v", cfg, Default())
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		envListenAddr:    "127.0.0.1:9090",
		envReadTimeout:   "3s",
		envShutdownGrace: "500ms",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:9090", cfg.ListenAddr)
	}
	if cfg.ReadTimeout != 3*time.Second {
		t.Errorf("ReadTimeout = %v, want 3s", cfg.ReadTimeout)
	}
	if cfg.ShutdownGrace != 500*time.Millisecond {
		t.Errorf("ShutdownGrace = %v, want 500ms", cfg.ShutdownGrace)
	}
	// Untouched fields keep their defaults.
	if cfg.WriteTimeout != Default().WriteTimeout {
		t.Errorf("WriteTimeout = %v, want default %v", cfg.WriteTimeout, Default().WriteTimeout)
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{envReadTimeout: "not-a-duration"}))
	if err == nil {
		t.Fatal("Load accepted an invalid duration, want error")
	}
}

func TestLoadRejectsNegativeDuration(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{envIdleTimeout: "-1s"}))
	if err == nil {
		t.Fatal("Load accepted a negative duration, want error")
	}
}

func TestLoadRejectsZeroDuration(t *testing.T) {
	// Zero must be rejected for every timeout field: net/http reads a zero timeout
	// as "no timeout", which would silently disable the bound.
	for _, name := range []string{
		envReadHeaderTimeout,
		envReadTimeout,
		envWriteTimeout,
		envIdleTimeout,
		envShutdownGrace,
	} {
		if _, err := Load(lookupFrom(map[string]string{name: "0s"})); err == nil {
			t.Errorf("Load accepted %s=0s, want error", name)
		}
	}
}

func TestLoadRejectsEmptyListenAddr(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{envListenAddr: ""}))
	if err == nil {
		t.Fatal("Load accepted an empty listen address, want error")
	}
}

func TestDefaultBudgetIsValid(t *testing.T) {
	// The provisional §8 defaults must themselves satisfy the §8.1 invariants;
	// otherwise the service could never start with zero configuration.
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default() failed Validate: %v", err)
	}
}

func TestValidateRejectsBadNesting(t *testing.T) {
	// Each case starts from a valid Default and violates exactly one clause of the
	// §8.1 per-request nesting: lock < statement <= txn < server < client.
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"lock == statement", func(c *Config) { c.RequestBudget.LockTimeout = c.RequestBudget.StatementTimeout }},
		{"lock > statement", func(c *Config) { c.RequestBudget.LockTimeout = c.RequestBudget.StatementTimeout + time.Millisecond }},
		{"statement > txn", func(c *Config) { c.RequestBudget.StatementTimeout = c.RequestBudget.TxnBudget + time.Millisecond }},
		{"txn == server", func(c *Config) { c.RequestBudget.TxnBudget = c.RequestBudget.ServerDeadline }},
		{"server == client", func(c *Config) { c.RequestBudget.ServerDeadline = c.RequestBudget.ClientDeadline }},
		{"server > client", func(c *Config) { c.RequestBudget.ServerDeadline = c.RequestBudget.ClientDeadline + time.Millisecond }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate accepted invalid nesting (%s), want error", tc.name)
			}
		})
	}
}

func TestValidateAllowsStatementEqualToTxn(t *testing.T) {
	// statement_timeout <= txn_budget is a non-strict bound: equality is valid.
	cfg := Default()
	cfg.RequestBudget.StatementTimeout = cfg.RequestBudget.TxnBudget
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected statement_timeout == txn_budget, want accepted: %v", err)
	}
}

func TestValidateEnforcesWritePhase(t *testing.T) {
	// WriteTimeout must exceed ServerDeadline + WriteResponseMargin so the Go context
	// deadline fires before a connection-write teardown.
	cfg := Default()
	cfg.WriteTimeout = cfg.RequestBudget.ServerDeadline + cfg.WriteResponseMargin
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted WriteTimeout == ServerDeadline + margin, want error")
	}

	cfg = Default()
	cfg.WriteTimeout = cfg.RequestBudget.ServerDeadline + cfg.WriteResponseMargin + time.Millisecond
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected WriteTimeout just above the margin, want accepted: %v", err)
	}
}

func TestValidateWritePhaseDoesNotOverflow(t *testing.T) {
	// Regression: time.Duration is an int64. A near-maximum WriteResponseMargin makes
	// ServerDeadline + WriteResponseMargin overflow to a negative duration. The check
	// must not compute that sum — with the default 10s WriteTimeout it is NOT actually
	// greater than server deadline + this margin, so Validate must reject it. An
	// additive check would overflow negative and wrongly accept.
	cfg := Default()
	cfg.WriteResponseMargin = time.Duration(math.MaxInt64) - time.Second
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a config whose ServerDeadline+WriteResponseMargin overflows int64, want error")
	}

	// Conversely, a near-maximum WriteTimeout with an ordinary margin is legitimately
	// valid and must not be rejected by any overflow-avoidance arithmetic.
	cfg = Default()
	cfg.WriteTimeout = time.Duration(math.MaxInt64)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected a near-max WriteTimeout with an ordinary margin: %v", err)
	}
}

func TestValidateIgnoresIndependentTransportBounds(t *testing.T) {
	// §8.1 clause 2: ReadHeaderTimeout, ReadTimeout, and IdleTimeout are independent
	// transport bounds, NOT mechanically nested under the business deadline. A config
	// where they are far smaller than the server deadline must still validate.
	cfg := Default()
	cfg.ReadHeaderTimeout = time.Millisecond
	cfg.ReadTimeout = time.Millisecond
	cfg.IdleTimeout = time.Millisecond
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected small independent transport bounds, want accepted: %v", err)
	}
}

func TestValidateRejectsNonPositiveBudget(t *testing.T) {
	cfg := Default()
	cfg.RequestBudget.LockTimeout = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a zero LockTimeout, want error")
	}
}

// TestBudgetValidateIsUsableStandalone pins that the §8.1 clause 1 checks are reachable
// from a RequestBudget alone, not only through Config.Validate. Consumers are handed a
// bare budget — the PostgreSQL pool renders LockTimeout and StatementTimeout into
// session settings, where a zero disables the bound entirely — and they cannot check the
// precondition they depend on unless it lives at this level.
func TestBudgetValidateIsUsableStandalone(t *testing.T) {
	if err := Default().RequestBudget.Validate(); err != nil {
		t.Fatalf("Validate rejected the default budget, want accepted: %v", err)
	}

	// The zero value must be rejected: it is what a caller who never populated the budget
	// holds, and every bound in it is the "no bound" value.
	if err := (RequestBudget{}).Validate(); err == nil {
		t.Fatal("Validate accepted the zero budget, want error")
	}

	// Ordering is enforced here too, not just positivity — otherwise Config.Validate
	// would still have to duplicate the nesting checks for its own callers.
	inverted := Default().RequestBudget
	inverted.LockTimeout = inverted.StatementTimeout + time.Second
	if err := inverted.Validate(); err == nil {
		t.Fatal("Validate accepted lock_timeout > statement_timeout, want error")
	}
}

func TestLoadRejectsBudgetOverrideThatBreaksOrdering(t *testing.T) {
	// An env override that inverts the chain must fail fast at Load, not start.
	_, err := Load(lookupFrom(map[string]string{
		envLockTimeout: "4s", // >= default statement_timeout (3s)
	}))
	if err == nil {
		t.Fatal("Load accepted lock_timeout > statement_timeout, want error")
	}
}

func TestLoadParsesBudgetOverrides(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		envServerDeadline:   "4s",
		envTxnBudget:        "3s",
		envStatementTimeout: "2500ms",
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.RequestBudget.ServerDeadline != 4*time.Second {
		t.Errorf("ServerDeadline = %v, want 4s", cfg.RequestBudget.ServerDeadline)
	}
	if cfg.RequestBudget.TxnBudget != 3*time.Second {
		t.Errorf("TxnBudget = %v, want 3s", cfg.RequestBudget.TxnBudget)
	}
	if cfg.RequestBudget.StatementTimeout != 2500*time.Millisecond {
		t.Errorf("StatementTimeout = %v, want 2500ms", cfg.RequestBudget.StatementTimeout)
	}
}
