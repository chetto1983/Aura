package handlers

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

func isRetryableBeforeEffects(err error) bool {
	var r interface{ RetryableBeforeEffects() bool }
	return errors.As(err, &r) && r.RetryableBeforeEffects()
}

func runJob(t *testing.T, turns ...agenttest.FakeTurn) error {
	t.Helper()
	fc := agenttest.NewFakeClient(turns...)
	// A per-call timeout lets the agent's own stream retry wait its 750 ms; the zero config
	// would expire the call before that wait and turn every failure into a deadline.
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, LLM: llm.Config{TotalTimeoutSec: 30}, Registry: jobRegistry()}}
	_, err := h.Run(context.Background(), Job{Payload: []byte(`{"goal":"summarize the news"}`), StepBudget: 5, RunID: "run-retry"})
	if err == nil {
		t.Fatal("the scripted failure must fail the run")
	}
	return err
}

// TestAgentJobMarksAnOutageBeforeAnyToolRetryable reproduces the run measured on the lab VM on
// 2026-10-09: the provider answered HTTP 502 because the internet was down, and the agent's own
// stream retry failed too. The model never asked for a tool, so the failure is marked for the
// dispatcher to fire the job again, and the provider error stays reachable through the mark.
func TestAgentJobMarksAnOutageBeforeAnyToolRetryable(t *testing.T) {
	t.Parallel()
	bad := &openai_compat.HTTPError{StatusCode: 502}
	err := runJob(t, agenttest.FakeTurn{Err: bad}, agenttest.FakeTurn{Err: bad})
	if !isRetryableBeforeEffects(err) {
		t.Fatalf("a 502 before any tool must be retryable, got %v", err)
	}
	if httpErr, ok := errors.AsType[*openai_compat.HTTPError](err); !ok || httpErr.StatusCode != 502 {
		t.Fatalf("the provider error must stay unwrappable, got %v", err)
	}
	if got := err.Error(); got != "agent_job run: "+bad.Error() {
		t.Fatalf("error text = %q, want the agent_job run wrap unchanged", got)
	}
}

// TestAgentJobNeverRetriesAfterATool covers the rule that keeps a retry from repeating an
// effect: once the model asked for a tool, the same transient failure is reported, not retried.
func TestAgentJobNeverRetriesAfterATool(t *testing.T) {
	t.Parallel()
	bad := &openai_compat.HTTPError{StatusCode: 502}
	err := runJob(t,
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "loop", `{"n":1}`)),
		agenttest.FakeTurn{Err: bad}, agenttest.FakeTurn{Err: bad},
	)
	if isRetryableBeforeEffects(err) {
		t.Fatalf("a failure after a tool call must not be retryable, got %v", err)
	}
	if !errors.Is(err, bad) {
		t.Fatalf("the provider error must still propagate, got %v", err)
	}
}

// TestAgentJobDoesNotRetryAPermanentFailure: a request the provider rejects fails the same way
// every time, so it is reported at once.
func TestAgentJobDoesNotRetryAPermanentFailure(t *testing.T) {
	t.Parallel()
	err := runJob(t, agenttest.FakeTurn{Err: &openai_compat.HTTPError{StatusCode: 400}})
	if isRetryableBeforeEffects(err) {
		t.Fatalf("a 400 must not be retryable, got %v", err)
	}
}

// TestRunFailureClassifiesTheOutageShapes pins the errors an outage produces when Aura calls a
// cloud provider directly: the lookup fails temporarily (SERVFAIL or a refused resolver) or
// times out. A name that does not exist and the job's own deadline are not outages.
func TestRunFailureClassifiesTheOutageShapes(t *testing.T) {
	t.Parallel()
	dial := func(dnsErr *net.DNSError) error {
		return &url.Error{Op: "Post", URL: "https://openrouter.ai/api/v1/chat/completions",
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}}
	}
	for _, tc := range []struct {
		name       string
		err        error
		askedTools bool
		want       bool
	}{
		{"dns servfail", dial(&net.DNSError{Err: "server misbehaving", Name: "openrouter.ai", IsTemporary: true}), false, true},
		{"dns timeout", dial(&net.DNSError{Err: "i/o timeout", Name: "openrouter.ai", IsTimeout: true}), false, true},
		{"dns no such host", dial(&net.DNSError{Err: "no such host", Name: "openrouter.ai", IsNotFound: true}), false, false},
		{"dns servfail after a tool", dial(&net.DNSError{Err: "server misbehaving", IsTemporary: true}), true, false},
		{"job deadline", context.DeadlineExceeded, false, false},
		{"cancelled", context.Canceled, false, false},
		{"stream went silent", openai_compat.ErrStreamIdleTimeout, false, true},
		{"rate limited", &openai_compat.HTTPError{StatusCode: 429}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runFailure(tc.err, tc.askedTools)
			if isRetryableBeforeEffects(got) != tc.want {
				t.Fatalf("runFailure(%v, %v) retryable = %v, want %v", tc.err, tc.askedTools, !tc.want, tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("runFailure must keep %v reachable, got %v", tc.err, got)
			}
		})
	}
}
