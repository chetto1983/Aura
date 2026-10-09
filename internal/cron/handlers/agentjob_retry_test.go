package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

func isRetryableBeforeEffects(err error) bool {
	var r interface{ RetryableBeforeEffects() bool }
	return errors.As(err, &r) && r.RetryableBeforeEffects()
}

// sendTool stands in for a tool that acts outside Aura, such as send_message.
type sendTool struct{}

func (sendTool) Spec() tools.Spec {
	return tools.Spec{Name: "send", Summary: "test send", Parameters: json.RawMessage(`{"type":"object"}`), Mutating: true}
}

func (sendTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Preview: "sent", Bytes: 4}, nil
}

func runJob(t *testing.T, turns ...agenttest.FakeTurn) error {
	t.Helper()
	fc := agenttest.NewFakeClient(turns...)
	reg := jobRegistry()
	reg.Register(sendTool{})
	// A per-call timeout lets the agent's own stream retry wait its 750 ms; the zero config
	// would expire the call before that wait and turn every failure into a deadline.
	h := AgentJobHandler{Deps: AgentDeps{Client: fc, LLM: llm.Config{TotalTimeoutSec: 30}, Registry: reg}}
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

// TestAgentJobRetriesAfterReadOnlyToolsOnly covers the rule that keeps a retry from repeating an
// effect without losing the common job: one that searched and then lost the model is retried,
// because a read-only tool left nothing behind. Once the model asked for a tool that changes
// state, or one the registry does not know, the same failure is reported instead.
func TestAgentJobRetriesAfterReadOnlyToolsOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		tool string
		want bool
	}{
		{"read-only tool", "loop", true},
		{"mutating tool", "send", false},
		{"unknown tool", "no_such_tool", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := &openai_compat.HTTPError{StatusCode: 502}
			err := runJob(t,
				agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", tc.tool, `{"n":1}`)),
				agenttest.FakeTurn{Err: bad}, agenttest.FakeTurn{Err: bad},
			)
			if isRetryableBeforeEffects(err) != tc.want {
				t.Fatalf("after %s: retryable = %v, want %v (%v)", tc.tool, !tc.want, tc.want, err)
			}
			if !errors.Is(err, bad) {
				t.Fatalf("the provider error must still propagate, got %v", err)
			}
		})
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

// TestRunFailureClassifiesTheOutageShapes pins the errors an outage produces: the lookup fails
// temporarily (SERVFAIL or a refused resolver) or times out when Aura calls a cloud provider
// directly, and a model call left hanging runs out of its own time while the job still has
// time. A name that does not exist, a cancel and the job's own deadline are not outages.
func TestRunFailureClassifiesTheOutageShapes(t *testing.T) {
	t.Parallel()
	dial := func(dnsErr *net.DNSError) error {
		return &url.Error{Op: "Post", URL: "https://openrouter.ai/api/v1/chat/completions",
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}}
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for _, tc := range []struct {
		name          string
		err           error
		askedMutating bool
		jobCtx        context.Context
		want          bool
	}{
		{"dns servfail", dial(&net.DNSError{Err: "server misbehaving", Name: "openrouter.ai", IsTemporary: true}), false, context.Background(), true},
		{"dns timeout", dial(&net.DNSError{Err: "i/o timeout", Name: "openrouter.ai", IsTimeout: true}), false, context.Background(), true},
		{"dns no such host", dial(&net.DNSError{Err: "no such host", Name: "openrouter.ai", IsNotFound: true}), false, context.Background(), false},
		{"dns servfail after a mutating tool", dial(&net.DNSError{Err: "server misbehaving", IsTemporary: true}), true, context.Background(), false},
		{"call timed out, job has time", context.DeadlineExceeded, false, context.Background(), true},
		{"call timed out after a mutating tool", context.DeadlineExceeded, true, context.Background(), false},
		{"job deadline", context.DeadlineExceeded, false, expired, false},
		{"cancelled", context.Canceled, false, context.Background(), false},
		{"stream went silent", openai_compat.ErrStreamIdleTimeout, false, context.Background(), true},
		{"rate limited", &openai_compat.HTTPError{StatusCode: 429}, false, context.Background(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runFailure(tc.jobCtx, tc.err, tc.askedMutating)
			if isRetryableBeforeEffects(got) != tc.want {
				t.Fatalf("runFailure(%v, %v) retryable = %v, want %v", tc.err, tc.askedMutating, !tc.want, tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("runFailure must keep %v reachable, got %v", tc.err, got)
			}
		})
	}
}

// hangingClient answers its scripted turns, then leaves every later call hanging until the
// call's context ends: a request the network dropped before any byte came back.
type hangingClient struct {
	mu    sync.Mutex
	turns []agenttest.FakeTurn
	calls int
}

func (c *hangingClient) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	c.mu.Lock()
	i := c.calls
	c.calls++
	c.mu.Unlock()
	if i >= len(c.turns) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ch := make(chan llm.Chunk, len(c.turns[i].Chunks))
	for _, chunk := range c.turns[i].Chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

// TestAgentJobRetriesAModelCallThatHungPastItsTimeout reproduces the lab VM run of 2026-10-09:
// the model asked for a read-only tool, the network went away, and the next model call hung
// until its own timeout. The job still had time, so the failure is retryable. When the job's own
// deadline ends the same hanging call, it is not.
func TestAgentJobRetriesAModelCallThatHungPastItsTimeout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		callTimeout int
		jobDuration time.Duration
		want        bool
	}{
		{"call timeout inside the job", 1, time.Minute, true},
		{"job deadline first", 30, time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &hangingClient{turns: []agenttest.FakeTurn{
				agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "loop", `{"n":1}`)),
			}}
			h := AgentJobHandler{Deps: AgentDeps{
				Client: client, LLM: llm.Config{TotalTimeoutSec: tc.callTimeout},
				Registry: jobRegistry(), MaxDuration: tc.jobDuration,
			}}
			_, err := h.Run(context.Background(), Job{Payload: []byte(`{"goal":"summarize the news"}`), StepBudget: 5, RunID: "run-hang"})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("the hanging call must end on a deadline, got %v", err)
			}
			if isRetryableBeforeEffects(err) != tc.want {
				t.Fatalf("retryable = %v, want %v (%v)", !tc.want, tc.want, err)
			}
		})
	}
}

// TestMutatingToolWithoutARegistry: with no registry nothing can be classified, so every tool
// counts as one that changes state and no retry can repeat an effect.
func TestMutatingToolWithoutARegistry(t *testing.T) {
	t.Parallel()
	if !(AgentJobHandler{}).mutatingTool("web_search") {
		t.Fatal("without a registry a tool must count as mutating")
	}
}
