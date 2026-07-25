module github.com/nancysworld/alloca-go

// go: the module language version (minimum Go semantics the code relies on).
// toolchain: the exact compiler used for CI and every measurement, so benchmark
// comparisons never silently change compiler patch versions. CI reads both via
// actions/setup-go go-version-file, and /meta reports the toolchain actually used.
go 1.26.0

toolchain go1.26.5

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pressly/goose/v3 v3.27.3
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
