package config

import (
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

func TestLoadRejectsEmptyListenAddr(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{envListenAddr: ""}))
	if err == nil {
		t.Fatal("Load accepted an empty listen address, want error")
	}
}
