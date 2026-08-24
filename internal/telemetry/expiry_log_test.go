package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/telemetry"
)

// The healthy expiry line must not carry `failed`.
//
// The worker ticks for the life of the process, so this is the line an operator actually
// reads, and a `"failed":false` on it has already been read as an incident. The absent-key
// assertion is the discriminating one: it is the only case here that fails against an
// implementation that emits the boolean unconditionally. Asserting the failure case alone
// would pass either way.
func TestExpiryLogNamesFailureOnlyWhenItHappened(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failed     bool
		wantFailed any
	}{
		{name: "healthy iteration omits the field", failed: false, wantFailed: nil},
		{name: "failed iteration says so", failed: true, wantFailed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			rec := telemetry.NewSlogRecorder(slog.New(slog.NewJSONHandler(&out, nil)))

			rec.RecordExpiry(context.Background(), telemetry.ExpiryObservation{
				Slots:    7,
				Expired:  3,
				Failed:   tc.failed,
				Duration: 12 * time.Millisecond,
			})

			var line map[string]any
			if err := json.Unmarshal(out.Bytes(), &line); err != nil {
				t.Fatalf("emitted line is not JSON: %v (%q)", err, out.String())
			}

			got, present := line["failed"]
			switch want := tc.wantFailed; {
			case want == nil && present:
				t.Errorf("healthy line carries failed=%v; the field must be absent", got)
			case want != nil && !present:
				t.Errorf("failed line omits the field; it must say failed=true")
			case want != nil && got != want:
				t.Errorf("failed = %v, want %v", got, want)
			}

			// Without these the absence above could be an empty line rather than a
			// healthy one, and the test would pass for the wrong reason.
			if line["msg"] != "expiry_iteration" {
				t.Errorf("msg = %v, want expiry_iteration", line["msg"])
			}
			if line["slots"] != float64(7) || line["expired"] != float64(3) {
				t.Errorf("counts not carried: slots=%v expired=%v", line["slots"], line["expired"])
			}
			if _, ok := line["duration_ms"]; !ok {
				t.Error("duration_ms not carried")
			}
		})
	}
}
