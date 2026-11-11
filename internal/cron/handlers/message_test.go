package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

const messageOwner = "10000000-0000-4000-8000-000000000001"

// fakeMessageDrafts serves one draft per task and records every transition the handler asks for.
type fakeMessageDrafts struct {
	mu       sync.Mutex
	draft    messagedrafts.Draft
	reread   *messagedrafts.Draft // what ForTask returns after a lost claim
	claims   int
	claimOK  int // how many claims succeed
	expires  int
	outcomes []string
	forTasks int
}

func (f *fakeMessageDrafts) ForTask(context.Context, string, string) (messagedrafts.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forTasks++
	if f.forTasks > 1 && f.reread != nil {
		return *f.reread, nil
	}
	if f.draft.ID == "" {
		return messagedrafts.Draft{}, messagedrafts.ErrUnavailable
	}
	return f.draft, nil
}

func (f *fakeMessageDrafts) ClaimScheduled(context.Context, string, string) (messagedrafts.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	if f.claims > f.claimOK {
		return messagedrafts.Draft{}, messagedrafts.ErrUnavailable
	}
	claimed := f.draft
	claimed.Status = messagedrafts.StatusDispatching
	return claimed, nil
}

func (f *fakeMessageDrafts) ExpireScheduled(context.Context, string, string) (messagedrafts.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expires++
	return f.draft, nil
}

func (f *fakeMessageDrafts) MarkOutcome(_ context.Context, _, _ string, status messagedrafts.Status, code string) (messagedrafts.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcomes = append(f.outcomes, string(status)+"/"+code)
	return f.draft, nil
}

// sendSpy is the mounted WhatsApp send, with the operation metadata of a mutating MCP tool.
type sendSpy struct {
	mu    sync.Mutex
	calls int
	args  json.RawMessage
	err   error
}

func (s *sendSpy) Spec() tools.Spec {
	return tools.Spec{
		Name: "wa__send_message", TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_message",
		Mutating: true, OperationScope: tools.OperationScopeMCP,
		OperationNormalizer: tools.OperationNormalizerCanonical, ReplayPolicy: tools.ReplayToolResult,
	}
}

func (s *sendSpy) Execute(_ context.Context, args json.RawMessage) (tools.ToolResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.args = args
	return tools.ToolResult{}, s.err
}

// approvedDraft is a reviewed WhatsApp draft, its effective arguments fingerprinted as the store
// fixes them.
func approvedDraft(t *testing.T) messagedrafts.Draft {
	t.Helper()
	target := messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"}
	original := json.RawMessage(`{"recipient":"10000000000","message":"Ciao"}`)
	effective, err := messagedrafts.MergeApprovedArgs(target, original, nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := json.Marshal(struct {
		Recipe string          `json:"recipe"`
		Tool   string          `json:"tool"`
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}{Recipe: target.Recipe, Tool: target.Tool, Args: effective})
	sum := sha256.Sum256(bound)
	return messagedrafts.Draft{
		ID: "22222222-2222-4222-8222-222222222222", IdentityID: messageOwner, ConversationID: "conv", ToolCallID: "call-1",
		Target: target, RegisteredToolName: "wa__send_message", Status: messagedrafts.StatusApproved,
		OriginalArgs: original, EffectiveArgs: effective, EffectiveFingerprint: hex.EncodeToString(sum[:]),
		SendAt: time.Now(), ScheduledTaskID: "task-1",
	}
}

func messageHandler(drafts *fakeMessageDrafts, spy *sendSpy) MessageHandler {
	reg := tools.NewRegistry()
	if spy != nil {
		reg.Register(spy)
	}
	rome, _ := time.LoadLocation("Europe/Rome")
	return MessageHandler{Drafts: drafts, Tools: reg, Location: rome}
}

// scheduledRunCtx is the context the dispatcher gives a run: the owner, and the scheduler's
// operation the send derives its own from.
func scheduledRunCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), messageOwner), idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: messageOwner, Scope: idempotency.ScopeSchedulerRun, Key: "run-1"},
		Fingerprint: sha256.Sum256([]byte("run-1")),
		ClaimToken:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestAnApprovedMessageIsSentOnceAtItsTime(t *testing.T) {
	draft := approvedDraft(t)
	drafts := &fakeMessageDrafts{draft: draft, claimOK: 1}
	spy := &sendSpy{}
	summary, err := messageHandler(drafts, spy).Run(scheduledRunCtx(t), Job{TaskID: "task-1"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if spy.calls != 1 || string(spy.args) != string(draft.EffectiveArgs) {
		t.Fatalf("sent %d times with %s", spy.calls, spy.args)
	}
	if len(drafts.outcomes) != 1 || drafts.outcomes[0] != "sent/ok" {
		t.Fatalf("outcomes = %v", drafts.outcomes)
	}
	if !strings.HasPrefix(summary, "Message sent to 10000000000 at ") || len(summary) != len("Message sent to 10000000000 at 08:25.") {
		t.Fatalf("summary = %q", summary)
	}
}

// Under the scheduler's own operation, the send derives the child operation it needs.
func TestTheFireTimeSendRunsUnderTheScheduledOperation(t *testing.T) {
	draft := approvedDraft(t)
	draft.Status = messagedrafts.StatusDispatching
	spy := &sendSpy{}
	if _, err := agent.ExecuteReviewedMessage(scheduledRunCtx(t), spy, nil, draft, "", 0); err != nil || spy.calls != 1 {
		t.Fatalf("send = %v, %d calls", err, spy.calls)
	}
}

func TestASecondFireNeverSendsAgain(t *testing.T) {
	for _, status := range []messagedrafts.Status{messagedrafts.StatusDispatching, messagedrafts.StatusSent} {
		draft := approvedDraft(t)
		draft.Status, draft.DispatchStartedAt = status, time.Now()
		spy := &sendSpy{}
		summary, err := messageHandler(&fakeMessageDrafts{draft: draft}, spy).Run(scheduledRunCtx(t), Job{TaskID: "task-1"})
		if err != nil || spy.calls != 0 || !strings.Contains(summary, "was already handled") {
			t.Fatalf("%s: %q, %v, %d sends", status, summary, err, spy.calls)
		}
	}
	draft := approvedDraft(t)
	handled := draft
	handled.Status = messagedrafts.StatusDispatching
	handled.DispatchStartedAt = time.Now()
	drafts := &fakeMessageDrafts{draft: draft, claimOK: 1, reread: &handled}
	spy := &sendSpy{}
	h := messageHandler(drafts, spy)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, _ = h.Run(scheduledRunCtx(t), Job{TaskID: "task-1"}) })
	}
	wg.Wait()
	if spy.calls != 1 {
		t.Fatalf("two concurrent fires sent %d times", spy.calls)
	}
}

func TestAPendingDraftIsNeverSent(t *testing.T) {
	draft := approvedDraft(t)
	draft.Status = messagedrafts.StatusPending
	drafts := &fakeMessageDrafts{draft: draft}
	spy := &sendSpy{}
	summary, err := messageHandler(drafts, spy).Run(scheduledRunCtx(t), Job{TaskID: "task-1"})
	if err != nil || drafts.expires != 1 || spy.calls != 0 || !strings.HasSuffix(summary, "it was not approved in time.") {
		t.Fatalf("%q, %v, expires %d, sends %d", summary, err, drafts.expires, spy.calls)
	}
}

func TestAMissingChannelLeavesTheDraftApproved(t *testing.T) {
	drafts := &fakeMessageDrafts{draft: approvedDraft(t), claimOK: 1}
	_, err := messageHandler(drafts, nil).Run(scheduledRunCtx(t), Job{TaskID: "task-1"})
	if err == nil || !strings.Contains(err.Error(), "its channel is not connected") || drafts.claims != 0 {
		t.Fatalf("err = %v, claims %d", err, drafts.claims)
	}
}

func TestAStaleDispatchIsReportedUncertain(t *testing.T) {
	draft := approvedDraft(t)
	draft.Status, draft.DispatchStartedAt = messagedrafts.StatusDispatching, time.Now().Add(-31*time.Minute)
	drafts := &fakeMessageDrafts{draft: draft}
	spy := &sendSpy{}
	_, err := messageHandler(drafts, spy).Run(scheduledRunCtx(t), Job{TaskID: "task-1"})
	if err == nil || !strings.Contains(err.Error(), "delivery is uncertain") || spy.calls != 0 ||
		len(drafts.outcomes) != 1 || drafts.outcomes[0] != "uncertain/stale_dispatch" {
		t.Fatalf("err = %v, sends %d, outcomes %v", err, spy.calls, drafts.outcomes)
	}
}

// Decision 39: a cancel that wins the race reads declined, not expired.
func TestALostClaimReadsTheDraftAgain(t *testing.T) {
	declined := approvedDraft(t)
	declined.Status = messagedrafts.StatusDeclined
	drafts := &fakeMessageDrafts{draft: approvedDraft(t), reread: &declined}
	spy := &sendSpy{}
	summary, err := messageHandler(drafts, spy).Run(scheduledRunCtx(t), Job{TaskID: "task-1"})
	if err != nil || spy.calls != 0 || !strings.HasSuffix(summary, "it was declined.") {
		t.Fatalf("%q, %v, sends %d", summary, err, spy.calls)
	}
}

func TestAFailedSendIsAFailedRun(t *testing.T) {
	drafts := &fakeMessageDrafts{draft: approvedDraft(t), claimOK: 1}
	spy := &sendSpy{err: agent.ErrToolNotExecuted}
	_, err := messageHandler(drafts, spy).Run(scheduledRunCtx(t), Job{TaskID: "task-1"})
	if err == nil || !strings.Contains(err.Error(), "not sent") || len(drafts.outcomes) != 1 || drafts.outcomes[0] != "failed/no_effect" {
		t.Fatalf("err = %v, outcomes %v", err, drafts.outcomes)
	}
}
