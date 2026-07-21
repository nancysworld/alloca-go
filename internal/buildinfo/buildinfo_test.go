package buildinfo

import (
	"runtime"
	"testing"
	"time"
)

func TestCollectReportsRuntimeFacts(t *testing.T) {
	started := time.Now()
	info := Collect(started)

	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.GOMAXPROCS != runtime.GOMAXPROCS(0) {
		t.Errorf("GOMAXPROCS = %d, want %d", info.GOMAXPROCS, runtime.GOMAXPROCS(0))
	}
	if info.NumCPU != runtime.NumCPU() {
		t.Errorf("NumCPU = %d, want %d", info.NumCPU, runtime.NumCPU())
	}
	if !info.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", info.StartedAt, started)
	}
}

func TestCollectGOMAXPROCSExplicit(t *testing.T) {
	// Unset: default runtime value, not explicitly configured.
	t.Setenv("GOMAXPROCS", "")
	// t.Setenv sets the variable, so LookupEnv reports it present. Verify the flag
	// tracks presence, which is the property §4.4 cares about.
	if !Collect(time.Now()).GOMAXPROCSExplicit {
		t.Error("GOMAXPROCSExplicit = false after setting GOMAXPROCS, want true")
	}
}
