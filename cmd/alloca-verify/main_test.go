package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The verifier's own setup errors, which are the ones that would otherwise be discovered as a
// clean verdict over the wrong topology.
//
// Every case here is refused *before* a database is read, so they need no schema. That is also
// why they are worth having: each one produces a well-formed verdict if it is not caught, and
// a well-formed verdict over a partition the run did not use is indistinguishable from a
// correct one.

const twoAuthorities = `{
  "version": "test-v1",
  "homes": {"org-a": "authority-1", "org-b": "authority-2",
            "org-c": "authority-1", "org-d": "authority-2"}
}`

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// A report the run flags are checked against. Its contents do not matter to these cases: they
// are refused before anything is reconciled.
func writeReport(t *testing.T) string {
	t.Helper()
	return writeFile(t, "run.json", `{"manifest":{},"summary":{}}`)
}

func TestVerifierRefusesAContradictoryTopologyConfiguration(t *testing.T) {
	reportPath := writeReport(t)
	placementPath := writeFile(t, "placement.json", twoAuthorities)

	const dsn1 = "postgres://alloca:alloca@localhost:15433/alloca?sslmode=disable"
	const dsn2 = "postgres://alloca:alloca@localhost:15434/alloca?sslmode=disable"

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a placement and a single database",
			args: []string{"-placement", placementPath, "-database-url", dsn1},
			want: "mutually exclusive",
		},
		{
			name: "a placement and a single organisation",
			args: []string{"-placement", placementPath, "-org", "org-a",
				"-authority-db", "authority-1=" + dsn1, "-authority-db", "authority-2=" + dsn2},
			want: "mutually exclusive",
		},
		{
			name: "one scrape for a multi-authority run",
			args: []string{"-placement", placementPath,
				"-authority-db", "authority-1=" + dsn1, "-authority-db", "authority-2=" + dsn2,
				"-metrics", "after.prom"},
			want: "single-authority flags",
		},
		{
			name: "an authority with no database",
			args: []string{"-placement", placementPath, "-authority-db", "authority-1=" + dsn1},
			want: "no -authority-db was supplied",
		},
		{
			name: "a database for an authority the map never names",
			args: []string{"-placement", placementPath,
				"-authority-db", "authority-1=" + dsn1, "-authority-db", "authority-2=" + dsn2,
				"-authority-db", "authority-9=" + dsn2},
			want: "never mentions",
		},
		{
			name: "a scrape for an authority the map never names",
			args: []string{"-placement", placementPath,
				"-authority-db", "authority-1=" + dsn1, "-authority-db", "authority-2=" + dsn2,
				"-authority-metrics", "authority-9=after.prom"},
			want: "never mentions",
		},
		{
			name: "the same authority twice",
			args: []string{"-placement", placementPath,
				"-authority-db", "authority-1=" + dsn1, "-authority-db", "authority-1=" + dsn2},
			want: "already has",
		},
		{
			name: "authorities without a placement",
			args: []string{"-authority-db", "authority-1=" + dsn1},
			want: "needs -placement",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := run(append([]string{"-run", reportPath}, tc.args...))
			if err == nil {
				t.Fatal("a contradictory verifier configuration was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A placement document that does not parse must stop the verifier rather than degrade it to
// some default routing, for the same reason it stops a service unit at startup: a verdict
// computed against a map nobody wrote describes a topology nobody ran.
func TestVerifierRefusesAPlacementDocumentItCannotParse(t *testing.T) {
	placementPath := writeFile(t, "placement.json",
		`{"version": "test-v1", "homes": {"org-a": "authority-1"}, "extra": true}`)

	err := run([]string{"-run", writeReport(t), "-placement", placementPath,
		"-authority-db", "authority-1=postgres://alloca@localhost:5432/alloca"})
	if err == nil {
		t.Fatal("a placement document with an unknown field was accepted")
	}
	if !strings.Contains(err.Error(), "parsing placement document") {
		t.Errorf("error %q does not name the placement document as the problem", err)
	}
}
