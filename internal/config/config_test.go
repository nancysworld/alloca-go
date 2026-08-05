package config

import (
	"math"
	"strings"
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

// The readiness probe acquires a connection and round-trips a statement, so a bound
// equal to the acquisition cap can expire on the round trip after acquisition succeeded
// — reporting a healthy database as unready. The relationship is validated at startup
// rather than left to a comment, like the rest of the §8.1 chain.
func TestValidateRejectsAReadinessTimeoutThatCannotOutlastAcquisition(t *testing.T) {
	tests := map[string]func(*Config){
		"equal to the acquisition cap": func(c *Config) {
			c.ReadinessTimeout = c.RequestBudget.DBAcquireCap
		},
		"below the acquisition cap": func(c *Config) {
			c.ReadinessTimeout = c.RequestBudget.DBAcquireCap - time.Millisecond
		},
		"beyond the server deadline": func(c *Config) {
			c.ReadinessTimeout = c.RequestBudget.ServerDeadline + time.Millisecond
		},
		"zero": func(c *Config) { c.ReadinessTimeout = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Errorf("accepted ReadinessTimeout %s against db_acquire_cap %s / server_deadline %s",
					cfg.ReadinessTimeout, cfg.RequestBudget.DBAcquireCap, cfg.RequestBudget.ServerDeadline)
			}
		})
	}
}

// A non-positive hold TTL must be refused by Validate, because service.New panics on one.
//
// This covers the Validate-without-Load path specifically: a Config assembled in code
// never passes through Load's duration table, so Validate is the only thing between it and
// that panic. The environment path is covered separately below, and the two are independent
// — this test still fails if the Validate check is removed, even though that one would not.
func TestValidateRejectsANonPositiveReservationTTL(t *testing.T) {
	for name, ttl := range map[string]time.Duration{
		"zero":     0,
		"negative": -time.Second,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			cfg.ReservationTTL = ttl
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("accepted ReservationTTL %s", ttl)
			}
			// The message must name the field, so the operator knows which variable to
			// correct without reading this test.
			if !strings.Contains(err.Error(), "ReservationTTL") {
				t.Errorf("error %q does not name ReservationTTL", err)
			}
		})
	}
}

// The environment path is already closed by Load's duration table, which rejects a
// non-positive value for every override it parses. Locking it in here because
// ReservationTTL's membership of that table is what makes it true, and dropping it from the
// table would otherwise be a silent regression: the panic would move from unreachable to
// reachable by an operator typo.
func TestLoadRejectsANonPositiveReservationTTLOverride(t *testing.T) {
	for _, raw := range []string{"0s", "-5s"} {
		_, err := Load(lookupFrom(map[string]string{envReservationTTL: raw}))
		if err == nil {
			t.Errorf("Load accepted %s=%s", envReservationTTL, raw)
			continue
		}
		if !strings.Contains(err.Error(), envReservationTTL) {
			t.Errorf("error for %s does not name the variable: %v", raw, err)
		}
	}
}

// The default must satisfy the relationship it enforces, or the service cannot start
// without configuration.
func TestDefaultReadinessTimeoutOutlastsAcquisition(t *testing.T) {
	cfg := Default()
	if cfg.ReadinessTimeout <= cfg.RequestBudget.DBAcquireCap {
		t.Errorf("default ReadinessTimeout %s does not exceed db_acquire_cap %s",
			cfg.ReadinessTimeout, cfg.RequestBudget.DBAcquireCap)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config is invalid: %v", err)
	}
}

// Placement configuration is optional, and the default is a named single authority
// rather than an empty one: a figure from a one-authority run should still say which
// database produced it.
func TestPlacementDefaultsToAnUnconfiguredSingleAuthority(t *testing.T) {
	cfg, err := Load(lookupFrom(nil))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Placement.Configured() {
		t.Error("no placement variables were set, so no map should be configured")
	}
	if cfg.Placement.AuthorityID != "" {
		t.Error("config records what the operator supplied; naming the sole authority is the wiring layer's job")
	}
}

func TestPlacementLoadsFromEnvironment(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		envAuthorityID: "authority-a",
		envPlacement:   `{"version":"v1","homes":{"org-a":"authority-a"}}`,
	}))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.Placement.Configured() {
		t.Fatal("a placement document was supplied but Configured reports false")
	}
	if cfg.Placement.AuthorityID != "authority-a" {
		t.Errorf("AuthorityID = %q, want authority-a", cfg.Placement.AuthorityID)
	}
}

// Each of these leaves the unit unable to make one unambiguous routing decision, so
// Load must refuse rather than resolve it silently.
func TestPlacementConfigurationsThatCannotRouteAreRejected(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "map without an authority identity",
			env:  map[string]string{envPlacement: `{"version":"v1","homes":{"org-a":"authority-a"}}`},
			want: envAuthorityID,
		},
		{
			name: "two documents",
			env: map[string]string{
				envAuthorityID:   "authority-a",
				envPlacement:     `{"version":"v1","homes":{"org-a":"authority-a"}}`,
				envPlacementFile: "/etc/alloca/placement.json",
			},
			want: "one placement document",
		},
		{
			name: "empty override",
			env:  map[string]string{envAuthorityID: ""},
			want: "must not be empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(lookupFrom(tc.env))
			if err == nil {
				t.Fatal("Load accepted a placement configuration that cannot route")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
