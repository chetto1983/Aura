package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// countingRoundTripper counts every RoundTrip call so a test can prove a
// client made ZERO network calls, rather than merely that it returned an
// error — a sentinel that "forgot" to skip the network but still failed
// (e.g. on a connection error) would pass a weaker assertion.
type countingRoundTripper struct {
	calls atomic.Int64
}

func (c *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return nil, context.Canceled
}

// installCountingTransport swaps the process-global http.DefaultTransport for a
// counter until the test ends. Callers must NOT call t.Parallel(): a parallel
// test swapping a global races every other parallel test that reaches the
// network through it (CI run 34450676656, race + leak DB tier).
func installCountingTransport(t *testing.T) *countingRoundTripper {
	t.Helper()
	rt := &countingRoundTripper{}
	prev := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = prev })
	return rt
}

// TestLLMNotConfiguredClientStillWorks pins the pre-existing sentinel's
// payload — this file did not exist before this plan (02-VALIDATION.md's
// Wave 0 gap), so llmNotConfiguredClient's behavior had no test asserting it
// directly anywhere in cmd/aura.
func TestLLMNotConfiguredClientStillWorks(t *testing.T) {
	rt := installCountingTransport(t)

	ch, err := llmNotConfiguredClient{}.Stream(context.Background(), llm.Request{})
	if ch != nil {
		t.Fatal("llmNotConfiguredClient.Stream returned a non-nil channel")
	}
	if err == nil {
		t.Fatal("llmNotConfiguredClient.Stream returned a nil error")
	}
	if rt.calls.Load() != 0 {
		t.Fatalf("llmNotConfiguredClient.Stream made %d network call(s), want 0", rt.calls.Load())
	}

	var payload struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if decErr := json.Unmarshal([]byte(err.Error()), &payload); decErr != nil {
		t.Fatalf("error string did not JSON-decode: %v (%q)", decErr, err.Error())
	}
	if payload.Error != llmNotConfiguredCode {
		t.Fatalf("payload.Error = %q, want %q", payload.Error, llmNotConfiguredCode)
	}
	if payload.Hint != llmNotConfiguredHint {
		t.Fatalf("payload.Hint = %q, want %q", payload.Hint, llmNotConfiguredHint)
	}
}

// TestCreditExhaustedClientStreamRefusesWithoutNetwork asserts "before the
// model is called" as an OBSERVATION (a transport call counter reading
// zero), not as a claim about where in the code the refusal happens.
func TestCreditExhaustedClientStreamRefusesWithoutNetwork(t *testing.T) {
	rt := installCountingTransport(t)

	ch, err := creditExhaustedClient{}.Stream(context.Background(), llm.Request{Model: "some/model"})
	if ch != nil {
		t.Fatal("creditExhaustedClient.Stream returned a non-nil channel")
	}
	if err == nil {
		t.Fatal("creditExhaustedClient.Stream returned a nil error")
	}
	if rt.calls.Load() != 0 {
		t.Fatalf("creditExhaustedClient.Stream made %d network call(s), want 0", rt.calls.Load())
	}
}

// TestCreditExhaustedErrorPayload asserts the refusal is machine-readable
// and matches the UI copy, so the two cannot drift: decode the error string
// as JSON and check fields, never substring-match the whole message.
func TestCreditExhaustedErrorPayload(t *testing.T) {
	t.Parallel()
	_, err := creditExhaustedClient{}.Stream(context.Background(), llm.Request{})
	if err == nil {
		t.Fatal("Stream returned a nil error")
	}

	var payload struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if decErr := json.Unmarshal([]byte(err.Error()), &payload); decErr != nil {
		t.Fatalf("error string did not JSON-decode: %v (%q)", decErr, err.Error())
	}
	if payload.Error != "credit_exhausted" {
		t.Fatalf("payload.Error = %q, want %q", payload.Error, "credit_exhausted")
	}
	if !strings.Contains(payload.Hint, "Settings") || !strings.Contains(payload.Hint, "credit") {
		t.Fatalf("payload.Hint = %q, want it to mention Settings and credit", payload.Hint)
	}
}
