package conversations

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/google/uuid"
)

var dumpT0 = time.Date(2026, 9, 24, 8, 52, 3, 0, time.UTC)

func dumpToolCalls(t *testing.T, calls ...llm.ToolCall) []byte {
	t.Helper()
	raw, err := json.Marshal(calls)
	if err != nil {
		t.Fatalf("marshal tool calls: %v", err)
	}
	return raw
}

func dumpCall(id, name, args string) llm.ToolCall {
	c := llm.ToolCall{ID: id, Type: "function"}
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

func linearTurn(seq int, role, content string) DumpTurn {
	return DumpTurn{
		Seq: seq, Role: role, Content: content, BranchID: uuid.Nil.String(),
		ParentSeq: seq - 1, CreatedAt: dumpT0.Add(time.Duration(seq) * time.Second),
	}
}

func dumpConversation() Conversation {
	return Conversation{
		ID: "01a0c42c-2b82-750d-9508-4db9cc44fbe8", Title: "Pranzo", TitleSet: true,
		Status: StatusActive, Model: "z-ai/glm-5.3-flash", CreatedAt: "2026-09-24T08:52:03Z",
		TotalInputTokens: 1500, TotalOutputTokens: 80, TotalCachedTokens: 900, TotalCostUSD: 0.0123,
	}
}

// TestDumpMarkdownRendersWhatTheSnapshotDropped pins the 2026-09-24 regression: every
// section the redacted export left as an empty fence must now carry its call and result.
func TestDumpMarkdownRendersWhatTheSnapshotDropped(t *testing.T) {
	t.Parallel()
	callTurn := linearTurn(3, llm.RoleAssistant, "")
	callTurn.ToolCalls = dumpToolCalls(t, dumpCall("call_1", "calendar__calendar", `{"action":"create","title":"Vado a pranzo"}`))
	callTurn.Reasoning = "The user wants an event at noon."
	callTurn.ReasoningDurationMS = 3200
	callTurn.InputTokens, callTurn.OutputTokens, callTurn.CachedTokens, callTurn.ContextTokens = 1200, 40, 900, 20000

	result := linearTurn(4, llm.RoleTool, `{"id":"evt-9","status":"created"}`)
	result.ToolCallID = "call_1"
	orphan := linearTurn(5, llm.RoleTool, "late result after the run was interrupted")
	orphan.ToolCallID = "call_lost"

	d := Dump{Turns: []DumpTurn{
		linearTurn(1, llm.RoleSystem, "You are Aura."),
		linearTurn(2, llm.RoleUser, "Mettimi un appuntamento oggi alle 12:00"),
		callTurn, result, orphan,
		linearTurn(6, llm.RoleAssistant, "Fatto: appuntamento creato."),
	}}
	md := string(d.Markdown(dumpConversation(), nil, dumpT0.Add(time.Hour)))

	for _, want := range []string{
		"# Pranzo\n",
		"- conversation: `01a0c42c-2b82-750d-9508-4db9cc44fbe8`",
		"- model: `z-ai/glm-5.3-flash`",
		"- exported: 2026-09-24T09:52:03Z",
		"- tokens: input 1500 · output 80 · cached 900 · cost $0.0123",
		"## #1 · system · 2026-09-24T08:52:04Z",
		"You are Aura.",
		"## #3 · assistant · 2026-09-24T08:52:06Z · tokens in 1200 · out 40 · cached 900 · context 20000",
		"**reasoning** (3.2 s)",
		"The user wants an event at noon.",
		"**tool call** `calendar__calendar` · id `call_1`",
		"```json\n{\n  \"action\": \"create\",\n  \"title\": \"Vado a pranzo\"\n}\n```",
		"_(empty content)_",
		"## #4 · tool · 2026-09-24T08:52:07Z · result of `calendar__calendar` · id `call_1`",
		`{"id":"evt-9","status":"created"}`,
		"## #5 · tool · 2026-09-24T08:52:08Z · result of unknown call `call_lost`",
		"late result after the run was interrupted",
		"Fatto: appuntamento creato.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("dump is missing %q\n--- dump ---\n%s", want, md)
		}
	}
	if got := strings.Count(md, "\n## #"); got != 6 {
		t.Errorf("rendered %d turn sections, want one per persisted turn (6)", got)
	}
	if strings.Contains(md, "## #2 · user · 2026-09-24T08:52:05Z · tokens") {
		t.Error("a turn without usage must not render a zero token line")
	}
}

func TestDumpMarkdownFenceOutlivesEmbeddedBackticks(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"before ```embedded``` after", "a ````four```` run", "ends with a backtick`"} {
		d := Dump{Turns: []DumpTurn{linearTurn(1, llm.RoleAssistant, text)}}
		md := string(d.Markdown(dumpConversation(), nil, dumpT0))
		fence := strings.Repeat("`", max(longestBacktickRun(text)+1, 3))
		if !strings.Contains(md, fence+"\n"+text+"\n"+fence+"\n") {
			t.Errorf("content %q is not wrapped whole in a %d-backtick fence:\n%s", text, len(fence), md)
		}
	}
}

func TestDumpMarkdownKeepsMalformedToolPayloadsVerbatim(t *testing.T) {
	t.Parallel()
	notJSON := linearTurn(1, llm.RoleAssistant, "")
	notJSON.ToolCalls = dumpToolCalls(t, dumpCall("c1", "shell_exec", "command=ls <arg_key>"), dumpCall("c2", "noop", ""))
	undecodable := linearTurn(2, llm.RoleAssistant, "")
	undecodable.ToolCalls = []byte(`{"not":"a list"}`)
	missingID := linearTurn(3, llm.RoleTool, "result")

	d := Dump{Turns: []DumpTurn{notJSON, undecodable, missingID}}
	md := string(d.Markdown(dumpConversation(), nil, dumpT0))
	for _, want := range []string{
		"```\ncommand=ls <arg_key>\n```",
		"**tool call** `noop` · id `c2`\n\n_(no arguments)_",
		"**tool calls** (undecodable)\n\n```\n{\"not\":\"a list\"}\n```",
		"· result without a call id",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("dump is missing %q\n--- dump ---\n%s", want, md)
		}
	}
}

func TestDumpMarkdownNamesOnlyNonLinearBranchPointers(t *testing.T) {
	t.Parallel()
	fork := uuid.MustParse("01a0c42c-0000-7000-8000-000000000001").String()
	forked := linearTurn(4, llm.RoleUser, "edited question")
	forked.BranchID, forked.ParentSeq = fork, 2
	detached := linearTurn(5, llm.RoleUser, "pre-backfill row")
	detached.ParentSeq = 0
	withMeta := linearTurn(6, llm.RoleUser, "see attachment")
	withMeta.AttachmentIDs = []string{"a-1", "a-2"}
	withMeta.DeliveryKey = "job-7:terminal"

	d := Dump{Turns: []DumpTurn{linearTurn(1, llm.RoleUser, "root"), forked, detached, withMeta}}
	md := string(d.Markdown(dumpConversation(), nil, dumpT0))
	for _, want := range []string{
		"## #4 · user · 2026-09-24T08:52:07Z · branch `" + fork + "` · parent #2\n",
		"## #5 · user · 2026-09-24T08:52:08Z · no parent\n",
		"delivery key: `job-7:terminal`",
		"attachments: `a-1`, `a-2`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("dump is missing %q\n--- dump ---\n%s", want, md)
		}
	}
	if !strings.Contains(md, "## #1 · user · 2026-09-24T08:52:04Z\n") {
		t.Errorf("a canonical root turn must carry no branch or parent suffix:\n%s", md)
	}
}

func TestDumpMarkdownAppendsCompactionsAndAssets(t *testing.T) {
	t.Parallel()
	d := Dump{Compactions: []DumpCompaction{{
		BranchID: uuid.Nil.String(), CoversThroughSeq: 40, SourceTurns: 38,
		Summary: "User set a noon reminder.", Model: "gemma4:31b-cloud", UpdatedAt: dumpT0,
	}}}
	assets := []DumpAsset{{ID: "as-1", FileName: "Riepilogo_Fido_Clienti.XLS", MIMEType: "application/vnd.ms-excel",
		SizeBytes: 5734, SourceKind: "agent", Status: "ready"}}
	md := string(d.Markdown(dumpConversation(), assets, dumpT0))
	for _, want := range []string{
		"## Compactions\n\n### branch `" + uuid.Nil.String() + "` · through #40 · 38 source turns · model `gemma4:31b-cloud` · updated 2026-09-24T08:52:03Z",
		"User set a noon reminder.",
		"## Assets\n\n- `Riepilogo_Fido_Clienti.XLS` · application/vnd.ms-excel · 5734 bytes · source agent · status ready · id `as-1`\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("dump is missing %q\n--- dump ---\n%s", want, md)
		}
	}

	empty := string(Dump{}.Markdown(Conversation{CreatedAt: "2026-09-24T08:52:03Z"}, nil, dumpT0))
	if !strings.HasPrefix(empty, "# (untitled 2026-09-24T08:52:03Z)\n") || strings.Contains(empty, "\n## ") {
		t.Errorf("an empty conversation must render only its header:\n%s", empty)
	}
}

// TestDumpMarkdownMasksConfiguredSecrets is serial: it swaps the process-wide redactor.
func TestDumpMarkdownMasksConfiguredSecrets(t *testing.T) {
	const configured = "sk-live-configured-0123456789"
	secret.ConfigureExactRedactor([]string{"AURA_DB_TOKEN=" + configured})
	t.Cleanup(func() { secret.ConfigureExactRedactor(os.Environ()) })

	call := linearTurn(1, llm.RoleAssistant, "")
	call.ToolCalls = dumpToolCalls(t, dumpCall("c1", "shell_exec", `{"command":"curl -H 'Authorization: `+configured+`'"}`))
	call.Reasoning = "use " + configured
	md := string(Dump{Turns: []DumpTurn{call}}.Markdown(dumpConversation(), nil, dumpT0))
	if strings.Contains(md, configured) {
		t.Fatalf("dump leaked a configured secret:\n%s", md)
	}
	if strings.Count(md, secret.ConfiguredValuePlaceholder) != 2 {
		t.Errorf("want the secret masked in both the arguments and the reasoning:\n%s", md)
	}
}
