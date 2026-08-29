package swarm

import (
	"context"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"go.uber.org/goleak"
)

// withToolCtx layers the tool-call spillover context the production runTool injects
// alongside the swarm context, so tools.NewResult inside the adapter can place its
// sidecar (the adapter always runs under a runTool-built ctx in production).
func withToolCtx(ctx context.Context, t *testing.T) context.Context {
	return tools.WithToolCallContext(ctx, "sess-adapter", "call-adapter", t.TempDir(), 2048)
}

// TestRunnerAdapterMissingContext pins Amendment #103: a dispatch outside the
// agent loop is a typed execution error, never an OK result containing error text.
func TestRunnerAdapterMissingContext(t *testing.T) {
	defer goleak.VerifyNone(t)
	a := NewRunnerAdapter(testRunConfig(t, newRouter(), 25).Cfg)
	_, err := a.Run(withToolCtx(context.Background(), t), []string{"x"}, "")
	if err == nil {
		t.Fatal("missing context must return an execution error")
	}
}

// TestRunnerAdapterDrivesEngine drives the full adapter path through the ctx seam:
// WithSwarmContext carries the parent budget/registry/client/cfg/convID, the adapter
// reads them off the ctx, and the engine fans the goals out. The worker registry the
// engine builds excludes swarm_spawn (D-08/D-10 — proven by the worker being able to
// run with swarm_spawn dropped; the Without exclusion is re-asserted directly below).
func TestRunnerAdapterDrivesEngine(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done"}).
		route("bravo", outcome{kind: "ok", text: "B done"})
	rc := testRunConfig(t, r, 25)

	ctx := agent.WithSwarmContext(withToolCtx(context.Background(), t),
		rc.ParentBudget, rc.ParentRegistry, rc.Client, rc.LLM, rc.ConvID, rc.Gateway)
	a := NewRunnerAdapter(rc.Cfg)

	res, err := a.Run(ctx, []string{"alpha task", "bravo task"}, "")
	if err != nil {
		t.Fatalf("adapter Run: %v", err)
	}
	reports := parseReports(t, res.Preview)
	if len(reports) != 2 {
		t.Fatalf("want 2 reports, got %d (%q)", len(reports), res.Preview)
	}
	if reports[0].Status != StatusOK || reports[1].Status != StatusOK {
		t.Fatalf("both workers should finish ok: %+v", reports)
	}
	if res.Provenance == nil {
		t.Fatal("swarm reports must carry untrusted provenance")
	}
	if res.Provenance.Source != "swarm" || res.Provenance.Trust != tools.TrustUntrusted {
		t.Fatalf("swarm provenance = %+v, want source=swarm trust=untrusted", res.Provenance)
	}
}

// TestRunnerAdapterThreadsContextToWorkerBrief proves the SWARM-01 context arg
// makes it all the way through RunnerAdapter.Run -> RunConfig.Context ->
// runChild -> structuredBrief for the SYNCHRONOUS path: the router matches on a
// substring that lives ONLY in the context text (never in the goal), so a
// successful report is only possible if the worker's actual outgoing brief
// carried it.
func TestRunnerAdapterThreadsContextToWorkerBrief(t *testing.T) {
	defer goleak.VerifyNone(t)
	const contextMarker = "marker-only-in-context-9f2a"
	r := newRouter().route(contextMarker, outcome{kind: "ok", text: "done"})
	rc := testRunConfig(t, r, 25)

	ctx := agent.WithSwarmContext(withToolCtx(context.Background(), t),
		rc.ParentBudget, rc.ParentRegistry, rc.Client, rc.LLM, rc.ConvID, rc.Gateway)
	a := NewRunnerAdapter(rc.Cfg)

	res, err := a.Run(ctx, []string{"a goal with no marker in it"}, contextMarker)
	if err != nil {
		t.Fatalf("adapter Run: %v", err)
	}
	reports := parseReports(t, res.Preview)
	if len(reports) != 1 || reports[0].Status != StatusOK {
		t.Fatalf("want 1 ok report (proves the router matched the context marker inside the worker's real brief), got %+v: %s", reports, res.Preview)
	}
}

// TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn re-asserts the flat-v1 invariant
// the adapter relies on: the worker registry the engine derives via
// Without(parentRegistry, "swarm_spawn") never contains swarm_spawn (D-08/D-10), so a
// worker cannot recursively fan out (T-09-12 mitigation).
func TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn(t *testing.T) {
	parent := stubWorkerRegistry() // includes a stub swarm_spawn entry
	if _, ok := parent.Get(swarmSpawnTool); !ok {
		t.Fatal("precondition: the parent registry must carry swarm_spawn")
	}
	worker := tools.Without(parent, swarmSpawnTool)
	if _, ok := worker.Get(swarmSpawnTool); ok {
		t.Fatal("the worker registry must NOT contain swarm_spawn (flat v1, D-08/D-10)")
	}
}

// fakeDelegationStore is an in-memory DelegationJobStore double shared by the
// enqueue tests here and the claim-loop tests in delegation_queue_unit_test.go.
// Every method returns a plausible row by default so a test that does not care
// about a leg can ignore it; the claim/error fields let a test drive one specific
// branch without a database. Zero value = "Create works, Claim returns nothing",
// which is exactly what the background-enqueue tests need.
// It is mutex-guarded because the claim loop's heartbeat runs on its own
// goroutine while the test observes the store -- the same reason routerClient
// above is goroutine-safe. Read the counters through the accessors, never the
// fields, or -race will (correctly) call it.
type fakeDelegationStore struct {
	mu      sync.Mutex
	created []documents.CreateIngestionJobRequest

	claimJobs  []documents.IngestionJob
	claimNext  []documents.IngestionJob // handed out by the claim AFTER claimJobs drained (rows that arrived later)
	claimErr   error
	claimCalls int

	transitions  []documents.TransitionIngestionJobRequest
	transitErr   error
	retries      []documents.RetryIngestionJobRequest
	heartbeats   []documents.HeartbeatIngestionJobRequest
	heartbeatErr error

	// answered/unparks back the 51-06b Task 2 DelegationResumeObserver legs
	// (DelegationJobStore.ListAnsweredAwaitingInput/UnparkIngestionJob). Zero
	// value = "nothing answered, unpark matches nothing", the same
	// ignore-it-if-you-don't-care-about-this-leg default every other field here
	// follows.
	answered   []documents.AnsweredAwaitingInputJob
	answerErr  error
	unparks    []documents.UnparkIngestionJobRequest
	unparkRows int64
	unparkErr  error
}

func (s *fakeDelegationStore) claimCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimCalls
}

func (s *fakeDelegationStore) heartbeatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.heartbeats)
}

func (s *fakeDelegationStore) transitionsSnapshot() []documents.TransitionIngestionJobRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]documents.TransitionIngestionJobRequest(nil), s.transitions...)
}

func (s *fakeDelegationStore) retriesSnapshot() []documents.RetryIngestionJobRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]documents.RetryIngestionJobRequest(nil), s.retries...)
}

func (s *fakeDelegationStore) Create(_ context.Context, req documents.CreateIngestionJobRequest) (documents.IngestionJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, req)
	return documents.IngestionJob{ID: "job-" + req.IdempotencyKey, IdentityID: req.IdentityID, JobType: req.JobType}, nil
}

func (s *fakeDelegationStore) Claim(context.Context, documents.ClaimIngestionJobsRequest) ([]documents.IngestionJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls++
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	jobs := s.claimJobs
	s.claimJobs = s.claimNext // the next pass sees what arrived meanwhile; then the queue is drained
	s.claimNext = nil
	return jobs, nil
}

func (s *fakeDelegationStore) UpdateStatus(_ context.Context, req documents.TransitionIngestionJobRequest) (documents.IngestionJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transitions = append(s.transitions, req)
	if s.transitErr != nil {
		return documents.IngestionJob{}, s.transitErr
	}
	return documents.IngestionJob{ID: req.JobID, Status: req.Status}, nil
}

func (s *fakeDelegationStore) Retry(_ context.Context, req documents.RetryIngestionJobRequest) (documents.IngestionJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retries = append(s.retries, req)
	return documents.IngestionJob{ID: req.JobID, Status: "queued"}, nil
}

func (s *fakeDelegationStore) Heartbeat(_ context.Context, req documents.HeartbeatIngestionJobRequest) (documents.IngestionJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats = append(s.heartbeats, req)
	if s.heartbeatErr != nil {
		return documents.IngestionJob{}, s.heartbeatErr
	}
	return documents.IngestionJob{ID: req.JobID, Status: "running"}, nil
}

func (s *fakeDelegationStore) ListAnsweredAwaitingInput(context.Context, string, int) ([]documents.AnsweredAwaitingInputJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.answerErr != nil {
		return nil, s.answerErr
	}
	return append([]documents.AnsweredAwaitingInputJob(nil), s.answered...), nil
}

func (s *fakeDelegationStore) UnparkIngestionJob(_ context.Context, req documents.UnparkIngestionJobRequest) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unparks = append(s.unparks, req)
	if s.unparkErr != nil {
		return 0, s.unparkErr
	}
	return s.unparkRows, nil
}

// TestRunnerAdapterBackgroundsWhenEnqueuerConfigured proves the wiring gap this
// tracer plan closes: a top-level RunnerAdapter with an Enqueuer configured
// resolves the caller's identity off the SAME ambient identityctx every other
// identity-scoped tool reads (no new plumbing through SwarmContextValue), writes
// one durable row per goal, and returns WITHOUT ever dispatching a worker through
// the router (the background path never calls Client.Stream).
func TestRunnerAdapterBackgroundsWhenEnqueuerConfigured(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter() // no routes configured -- a dispatch here would fail the test
	rc := testRunConfig(t, r, 25)

	store := &fakeDelegationStore{}
	a := NewRunnerAdapter(rc.Cfg)
	a.Enqueuer = &DelegationEnqueuer{Store: store}

	ctx := agent.WithSwarmContext(withToolCtx(context.Background(), t),
		rc.ParentBudget, rc.ParentRegistry, rc.Client, rc.LLM, rc.ConvID, rc.Gateway)
	ctx = identityctx.WithIdentityID(ctx, "identity-adapter-test")

	res, err := a.Run(ctx, []string{"alpha task", "bravo task"}, "shared background context")
	if err != nil {
		t.Fatalf("adapter Run: %v", err)
	}
	if len(store.created) != 2 {
		t.Fatalf("enqueued %d rows, want 2 (one per goal)", len(store.created))
	}
	for _, req := range store.created {
		if req.IdentityID != "identity-adapter-test" {
			t.Fatalf("enqueued row identity = %q, want the ambient identityctx value", req.IdentityID)
		}
		if req.JobType != JobTypeSwarmDelegation {
			t.Fatalf("enqueued row job_type = %q, want %q", req.JobType, JobTypeSwarmDelegation)
		}
		// SWARM-01 (51-03): the context arg threads through RunnerAdapter.Run ->
		// RunConfig.Context -> the enqueue branch's DelegationPayload, so a
		// background-delegated worker's brief still carries it.
		if got, _ := req.Payload["context"].(string); got != "shared background context" {
			t.Fatalf("enqueued row payload context = %q, want %q", got, "shared background context")
		}
	}
	if res.Preview == "" {
		t.Fatal("background enqueue must still return a model-readable result")
	}
}
