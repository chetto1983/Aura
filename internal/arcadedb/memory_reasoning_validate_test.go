package arcadedb

import (
	"strings"
	"testing"
	"time"
)

// The reasoning validators are the boundary between a provider's own words and the graph.
// Every branch below is a refusal, and a refusal that never runs is a refusal nobody has
// checked: the live tier reported the whole file uncovered on 2026-09-02.

func TestNormalizeReasoningTraceRejectsAMalformedTrace(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	cases := map[string]struct {
		mutate func(*ReasoningTrace)
		reason string
	}{
		"absent identity": {
			func(trace *ReasoningTrace) { trace.IdentityID = "" },
			"identity_id must be non-empty",
		},
		"summary carries an embedded blob": {
			func(trace *ReasoningTrace) { trace.ProviderSummary = "data:text/plain;base64,QUJD" },
			"embedded blob",
		},
		"more steps than the cap": {
			func(trace *ReasoningTrace) {
				trace.Steps = make([]ReasoningStep, reasoningMaxSteps+1)
				for index := range trace.Steps {
					trace.Steps[index] = ReasoningStep{Index: index + 1, CreatedAt: created}
				}
			},
			"steps exceeds",
		},
		"step index is not contiguous": {
			func(trace *ReasoningTrace) { trace.Steps[0].Index = 2 },
			"not contiguous",
		},
		"step has no timestamp": {
			func(trace *ReasoningTrace) { trace.Steps[0].CreatedAt = time.Time{} },
			"created_at must be set",
		},
		"step summary carries an embedded blob": {
			func(trace *ReasoningTrace) { trace.Steps[0].ProviderSummary = "data:image/png;base64,AAAA" },
			"embedded blob",
		},
		"more tools in one step than the cap": {
			func(trace *ReasoningTrace) {
				tools := make([]ReasoningToolCall, reasoningMaxToolsPerStep+1)
				for index := range tools {
					tools[index] = trace.Steps[0].ToolCalls[0]
				}
				trace.Steps[0].ToolCalls = tools
			},
			"tools exceeds",
		},
		"tool observation carries an embedded blob": {
			func(trace *ReasoningTrace) {
				trace.Steps[0].ToolCalls[0].Observation = "data:text/plain;base64,QUJD"
			},
			"embedded blob",
		},
		"two tools reuse one call_id": {
			func(trace *ReasoningTrace) {
				second := trace.Steps[0].ToolCalls[0]
				trace.Steps[0].ToolCalls = append(trace.Steps[0].ToolCalls, second)
			},
			"duplicate reasoning call_id",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			trace := validReasoningTrace()
			testCase.mutate(&trace)
			_, err := normalizeReasoningTrace(trace)
			if err == nil {
				t.Fatalf("normalizeReasoningTrace accepted %s", name)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("error does not name the refusal: %v", err)
			}
		})
	}
}

func TestNormalizeReasoningTraceCanonicalisesEvidence(t *testing.T) {
	t.Parallel()
	trace := validReasoningTrace()
	trace.ProviderSummary = "  checked\tthe\ndeployment  "
	normalized, err := normalizeReasoningTrace(trace)
	if err != nil {
		t.Fatalf("normalizeReasoningTrace: %v", err)
	}
	if normalized.ProviderSummary != "checked the deployment" {
		t.Fatalf("summary was not folded to canonical whitespace: %q", normalized.ProviderSummary)
	}
	// The normalized trace must not alias the caller's slices: a later mutation of the
	// argument cannot be allowed to reach what was already validated.
	trace.Steps[0].ToolCalls[0].EntityRefs[0] = "mutated"
	if normalized.Steps[0].ToolCalls[0].EntityRefs[0] == "mutated" {
		t.Fatal("normalized trace aliases the caller's entity refs")
	}
}

func TestValidateReasoningIdentityRejectsAnUnusableTrace(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate func(*ReasoningTrace)
		reason string
	}{
		"conversation id is not canonical": {
			func(trace *ReasoningTrace) { trace.ConversationID = " conversation-a" },
			"conversation_id must be non-empty and canonical",
		},
		"source_ref does not name a conversation turn": {
			func(trace *ReasoningTrace) { trace.SourceRef = "https://example.test/turn/7" },
			"authoritative conversation turn",
		},
		"source_ref traverses": {
			func(trace *ReasoningTrace) {
				trace.SourceRef = "postgres://aura/conversations/../../etc/passwd"
			},
			"authoritative conversation turn",
		},
		"turn_seq is not positive": {
			func(trace *ReasoningTrace) { trace.TurnSeq = 0 },
			"turn_seq must be positive",
		},
		"no created_at": {
			func(trace *ReasoningTrace) { trace.CreatedAt = time.Time{} },
			"created_at must be set",
		},
		"status is not terminal": {
			func(trace *ReasoningTrace) { trace.Status = "running" },
			"unsupported reasoning status",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			trace := validReasoningTrace()
			testCase.mutate(&trace)
			err := validateReasoningIdentity(trace)
			if err == nil {
				t.Fatalf("validateReasoningIdentity accepted %s", name)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("error does not name the refusal: %v", err)
			}
		})
	}
}

func TestValidateReasoningToolRejectsAnUnusableCall(t *testing.T) {
	t.Parallel()
	const source = "postgres://aura/conversations/conversation-a/turns/7"

	cases := map[string]struct {
		mutate func(*ReasoningToolCall)
		reason string
	}{
		"absent tool name": {
			func(tool *ReasoningToolCall) { tool.ToolName = "" },
			"tool_name must be non-empty",
		},
		"tool name carries a control character": {
			func(tool *ReasoningToolCall) { tool.ToolName = "shell\x00exec" },
			"control character",
		},
		"status is not terminal": {
			func(tool *ReasoningToolCall) { tool.Status = "running" },
			"unsupported reasoning tool status",
		},
		"negative duration": {
			func(tool *ReasoningToolCall) { tool.DurationMillis = -1 },
			"duration_ms must be non-negative",
		},
		"digest is the wrong length": {
			func(tool *ReasoningToolCall) { tool.ArgumentDigest = strings.Repeat("a", 63) },
			"must be a SHA-256 hex digest",
		},
		"digest is uppercase": {
			func(tool *ReasoningToolCall) { tool.ArgumentDigest = strings.Repeat("A", reasoningDigestRunes) },
			"lowercase SHA-256 hex",
		},
		"digest is not hex": {
			func(tool *ReasoningToolCall) { tool.ArgumentDigest = strings.Repeat("z", reasoningDigestRunes) },
			"lowercase SHA-256 hex",
		},
		"observation carries an embedded blob": {
			func(tool *ReasoningToolCall) { tool.Observation = "data:text/plain;base64,QUJD" },
			"embedded blob",
		},
		"source_ref does not match the trace": {
			func(tool *ReasoningToolCall) { tool.SourceRef = source + "/8" },
			"source_ref must match the trace source",
		},
		"more artifact refs than the cap": {
			func(tool *ReasoningToolCall) {
				refs := make([]string, reasoningMaxReferences+1)
				for index := range refs {
					refs[index] = "artifact://run-a/report.txt"
				}
				tool.ArtifactRefs = refs
			},
			"references exceeds",
		},
		"artifact ref is not an artifact URI": {
			func(tool *ReasoningToolCall) { tool.ArtifactRefs = []string{"file:///etc/passwd"} },
			"allowlisted artifact URI",
		},
		"artifact ref traverses": {
			func(tool *ReasoningToolCall) { tool.ArtifactRefs = []string{"artifact://run-a/../../etc/passwd"} },
			"allowlisted artifact URI",
		},
		"entity ref is empty": {
			func(tool *ReasoningToolCall) { tool.EntityRefs = []string{""} },
			"entity_ref must be non-empty",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool := validReasoningTrace().Steps[0].ToolCalls[0]
			testCase.mutate(&tool)
			err := validateReasoningTool(source, tool)
			if err == nil {
				t.Fatalf("validateReasoningTool accepted %s", name)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("error does not name the refusal: %v", err)
			}
		})
	}
}

func TestValidateReasoningTextEnforcesItsRuneCeiling(t *testing.T) {
	t.Parallel()
	if err := validateReasoningText("entity_ref", strings.Repeat("x", 8), 4); err == nil {
		t.Fatal("validateReasoningText accepted a value over its rune limit")
	}
	if err := validateReasoningText("entity_ref", "deployment-a", 64); err != nil {
		t.Fatalf("validateReasoningText rejected a canonical value: %v", err)
	}
}
