package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// stubRow answers one Scan, so the gate's refusal paths can be exercised without a
// database. The real query is covered by the integration suite.
type stubRow struct {
	version *int64
	err     error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(**int64)) = r.version
	return nil
}

type stubQuerier struct{ row stubRow }

func (q stubQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return q.row }

func version(v int64) *int64 { return &v }

// ADR-0002 keeps migration out of the serving path, so a serving process cannot repair
// what it finds. Every one of these must stop the process rather than let it meet the
// mismatch one failing query at a time, under load.
func TestCheckSchemaRefusesToServeAnIncompatibleAuthority(t *testing.T) {
	expected, err := ExpectedSchemaVersion()
	if err != nil {
		t.Fatalf("reading the expected version: %v", err)
	}
	if expected <= 0 {
		t.Fatalf("expected schema version = %d, want a positive version from the embedded migrations", expected)
	}

	tests := []struct {
		name string
		row  stubRow
		want string
	}{
		{
			name: "table missing or unreadable",
			row:  stubRow{err: errors.New(`relation "goose_db_version" does not exist`)},
			want: "cannot read the applied schema version",
		},
		{
			name: "table present, nothing applied",
			row:  stubRow{version: nil},
			want: "no migration is applied",
		},
		{
			name: "authority behind this binary",
			row:  stubRow{version: version(expected - 1)},
			want: "run alloca-migrate before serving",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckSchema(context.Background(), stubQuerier{row: tc.row})
			if err == nil {
				t.Fatalf("an incompatible authority was accepted (version %d)", got)
			}
			if !errors.Is(err, ErrSchemaIncompatible) {
				t.Errorf("error %v does not wrap ErrSchemaIncompatible", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A rollout migrates before the new version serves (ADR-0002), so every not-yet-replaced
// replica runs against a schema newer than itself. Refusing that would make the gate
// reject the normal deploy.
func TestCheckSchemaAcceptsAnAuthorityAtOrAheadOfThisBinary(t *testing.T) {
	expected, err := ExpectedSchemaVersion()
	if err != nil {
		t.Fatalf("reading the expected version: %v", err)
	}

	for _, applied := range []int64{expected, expected + 1} {
		t.Run(fmt.Sprintf("applied=%d", applied), func(t *testing.T) {
			got, err := CheckSchema(context.Background(), stubQuerier{row: stubRow{version: version(applied)}})
			if err != nil {
				t.Fatalf("a compatible authority was refused: %v", err)
			}
			if got != applied {
				t.Errorf("reported version = %d, want %d", got, applied)
			}
		})
	}
}
