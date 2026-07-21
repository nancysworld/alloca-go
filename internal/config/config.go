// Package config loads Alloca-Go service configuration from the environment.
//
// Every field has a safe default so the service runs with no configuration. The
// HTTP timeout defaults are deliberately aligned with the provisional timeout
// budget in docs/design/measurement-contract.md; they are hypotheses, not tuned
// production values, and AG-M3 validates the full deadline chain end to end.
package config

import (
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
	// WriteTimeout bounds the time to write the response.
	WriteTimeout time.Duration
	// IdleTimeout bounds how long a kept-alive connection may sit idle.
	IdleTimeout time.Duration

	// ShutdownGrace bounds graceful shutdown: in-flight requests have until this
	// deadline to complete before the server is forced closed.
	ShutdownGrace time.Duration
}

// Default returns the configuration used when no environment overrides are set.
func Default() Config {
	return Config{
		ListenAddr:        ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownGrace:     15 * time.Second,
	}
}

// Environment variable names recognised by Load.
const (
	envListenAddr        = "ALLOCA_LISTEN_ADDR"
	envReadHeaderTimeout = "ALLOCA_READ_HEADER_TIMEOUT"
	envReadTimeout       = "ALLOCA_READ_TIMEOUT"
	envWriteTimeout      = "ALLOCA_WRITE_TIMEOUT"
	envIdleTimeout       = "ALLOCA_IDLE_TIMEOUT"
	envShutdownGrace     = "ALLOCA_SHUTDOWN_GRACE"
)

// Load builds a Config from Default, applying any environment overrides found via
// the provided lookup function (typically os.LookupEnv). It returns an error if a
// present variable cannot be parsed, so a misconfiguration fails fast rather than
// silently falling back to a default.
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
		{envShutdownGrace, &cfg.ShutdownGrace},
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
		if parsed < 0 {
			return Config{}, fmt.Errorf("config: %s must not be negative", d.name)
		}
		*d.dst = parsed
	}

	return cfg, nil
}

// LoadFromOS is a convenience wrapper around Load using the process environment.
func LoadFromOS() (Config, error) {
	return Load(os.LookupEnv)
}
