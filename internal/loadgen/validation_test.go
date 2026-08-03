package loadgen_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nancysworld/alloca-go/internal/domain"
	"github.com/nancysworld/alloca-go/internal/loadgen"
)

// contradictoryServer answers every request with a status and an outcome that disagree:
// HTTP 200 carrying business_refusal, which the contract maps to 409.
//
// This is the shape of the failure the control exists to catch. A service that silently
// stopped doing the work would answer 200 while its body said otherwise, and a harness
// that only looked at the status would count every one of them as goodput.
func contradictoryServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"outcome":"business_refusal","reason":"no_capacity","replay":false}`))
	}))
}

// TestResponseValidationActiveControl is the mandatory negative control of
// measurement-contract §5.5: a control that **fails when validation is silently disabled**,
// so a reported success cannot be an unchecked 200.
//
// It runs the same contradictory service twice and asserts the two halves that make the
// control meaningful. With validation on, the contradiction is caught and the run is not
// quotable. With validation off, the identical responses pass unremarked — which is the
// proof that the first half was doing work rather than describing a service that happened
// to be well behaved.
//
// A control that only tested the enabled path would pass just as happily against a harness
// whose validation was a no-op.
func TestResponseValidationActiveControl(t *testing.T) {
	srv := contradictoryServer(t)
	defer srv.Close()

	run := func(validate bool) loadgen.Summary {
		c := loadgen.NewClient(srv.URL, 5*time.Second, validate)
		r := loadgen.NewRunner(c, loadgen.Options{Concurrency: 4, Iterations: 40})
		return r.Run(context.Background(), loadgen.HotSlot{
			Org:  "org",
			Slot: loadgen.Slot{OrganisationID: "org", SlotID: "s1"},
		})
	}

	t.Run("validation on catches the contradiction", func(t *testing.T) {
		s := run(true)
		if s.Invalid != s.Completed || s.Completed == 0 {
			t.Fatalf("invalid=%d of completed=%d; every response contradicts the contract "+
				"and every one should have been caught", s.Invalid, s.Completed)
		}
		if s.Sound {
			t.Error("run is marked quotable despite failing response validation")
		}
		if !strings.Contains(strings.Join(s.InvalidSamples, " "), "contradicts outcome") {
			t.Errorf("validation failure does not name the contradiction: %v", s.InvalidSamples)
		}
	})

	t.Run("validation off lets the same responses pass", func(t *testing.T) {
		s := run(false)
		if s.Invalid != 0 {
			t.Fatalf("invalid=%d with validation disabled; the control cannot distinguish "+
				"a disabled check from a passing one", s.Invalid)
		}
		// The run must still refuse to certify itself. This is the half that makes a
		// disabled check safe: the numbers exist, but the summary says why they may not
		// be quoted.
		if s.Sound {
			t.Error("run with validation disabled is marked quotable")
		}
		if !strings.Contains(s.NotSoundBecause, "validation was disabled") {
			t.Errorf("summary does not say validation was disabled: %q", s.NotSoundBecause)
		}
	})
}

// TestValidationRejectsUnknownOutcome covers the other dimension §5.4 requires: an outcome
// outside the closed set is a validation failure even when the status is plausible.
func TestValidationRejectsUnknownOutcome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"outcome":"probably_fine","replay":false}`))
	}))
	defer srv.Close()

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	r := loadgen.NewRunner(c, loadgen.Options{Concurrency: 2, Iterations: 10})
	s := r.Run(context.Background(), loadgen.HotSlot{Org: "org",
		Slot: loadgen.Slot{OrganisationID: "org", SlotID: "s1"}})

	if s.Invalid != s.Completed {
		t.Fatalf("invalid=%d of %d; an outcome outside the closed set must not validate",
			s.Invalid, s.Completed)
	}
	if !strings.Contains(strings.Join(s.InvalidSamples, " "), "closed terminal-outcome set") {
		t.Errorf("failure does not name the closed-set violation: %v", s.InvalidSamples)
	}
}

// TestValidRunIsQuotable guards the opposite failure: a validator that rejected everything
// would pass every test above while making all measurement impossible.
func TestValidRunIsQuotable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"outcome":"admitted_success","replay":false,"reservation_id":"r1"}`))
	}))
	defer srv.Close()

	c := loadgen.NewClient(srv.URL, 5*time.Second, true)
	r := loadgen.NewRunner(c, loadgen.Options{Concurrency: 4, Iterations: 40})
	s := r.Run(context.Background(), loadgen.HotSlot{Org: "org",
		Slot: loadgen.Slot{OrganisationID: "org", SlotID: "s1"}})

	if s.Invalid != 0 {
		t.Fatalf("valid responses were rejected: %v", s.InvalidSamples)
	}
	if !s.Sound {
		t.Fatalf("clean run is not quotable: %q", s.NotSoundBecause)
	}
	if s.Goodput != s.Completed || s.Completed != 40 {
		t.Fatalf("goodput=%d completed=%d, want 40 and 40", s.Goodput, s.Completed)
	}
}

// TestListingIsNotGoodput pins observability §3.1: a successful listing is admitted_success
// but is not a booking, so it must not count toward mutation goodput. Fold the read route
// into goodput and a dispersed workload's headline number inflates by its read traffic.
func TestListingIsNotGoodput(t *testing.T) {
	responses := []loadgen.Response{
		{Operation: string(domain.OpReserve), Outcome: domain.OutcomeAdmittedSuccess},
		{Operation: "list_slots", Outcome: domain.OutcomeAdmittedSuccess},
	}
	s := loadgen.SummariseForTest(responses)

	if s.Completed != 2 {
		t.Fatalf("completed=%d, want 2: both requests completed", s.Completed)
	}
	if s.Goodput != 1 {
		t.Fatalf("goodput=%d, want 1: a successful listing is not a booking", s.Goodput)
	}
}
