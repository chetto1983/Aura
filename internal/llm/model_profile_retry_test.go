package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The measured failure: one blip while the container was starting, and the catalogue was
// never asked again for the life of the process. GET /models answered 200 in 0.25s from
// inside that same container hours later, so nothing was misconfigured -- the fetch lost
// a startup race and the config kept the hole.
func TestModelProfileRetriesAnUnreachableCatalogue(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			// What a half-started dependency looks like from here.
			hijacked, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = hijacked.Close()
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m","context_length":8192,` +
			`"pricing":{"prompt":"0.000001","completion":"0.000002"},` +
			`"reasoning":{"mandatory":true,"supported_efforts":["max","high","low"]}}]}`))
	}))
	defer server.Close()

	metadata, err := fetchModelProfileWithRetry(
		t.Context(), server.Client(), "openrouter", server.URL, "", "m")
	if err != nil {
		t.Fatalf("profile unresolved after %d attempts: %v", calls.Load(), err)
	}
	if calls.Load() != 3 {
		t.Fatalf("catalogue read %d times, want the two failures then the success", calls.Load())
	}
	if !metadata.ReasoningMandatory || len(metadata.SupportedReasoningEfforts) != 3 {
		t.Fatalf("metadata = %+v, want the reasoning contract the retry existed to recover", metadata)
	}
}

// A catalogue that ANSWERS and does not describe the model has settled the question.
// Asking again returns the same answer, so retrying it would turn a fast legible failure
// into a slow one.
func TestModelProfileDoesNotRetryASettledAnswer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	if _, err := fetchModelProfileWithRetry(
		t.Context(), server.Client(), "openrouter", server.URL, "", "absent"); err == nil {
		t.Fatal("a catalogue without the model resolved a profile")
	}
	if calls.Load() != 1 {
		t.Fatalf("catalogue read %d times for an answer it had already given", calls.Load())
	}
}

// The retry budget rides on the caller's context, so a boot being cancelled does not
// first sit through the backoffs it has left.
func TestModelProfileRetryStopsWithTheContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacked, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_ = hijacked.Close()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	started := time.Now()
	if _, err := fetchModelProfileWithRetry(
		ctx, server.Client(), "openrouter", server.URL, "", "m"); err == nil {
		t.Fatal("a cancelled boot resolved a profile")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled retry took %s, so it slept through its remaining backoff", elapsed)
	}
}

// The cause has to survive the wrapping. It was dropped for a bare "GET /models failed",
// which reads identically whether the key was missing, DNS was not up, or the endpoint
// answered 500 -- and the silence downstream had already killed live turns before anyone
// could tell those apart by hand.
func TestModelProfileFailureNamesItsCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := FetchModelProfile(t.Context(), server.Client(), "openrouter", server.URL, "", "m")
	if err == nil {
		t.Fatal("a failing catalogue resolved a profile")
	}
	if !errors.Is(err, ErrModelProfileUnavailable) || !errors.Is(err, ErrModelCatalogueUnreachable) {
		t.Fatalf("err = %v, want both the profile and the transport sentinel", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, which does not say what actually went wrong", err)
	}
}

// ReasoningMandatory was carried from the catalogue into the config and read by nobody,
// while the model it described refused every request that disabled reasoning.
func TestClampRefusesToDisableReasoningTheModelDeclaredMandatory(t *testing.T) {
	mandatory := Config{
		ReasoningMandatory:        true,
		SupportedReasoningEfforts: []ReasoningEffort{ReasoningEffortMax, ReasoningEffortHigh, ReasoningEffortLow},
	}
	if got := mandatory.ClampReasoningEffort(ReasoningEffortNone); got != ReasoningEffortLow {
		t.Fatalf("clamp(none) = %q on a mandatory-reasoning model, want the cheapest it allows", got)
	}
	// Sizing is untouched: only disabling is refused.
	if got := mandatory.ClampReasoningEffort(ReasoningEffortHigh); got != ReasoningEffortHigh {
		t.Fatalf("clamp(high) = %q, want it left alone", got)
	}
	optional := Config{SupportedReasoningEfforts: []ReasoningEffort{ReasoningEffortNone, ReasoningEffortHigh}}
	if got := optional.ClampReasoningEffort(ReasoningEffortNone); got != ReasoningEffortNone {
		t.Fatalf("clamp(none) = %q on a model that permits none", got)
	}
}
