package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCallFingerprintIgnoresKeyOrder(t *testing.T) {
	t.Parallel()
	a := CallFingerprint(json.RawMessage(`{"action":"delete","name":"obsolete-skill"}`))
	b := CallFingerprint(json.RawMessage(`{ "name": "obsolete-skill", "action": "delete" }`))
	if a == "" || a != b {
		t.Fatalf("fingerprints %q and %q differ for the same call", a, b)
	}
	if other := CallFingerprint(json.RawMessage(`{"action":"delete","name":"another-skill"}`)); other == a {
		t.Fatal("a call with a different argument shares the fingerprint")
	}
	if len(a) != 64 {
		t.Fatalf("fingerprint %q is not a hex sha256", a)
	}
}

func TestApprovedCallForMatchesSessionAndCallID(t *testing.T) {
	t.Parallel()
	const conv = "11111111-1111-1111-1111-111111111111"
	want := ApprovedCall{ConversationID: conv, ToolCallID: "call-2", ToolName: "skill_manage", OperatorID: "op-1", Scope: "once"}
	calls := []ApprovedCall{
		{ConversationID: "22222222-2222-2222-2222-222222222222", ToolCallID: "call-2", ToolName: "skill_manage"},
		{ConversationID: conv, ToolCallID: "call-1", ToolName: "skill_manage"},
		want,
	}
	ctx := WithApprovedCalls(context.Background(), calls)

	got, ok := ApprovedCallFor(WithToolCallContext(ctx, conv, "call-2", "", 0))
	if !ok || got.ToolCallID != want.ToolCallID || got.ConversationID != conv || got.OperatorID != "op-1" {
		t.Fatalf("ApprovedCallFor = %+v, %v; want the call of this conversation and call id", got, ok)
	}
	if _, ok := ApprovedCallFor(WithToolCallContext(ctx, conv, "call-3", "", 0)); ok {
		t.Fatal("an unlisted call id was authorized")
	}
	if _, ok := ApprovedCallFor(ctx); ok {
		t.Fatal("a dispatch without a tool-call context was authorized")
	}
	if _, ok := ApprovedCallFor(WithToolCallContext(context.Background(), conv, "call-2", "", 0)); ok {
		t.Fatal("a context carrying no approved calls was authorized")
	}
	empty := WithApprovedCalls(context.Background(), []ApprovedCall{{}})
	if _, ok := ApprovedCallFor(WithToolCallContext(empty, "", "", "", 0)); ok {
		t.Fatal("an empty session and call id matched an empty record")
	}
}

func TestAuthorizesRequiresNameAndFingerprint(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"delete","name":"obsolete-skill"}`)
	call := ApprovedCall{ToolName: "skill_manage", Arguments: args, Fingerprint: CallFingerprint(args)}

	if !call.Authorizes("skill_manage", json.RawMessage(`{"name":"obsolete-skill","action":"delete"}`)) {
		t.Fatal("the approved call, re-encoded, is not authorized")
	}
	if call.Authorizes("memory_forget", args) {
		t.Fatal("another tool with the same arguments is authorized")
	}
	if call.Authorizes("skill_manage", json.RawMessage(`{"action":"delete","name":"another-skill"}`)) {
		t.Fatal("other arguments are authorized")
	}
	if (ApprovedCall{ToolName: "skill_manage", Arguments: args}).Authorizes("skill_manage", args) {
		t.Fatal("a record with no fingerprint authorizes a call")
	}
	if (ApprovedCall{Fingerprint: CallFingerprint(args)}).Authorizes("", args) {
		t.Fatal("a record with no tool name authorizes a nameless call")
	}
}
