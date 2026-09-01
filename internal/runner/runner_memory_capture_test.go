package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

func captureEvent(toolName, status, callID string, meta map[string]any) *agent.Event {
	now := time.Date(2026, 9, 1, 2, 3, 4, 0, time.UTC)
	return &agent.Event{
		RequestID: uuid.MustParse("01991f4c-7a00-7000-8000-000000000001"),
		Timestamp: now,
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationEnd, ToolName: toolName, ToolCallID: callID,
			Status: status, Meta: meta,
		}},
	}
}

func TestMemoryUpsertAcceptedCapture(t *testing.T) {
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	evidence := tools.AcceptedFactEvidence{
		Subject: "operator", Predicate: "lives_in", Object: "Torino",
		Statement: "The operator lives in Torino.", SourceMemoryIDs: []string{"message-7"},
		ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "parent",
	}
	ev := captureEvent("memory__memory_upsert_fact", "ok", "call-memory", map[string]any{
		tools.MetaAcceptedFact: evidence,
	})

	got, ok := acceptedCaptureFromEvent(ctx, "conversation-a", ev)
	if !ok {
		t.Fatal("successful memory_upsert_fact evidence emitted no AcceptedCapture")
	}
	if got.SourceKind != CaptureSourceExplicitFact || got.Subject != evidence.Subject ||
		got.Predicate != evidence.Predicate || got.Object != evidence.Object || got.Statement != evidence.Statement {
		t.Fatalf("fact capture = %+v, want canonical evidence %+v", got, evidence)
	}
	if got.IdentityID != "identity-a" || got.ActorRunID != evidence.ActorRunID || got.ActorRole != "parent" ||
		got.ConversationID != "conversation-a" || got.ToolCallID != "call-memory" {
		t.Fatalf("direct provenance changed: %+v", got)
	}
	if got.IdempotencyKey == "" || got.Confidence <= 0 || !got.ObservedAt.Equal(ev.Timestamp) {
		t.Fatalf("capture lacks stable identity/confidence/time: %+v", got)
	}
}

func TestDurableArtifactAcceptedCapture(t *testing.T) {
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	for _, tc := range []struct {
		tool      string
		operation string
	}{
		{tool: "write_file", operation: "write"},
		{tool: "patch", operation: "patch"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			evidence := tools.DurableArtifactEvidence{
				Path: "/workspace/report.md", Operation: tc.operation,
				ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "worker",
			}
			ev := captureEvent(tc.tool, "ok", "call-"+tc.tool, map[string]any{
				tools.MetaDurableArtifact: evidence,
			})
			got, ok := acceptedCaptureFromEvent(ctx, "conversation-a", ev)
			if !ok {
				t.Fatal("successful durable artifact evidence emitted no AcceptedCapture")
			}
			if got.SourceKind != CaptureSourceDurableArtifact || got.ArtifactRef != evidence.Path ||
				got.ActorRole != "worker" || got.ActorRunID != evidence.ActorRunID {
				t.Fatalf("artifact capture = %+v, want evidence %+v", got, evidence)
			}
			if got.Statement != "" || got.IdempotencyKey == "" {
				t.Fatalf("artifact capture copied prose or lacks idempotency: %+v", got)
			}
		})
	}
}

func TestAcceptedCaptureProducerRejectsExcludedSources(t *testing.T) {
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	validFact := tools.AcceptedFactEvidence{
		Subject: "operator", Predicate: "uses", Object: "Aura", Statement: "The operator uses Aura.",
		ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "parent",
	}
	validArtifact := tools.DurableArtifactEvidence{
		Path: "/workspace/report.md", Operation: "write",
		ActorRunID: "01991f4c-7a00-7000-8000-000000000001", ActorRole: "parent",
	}
	cases := map[string]*agent.Event{
		"failed fact":        captureEvent("memory__memory_upsert_fact", "error", "c1", map[string]any{tools.MetaAcceptedFact: validFact}),
		"shell output":       captureEvent("shell_exec", "success", "c2", map[string]any{tools.MetaDurableArtifact: validArtifact}),
		"read output":        captureEvent("read_file", "success", "c3", map[string]any{tools.MetaDurableArtifact: validArtifact}),
		"document output":    captureEvent("document_search", "success", "c4", map[string]any{tools.MetaAcceptedFact: validFact}),
		"temporary artifact": captureEvent("write_file", "ok", "c5", map[string]any{"temporary_artifact": validArtifact}),
		"assistant prose":    {LLMResponse: &agent.LLMResponse{Content: "I infer a durable fact."}},
		"reasoning":          {LLMResponse: &agent.LLMResponse{Reasoning: "scratch-work conclusion"}},
		"discard":            {Actions: agent.Actions{DiscardStreamed: true}},
	}
	secretFact := validFact
	secretFact.Object = "sk-or-1234567890abcdefghijklmnopqrstuvwxyz"
	secretFact.Statement = "API key is " + secretFact.Object
	cases["secret"] = captureEvent("memory__memory_upsert_fact", "ok", "c6", map[string]any{tools.MetaAcceptedFact: secretFact})

	for name, ev := range cases {
		t.Run(name, func(t *testing.T) {
			if got, ok := acceptedCaptureFromEvent(ctx, "conversation-a", ev); ok {
				t.Fatalf("excluded source emitted capture: %+v", got)
			}
		})
	}
	if _, ok := acceptedCaptureFromEvent(context.Background(), "conversation-a",
		captureEvent("write_file", "ok", "c7", map[string]any{tools.MetaDurableArtifact: validArtifact})); ok {
		t.Fatal("missing authenticated identity emitted capture")
	}
	if strings.TrimSpace(validFact.Statement) == "" {
		t.Fatal("fixture must carry explicit fact text")
	}
}
