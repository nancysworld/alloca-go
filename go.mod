module github.com/nancysworld/alloca-go

// go: the module language version (minimum Go semantics the code relies on).
// toolchain: the exact compiler used for CI and every measurement, so benchmark
// comparisons never silently change compiler patch versions. CI reads both via
// actions/setup-go go-version-file, and /meta reports the toolchain actually used.
go 1.26.0

toolchain go1.26.5
