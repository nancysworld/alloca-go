// Package config loads Alloca-Go service configuration from the environment.
//
// Every field has a safe default so the service runs with no configuration. The
// timeout defaults are deliberately aligned with the provisional timeout budget in
// docs/design/measurement-contract.md; they are hypotheses, not tuned production
// values, and AG-M2 sweeps them locally while AG-M3 validates the full deadline
// chain end to end.
//
// Two layers of timeout live here:
//
//   - Connection-level timeouts (ReadHeaderTimeout, ReadTimeout, WriteTimeout,
//     IdleTimeout): coarse transport / resource-protection bounds, each governing a
//     different phase of an HTTP connection.
//   - The per-request RequestBudget: the nested chain of Go-context and database
//     deadlines that is the authoritative business-operation deadline for a mutation
//     (measurement-contract §8). AG-M1 introduces it here.
//
// Load validates both: it rejects a zero/negative connection timeout (net/http
// treats zero as "no timeout"), and it enforces the §8.1 startup invariants over the
// RequestBudget — the per-request nesting and the write-phase relationship — failing
// fast before the service accepts traffic. Only WriteTimeout is nested above the
// request deadline (so the Go context fires before a connection-write teardown);
// ReadHeaderTimeout, ReadTimeout, and IdleTimeout are sized by their own concern and
// are deliberately not compared with it — see the phase table in
// measurement-contract §8.1.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config is the resolved service configuration.
type Config struct {
	// ListenAddr is the TCP address the HTTP server binds to.
	ListenAddr string

	// ReadHeaderTimeout bounds the time spent reading request headers. It is the
	// primary defence against Slowloris-style stalls and should always be set.
	ReadHeaderTimeout time.Duration
	// ReadTimeout bounds the time to read the entire request, headers and body.
	ReadTimeout time.Duration
	// WriteTimeout bounds the time to write the response. It is the only
	// connection-level timeout nested above the per-request budget: it must exceed
	// RequestBudget.ServerDeadline by at least WriteResponseMargin so the Go context
	// deadline fires first and yields a classified outcome rather than a torn
	// connection (measurement-contract §8.1).
	WriteTimeout time.Duration
	// IdleTimeout bounds how long a kept-alive connection may sit idle.
	IdleTimeout time.Duration

	// WriteResponseMargin is the minimum headroom required between the per-request
	// server deadline and WriteTimeout: time reserved for encoding and writing the
	// response after the handler's context deadline could fire. It makes the
	// "explicit response-writing margin" of §8.1 a named, validated value rather than
	// an implicit assumption.
	WriteResponseMargin time.Duration

	// ShutdownGrace bounds graceful shutdown: in-flight requests have until this
	// deadline to complete before the server is forced closed.
	ShutdownGrace time.Duration

	// RequestBudget is the per-request deadline chain (measurement-contract §8).
	RequestBudget RequestBudget
}

// RequestBudget is the nested per-request deadline chain from measurement-contract
// §8. The ordering invariant it must satisfy is:
//
//	LockTimeout < StatementTimeout <= TxnBudget < ServerDeadline < ClientDeadline
//
// so the innermost responsible layer times out first and returns an explicit,
// classified outcome rather than a generic outer timeout. Admission and DB-pool
// acquisition happen outside the transaction budget; ClientDeadline is owned by the
// client/load system, but the service records it and validates the ordering against
// it. The values are provisional hypotheses; the ordering is normative.
type RequestBudget struct {
	// ClientDeadline is the client end-to-end deadline: a degraded-operation safety
	// ceiling, not a latency SLO. Owned by the client; the service validates the
	// chain against it and exposes it for provenance.
	ClientDeadline time.Duration
	// ServerDeadline is the per-request Go-context deadline: the authoritative
	// business-operation deadline inside the service.
	ServerDeadline time.Duration
	// AdmissionCap bounds the admission decision before work is accepted or explicitly
	// rejected/deferred. It sits outside the transaction budget.
	AdmissionCap time.Duration
	// DBAcquireCap bounds waiting for a pooled database connection. It sits outside
	// the transaction budget.
	DBAcquireCap time.Duration
	// LockTimeout maps to PostgreSQL lock_timeout: the bound on each lock acquisition.
	LockTimeout time.Duration
	// StatementTimeout maps to PostgreSQL statement_timeout: the per-statement bound,
	// greater than LockTimeout so a lock wait fails before the statement bound.
	StatementTimeout time.Duration
	// TxnBudget is the total database transaction / context budget inside the server
	// deadline.
	TxnBudget time.Duration
}

// MarshalJSON renders the budget with human-readable duration strings (e.g. "5s")
// rather than raw nanoseconds, so /meta provenance is legible to reviewers while
// remaining machine-parseable.
func (b RequestBudget) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ClientDeadline   string `json:"client_deadline"`
		ServerDeadline   string `json:"server_deadline"`
		AdmissionCap     string `json:"admission_cap"`
		DBAcquireCap     string `json:"db_acquire_cap"`
		LockTimeout      string `json:"lock_timeout"`
		StatementTimeout string `json:"statement_timeout"`
		TxnBudget        string `json:"txn_budget"`
	}{
		ClientDeadline:   b.ClientDeadline.String(),
		ServerDeadline:   b.ServerDeadline.String(),
		AdmissionCap:     b.AdmissionCap.String(),
		DBAcquireCap:     b.DBAcquireCap.String(),
		LockTimeout:      b.LockTimeout.String(),
		StatementTimeout: b.StatementTimeout.String(),
		TxnBudget:        b.TxnBudget.String(),
	})
}

// All timeout fields must be strictly positive; Load rejects zero and negative
// overrides. See the check in Load for why disabling a bound is not a supported mode.

// Default returns the configuration used when no environment overrides are set. The
// RequestBudget values are the provisional §8 hypotheses; WriteResponseMargin is a
// provisional headroom choice documented in measurement-contract §8.1.
func Default() Config {
	return Config{
		ListenAddr:          ":8080",
		ReadHeaderTimeout:   5 * time.Second,
		ReadTimeout:         10 * time.Second,
		WriteTimeout:        10 * time.Second,
		IdleTimeout:         60 * time.Second,
		WriteResponseMargin: 500 * time.Millisecond,
		ShutdownGrace:       15 * time.Second,
		RequestBudget: RequestBudget{
			ClientDeadline:   6000 * time.Millisecond,
			ServerDeadline:   5000 * time.Millisecond,
			AdmissionCap:     250 * time.Millisecond,
			DBAcquireCap:     500 * time.Millisecond,
			LockTimeout:      2000 * time.Millisecond,
			StatementTimeout: 3000 * time.Millisecond,
			TxnBudget:        3500 * time.Millisecond,
		},
	}
}

// Environment variable names recognised by Load.
const (
	envListenAddr          = "ALLOCA_LISTEN_ADDR"
	envReadHeaderTimeout   = "ALLOCA_READ_HEADER_TIMEOUT"
	envReadTimeout         = "ALLOCA_READ_TIMEOUT"
	envWriteTimeout        = "ALLOCA_WRITE_TIMEOUT"
	envIdleTimeout         = "ALLOCA_IDLE_TIMEOUT"
	envWriteResponseMargin = "ALLOCA_WRITE_RESPONSE_MARGIN"
	envShutdownGrace       = "ALLOCA_SHUTDOWN_GRACE"

	envClientDeadline   = "ALLOCA_CLIENT_DEADLINE"
	envServerDeadline   = "ALLOCA_SERVER_DEADLINE"
	envAdmissionCap     = "ALLOCA_ADMISSION_CAP"
	envDBAcquireCap     = "ALLOCA_DB_ACQUIRE_CAP"
	envLockTimeout      = "ALLOCA_LOCK_TIMEOUT"
	envStatementTimeout = "ALLOCA_STATEMENT_TIMEOUT"
	envTxnBudget        = "ALLOCA_TXN_BUDGET"
)

// Load builds a Config from Default, applying any environment overrides found via
// the provided lookup function (typically os.LookupEnv). It returns an error if a
// present variable cannot be parsed or is non-positive, and if the resolved
// RequestBudget violates the §8.1 startup invariants — so a misconfiguration fails
// fast rather than silently falling back to a default or accepting an unsafe chain.
func Load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Default()

	if v, ok := lookup(envListenAddr); ok {
		if v == "" {
			return Config{}, fmt.Errorf("config: %s must not be empty", envListenAddr)
		}
		cfg.ListenAddr = v
	}

	durs := []struct {
		name string
		dst  *time.Duration
	}{
		{envReadHeaderTimeout, &cfg.ReadHeaderTimeout},
		{envReadTimeout, &cfg.ReadTimeout},
		{envWriteTimeout, &cfg.WriteTimeout},
		{envIdleTimeout, &cfg.IdleTimeout},
		{envWriteResponseMargin, &cfg.WriteResponseMargin},
		{envShutdownGrace, &cfg.ShutdownGrace},
		{envClientDeadline, &cfg.RequestBudget.ClientDeadline},
		{envServerDeadline, &cfg.RequestBudget.ServerDeadline},
		{envAdmissionCap, &cfg.RequestBudget.AdmissionCap},
		{envDBAcquireCap, &cfg.RequestBudget.DBAcquireCap},
		{envLockTimeout, &cfg.RequestBudget.LockTimeout},
		{envStatementTimeout, &cfg.RequestBudget.StatementTimeout},
		{envTxnBudget, &cfg.RequestBudget.TxnBudget},
	}
	for _, d := range durs {
		v, ok := lookup(d.name)
		if !ok {
			continue
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("config: %s: %w", d.name, err)
		}
		// Require strictly positive values. net/http treats a zero timeout as
		// "no timeout", so accepting 0s here would silently remove the very bound
		// this config exists to guarantee (e.g. ReadHeaderTimeout against
		// Slowloris). Disabling a bound is therefore not a supported mode; a
		// deployment that wants a longer bound must state a positive duration.
		if parsed <= 0 {
			return Config{}, fmt.Errorf("config: %s must be positive (got %s); a zero or negative timeout disables the bound", d.name, parsed)
		}
		*d.dst = parsed
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate enforces the measurement-contract §8.1 startup invariants over the
// resolved configuration. It is the checked form of the ordering the design docs
// only describe, so an unsafe deadline chain is rejected before the service accepts
// traffic. It deliberately enforces two things and no more:
//
//  1. The per-request nesting (§8.1 clause 1):
//     LockTimeout < StatementTimeout <= TxnBudget < ServerDeadline < ClientDeadline.
//  2. The write-phase relationship (§8.1 clause 2):
//     WriteTimeout > ServerDeadline + WriteResponseMargin.
//
// It does NOT compare ReadHeaderTimeout, ReadTimeout, or IdleTimeout with the
// business deadline: those are independent transport bounds sized by their own
// concern (§8.1 clause 2, phase table), and mechanically nesting them would be a
// misreading of the contract.
func (c Config) Validate() error {
	b := c.RequestBudget

	// Positivity of the budget fields and the write margin. Load already guarantees
	// this for env-overridden values, but Validate is the startup gate and must also
	// reject a directly-constructed unsafe Config, so a non-positive value cannot slip
	// through the ordering checks below (e.g. a negative LockTimeout).
	positives := []struct {
		name string
		v    time.Duration
	}{
		{"WriteResponseMargin", c.WriteResponseMargin},
		{"RequestBudget.ClientDeadline", b.ClientDeadline},
		{"RequestBudget.ServerDeadline", b.ServerDeadline},
		{"RequestBudget.AdmissionCap", b.AdmissionCap},
		{"RequestBudget.DBAcquireCap", b.DBAcquireCap},
		{"RequestBudget.LockTimeout", b.LockTimeout},
		{"RequestBudget.StatementTimeout", b.StatementTimeout},
		{"RequestBudget.TxnBudget", b.TxnBudget},
	}
	for _, p := range positives {
		if p.v <= 0 {
			return fmt.Errorf("config: %s must be positive (got %s)", p.name, p.v)
		}
	}

	// Per-request nesting (§8.1 clause 1). The innermost responsible layer must time
	// out first, so each bound is strictly smaller than the one that contains it —
	// except statement_timeout, which may equal the transaction budget.
	switch {
	case b.LockTimeout >= b.StatementTimeout:
		return fmt.Errorf("config: lock_timeout (%s) must be < statement_timeout (%s)", b.LockTimeout, b.StatementTimeout)
	case b.StatementTimeout > b.TxnBudget:
		return fmt.Errorf("config: statement_timeout (%s) must be <= txn_budget (%s)", b.StatementTimeout, b.TxnBudget)
	case b.TxnBudget >= b.ServerDeadline:
		return fmt.Errorf("config: txn_budget (%s) must be < server_deadline (%s)", b.TxnBudget, b.ServerDeadline)
	case b.ServerDeadline >= b.ClientDeadline:
		return fmt.Errorf("config: server_deadline (%s) must be < client_deadline (%s)", b.ServerDeadline, b.ClientDeadline)
	}

	// Write-phase relationship (§8.1 clause 2): the Go context deadline must fire
	// before the connection-level write timeout, with margin for encoding and writing
	// the response, so an overrun is a classified timeout_server/timeout_db rather
	// than a torn connection. The required relationship is
	// WriteTimeout > ServerDeadline + WriteResponseMargin, but we must not compute that
	// sum: time.Duration is an int64, so a large (parseable) override could overflow it
	// to a negative value and make an unsafe config pass. Instead we establish
	// WriteTimeout > ServerDeadline first, then compare the (now safely positive)
	// difference against the margin — no addition, no overflow.
	if c.WriteTimeout <= b.ServerDeadline || c.WriteTimeout-b.ServerDeadline <= c.WriteResponseMargin {
		return fmt.Errorf("config: WriteTimeout (%s) must be > server_deadline (%s) + WriteResponseMargin (%s)", c.WriteTimeout, b.ServerDeadline, c.WriteResponseMargin)
	}

	return nil
}

// LoadFromOS is a convenience wrapper around Load using the process environment.
func LoadFromOS() (Config, error) {
	return Load(os.LookupEnv)
}
