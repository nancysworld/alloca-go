// Package buildinfo exposes the runtime and build metadata measurement-contract §11
// requires on every capacity run: Go version, observed GOMAXPROCS, whether
// GOMAXPROCS/GODEBUG were set explicitly, and the source revision.
//
// It exists in AG-M0, before any measurement, so that the load system in AG-M2 and
// the deployment in AG-M3 can scrape a single stable endpoint (/meta) for the
// environment provenance every result must carry.
package buildinfo

import (
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// Info is a snapshot of runtime and build metadata. It is safe to marshal to JSON
// for the /meta endpoint.
type Info struct {
	// GoVersion is the toolchain version the binary was built with.
	GoVersion string `json:"go_version"`
	// GOMAXPROCS is the observed runtime.GOMAXPROCS(0) at collection time.
	GOMAXPROCS int `json:"gomaxprocs"`
	// NumCPU is runtime.NumCPU(), the number of logical CPUs usable by the process.
	NumCPU int `json:"num_cpu"`
	// GOMAXPROCSExplicit reports whether the GOMAXPROCS environment variable was
	// set. When false, the value above is the Go runtime default (which, for Go
	// 1.25+, is container-CPU-limit aware). This distinction is required by §4.4.
	GOMAXPROCSExplicit bool `json:"gomaxprocs_explicit"`
	// GODEBUGValue is the raw GODEBUG environment value, empty when unset. It can
	// change scheduler and runtime behaviour that affects capacity results.
	GODEBUGValue string `json:"godebug,omitempty"`
	// Revision is the VCS revision the binary was built from, when available.
	Revision string `json:"revision,omitempty"`
	// Modified reports whether the working tree had uncommitted changes at build.
	Modified bool `json:"modified"`
	// StartedAt is the process start time, set by the caller at startup.
	StartedAt time.Time `json:"started_at"`
}

// Collect gathers the current runtime and build metadata. startedAt is supplied by
// the caller (recorded once at process start) so the value is stable across calls.
func Collect(startedAt time.Time) Info {
	info := Info{
		GoVersion:          runtime.Version(),
		GOMAXPROCS:         runtime.GOMAXPROCS(0),
		NumCPU:             runtime.NumCPU(),
		GOMAXPROCSExplicit: envSet("GOMAXPROCS"),
		GODEBUGValue:       os.Getenv("GODEBUG"),
		StartedAt:          startedAt,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Revision = s.Value
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
	}

	return info
}

// envSet reports whether the named environment variable is present, regardless of
// its value. GOMAXPROCS=0 or an empty override still counts as "explicitly set".
func envSet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}
