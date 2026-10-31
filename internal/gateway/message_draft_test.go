package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

func TestMessageDraftGatewayRequiresReviewInEveryProfile(t *testing.T) {
	spec := reviewedEmailSpec()
	args := reviewedEmailArgs()
	for _, profile := range []config.RuntimeProfile{config.ProfileDev, config.ProfileLocalTrusted, config.ProfileSingleUserHardened} {
		store := &fakeStore{}
		gateway := New(profile, store)
		verdict, err := gateway.Decide(context.Background(), spec, args, testKey())
		if err != nil || verdict.Decision != Deny || len(store.reserves()) != 0 {
			t.Fatalf("%s: unreviewed send = (%+v, %v), reserves=%d", profile, verdict, err, len(store.reserves()))
		}
	}
	var absent *Gateway
	verdict, err := absent.Decide(context.Background(), spec, args, testKey())
	if err != nil || verdict.Decision != Deny {
		t.Fatalf("nil gateway allowed an unreviewed send: (%+v, %v)", verdict, err)
	}
}

// A headless job has no Runner to write its own row, so the gateway's deny fact is the only
// trace of a refused send: measured missing on 2026-10-31, when an injected mail had a
// scheduled run try to write to its sender
// (docs/superpowers/verification/2026-10-31-briefing-model-in-the-loop.md).
func TestARefusedUnreviewedSendIsRecordedOnAStrictProfile(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	verdict, err := New(config.ProfileSingleUserHardened, store).Decide(t.Context(), reviewedEmailSpec(), reviewedEmailArgs(), testKey())
	if err != nil || verdict.Decision != Deny || !verdict.Refusal {
		t.Fatalf("unreviewed send = (%+v, %v), want a refusal", verdict, err)
	}
	facts := store.calls()
	if len(facts) != 1 {
		t.Fatalf("deny facts = %+v, want exactly one", facts)
	}
	fact := facts[0]
	key := testKey()
	if fact.Event != "end" || fact.Status != "error" || fact.Error != verdict.Reason ||
		fact.ConversationID != key.ConversationID || fact.RequestID != key.RequestID || fact.ToolCallID != key.ToolCallID ||
		fact.ToolName != reviewedEmailSpec().Name {
		t.Errorf("deny fact = %+v, want the call's end row carrying the refusal", fact)
	}
	if fact.Meta["gateway_verdict"] != string(Deny) || fact.Meta["reason"] != "message_review" || fact.Meta["degraded_deny"] != nil {
		t.Errorf("deny fact meta = %v, want gateway_verdict=deny reason=message_review", fact.Meta)
	}
	if fact.Meta["gateway_tier"] != string(classify(reviewedEmailSpec(), reviewedEmailArgs())) {
		t.Errorf("deny fact tier = %v, want the call's own tier", fact.Meta["gateway_tier"])
	}
}

// Non-strict profiles promise no store write (Decide's doc), so their refusal is a verdict only.
func TestARefusedUnreviewedSendWritesNothingOffStrict(t *testing.T) {
	t.Parallel()
	for _, profile := range []config.RuntimeProfile{config.ProfileDev, config.ProfileLocalTrusted} {
		store := &fakeStore{}
		verdict, err := New(profile, store).Decide(t.Context(), reviewedEmailSpec(), reviewedEmailArgs(), testKey())
		if err != nil || verdict.Decision != Deny || !verdict.Refusal || len(store.calls()) != 0 {
			t.Fatalf("%s: unreviewed send = (%+v, %v), facts=%d; want a refusal and no row", profile, verdict, err, len(store.calls()))
		}
	}
}

func TestMessageDraftGatewayChecksExactClaimBeforeReserving(t *testing.T) {
	spec := reviewedEmailSpec()
	args := reviewedEmailArgs()
	key := testKey()
	claim := reviewedEmailClaim(t, key, args)
	store := &fakeStore{}
	gateway := New(config.ProfileSingleUserHardened, store)
	ownerCtx := identityctx.WithIdentityID(t.Context(), claim.IdentityID)

	bad := []struct {
		name  string
		ctx   context.Context
		spec  tools.Spec
		args  json.RawMessage
		key   ReservationKey
		claim messagedrafts.Draft
	}{
		{name: "other owner", ctx: identityctx.WithIdentityID(t.Context(), "other"), spec: spec, args: args, key: key, claim: claim},
		{name: "other registered name", ctx: ownerCtx, spec: tools.Spec{Name: "other__calendar", TrustedRecipeSource: spec.TrustedRecipeSource, TrustedRecipeTool: spec.TrustedRecipeTool}, args: args, key: key, claim: claim},
		{name: "edited destination", ctx: ownerCtx, spec: spec, args: json.RawMessage(`{"action":"send_email","to":["other@example.test"],"subject":"Hello","body":"Reviewed"}`), key: key, claim: claim},
		{name: "other request", ctx: ownerCtx, spec: spec, args: args, key: ReservationKey{ConversationID: key.ConversationID, ToolCallID: key.ToolCallID, RequestID: "other"}, claim: claim},
		{name: "other call", ctx: ownerCtx, spec: spec, args: args, key: ReservationKey{ConversationID: key.ConversationID, ToolCallID: "other", RequestID: key.RequestID}, claim: claim},
		{name: "other conversation", ctx: ownerCtx, spec: spec, args: args, key: ReservationKey{ConversationID: "33333333-3333-3333-3333-333333333333", ToolCallID: key.ToolCallID, RequestID: key.RequestID}, claim: claim},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			verdict, err := gateway.Decide(WithReviewedMessageDraft(tc.ctx, tc.claim), tc.spec, tc.args, tc.key)
			if err != nil || verdict.Decision != Deny || len(store.reserves()) != 0 {
				t.Fatalf("mismatched review = (%+v, %v), reserves=%d", verdict, err, len(store.reserves()))
			}
		})
	}
	verdict, err := gateway.Decide(WithReviewedMessageDraft(ownerCtx, claim), spec, args, key)
	if err != nil || verdict.Decision != Allow || len(store.reserves()) != 1 {
		t.Fatalf("exact reviewed send = (%+v, %v), reserves=%d", verdict, err, len(store.reserves()))
	}
}

// A review covers one outbound send. A claim riding a call that sends nothing must not
// carry it past the gate either.
func TestAReviewClaimOnACallThatSendsNothingIsRefused(t *testing.T) {
	t.Parallel()
	key := testKey()
	claim := reviewedEmailClaim(t, key, reviewedEmailArgs())
	ctx := WithReviewedMessageDraft(identityctx.WithIdentityID(t.Context(), claim.IdentityID), claim)
	store := &fakeStore{}
	verdict, err := New(config.ProfileSingleUserHardened, store).Decide(ctx, tools.Spec{Name: "board", Mutating: true}, json.RawMessage(`{"action":"list"}`), key)
	if err != nil || verdict.Decision != Deny || !verdict.Refusal || len(store.reserves()) != 0 {
		t.Fatalf("claim on a board call = (%+v, %v), reserves=%d; want a refusal", verdict, err, len(store.reserves()))
	}
}

// The exact reviewed send goes out in every profile; only a strict one reserves it, at the
// Destructive tier whatever the spec said.
func TestAnExactReviewedSendIsAllowedInEveryProfile(t *testing.T) {
	t.Parallel()
	key := testKey()
	args := reviewedEmailArgs()
	claim := reviewedEmailClaim(t, key, args)
	ctx := WithReviewedMessageDraft(identityctx.WithIdentityID(t.Context(), claim.IdentityID), claim)

	var absent *Gateway
	if verdict, err := absent.Decide(ctx, reviewedEmailSpec(), args, key); err != nil || verdict.Decision != Allow {
		t.Errorf("nil gateway: reviewed send = (%+v, %v), want allow", verdict, err)
	}
	for _, profile := range []config.RuntimeProfile{config.ProfileDev, config.ProfileLocalTrusted} {
		store := &fakeStore{}
		verdict, err := New(profile, store).Decide(ctx, reviewedEmailSpec(), args, key)
		if err != nil || verdict.Decision != Allow || len(store.reserves()) != 0 || len(store.calls()) != 0 {
			t.Errorf("%s: reviewed send = (%+v, %v), reserves=%d facts=%d; want allow and no row", profile, verdict, err, len(store.reserves()), len(store.calls()))
		}
	}
	store := &fakeStore{}
	verdict, err := New(config.ProfileSingleUserHardened, store).Decide(ctx, reviewedEmailSpec(), args, key)
	if err != nil || verdict.Decision != Allow || len(store.reserves()) != 1 {
		t.Fatalf("strict: reviewed send = (%+v, %v), reserves=%d; want allow and one reservation", verdict, err, len(store.reserves()))
	}
	if tier := store.reserves()[0].Meta["gateway_tier"]; tier != "destructive" {
		t.Errorf("reserved at tier %v, want destructive", tier)
	}
}

// Under a strict profile a reviewed send still enters the operation registry: a refusal there
// stops it before the reservation, and an acquired claim travels with the verdict.
func TestAReviewedSendGoesThroughTheOperationRegistry(t *testing.T) {
	t.Parallel()
	key := testKey()
	spec := reviewedEmailSpec()
	spec.OperationScope = tools.OperationScopeAgent
	spec.OperationNormalizer = tools.OperationNormalizerCanonical
	spec.ReplayPolicy = tools.ReplayToolResult
	args := reviewedEmailArgs()
	claim := reviewedEmailClaim(t, key, args)
	owner := identityctx.WithIdentityID(t.Context(), claim.IdentityID)

	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	g.SetOperationRegistry(&fakeOperationRegistry{})
	verdict, err := g.Decide(WithReviewedMessageDraft(owner, claim), spec, args, key)
	if err != nil || verdict.Decision != Deny || verdict.Reason != "operation context missing" || len(store.reserves()) != 0 {
		t.Fatalf("no operation context = (%+v, %v), reserves=%d; want the registry's deny", verdict, err, len(store.reserves()))
	}

	// The bridge may hand over the send as a plain call; the reviewed path makes it a
	// mutation itself, and the operation's fingerprint is taken over that.
	mutating := spec
	spec.Mutating = false
	fingerprint, err := tools.OperationFingerprint(mutating, args)
	if err != nil {
		t.Fatal(err)
	}
	opCtx, err := idempotency.WithOperation(owner, idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: claim.IdentityID, Scope: idempotency.ScopeAgentTool, Key: "send-1"},
		Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	g.SetOperationRegistry(&fakeOperationRegistry{decision: idempotency.BeginDecision{Decision: idempotency.DecisionAcquired, ClaimToken: 7}})
	verdict, err = g.Decide(WithReviewedMessageDraft(opCtx, claim), spec, args, key)
	if err != nil || verdict.Decision != Allow || verdict.OperationDecision != idempotency.DecisionAcquired || verdict.OperationClaimToken != 7 {
		t.Fatalf("acquired operation = (%+v, %v), want allow carrying the claim", verdict, err)
	}
}

func reviewedEmailSpec() tools.Spec {
	return tools.Spec{Name: "namespace_that_is_very_long_and_needs_truncate__c_0123456789ab", TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar", Mutating: true}
}

func reviewedEmailArgs() json.RawMessage {
	return json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","body":"Reviewed"}`)
}

func reviewedEmailClaim(t *testing.T, key ReservationKey, args json.RawMessage) messagedrafts.Draft {
	t.Helper()
	var canonical json.RawMessage
	if err := json.Unmarshal(args, &canonical); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &fields); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	target := messagedrafts.Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}
	bound, err := json.Marshal(struct {
		Recipe string          `json:"recipe"`
		Tool   string          `json:"tool"`
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}{Recipe: target.Recipe, Tool: target.Tool, Action: target.Action, Args: canonical})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bound)
	return messagedrafts.Draft{
		ID: key.RequestID, IdentityID: "11111111-1111-4111-8111-111111111111", ConversationID: key.ConversationID,
		ToolCallID: key.ToolCallID, RegisteredToolName: reviewedEmailSpec().Name, Target: target,
		Status: messagedrafts.StatusDispatching, EffectiveArgs: canonical, EffectiveFingerprint: hex.EncodeToString(sum[:]),
	}
}
