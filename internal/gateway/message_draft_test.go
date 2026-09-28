package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
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
