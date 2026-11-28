package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// lookbackPositions is the prompt-caching docs' read window: a breakpoint checks itself and
// the 19 positions before it for an entry an earlier request wrote.
const lookbackPositions = 20

// wireBlock is one content block of a captured request, with its cache_control removed so
// two requests compare on what the cache hashes.
type wireBlock struct {
	message int
	role    string
	kind    string
	bytes   string
	cached  bool
}

type wireRequest struct {
	tools, system string
	blocks        []wireBlock
}

func parseWire(t *testing.T, body []byte) wireRequest {
	t.Helper()
	var req struct {
		Tools    json.RawMessage `json:"tools"`
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("request body: %v", err)
	}
	out := wireRequest{tools: string(req.Tools), system: string(req.System)}
	for i, message := range req.Messages {
		for _, raw := range message.Content {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("block: %v", err)
			}
			_, cached := fields["cache_control"]
			delete(fields, "cache_control")
			var kind string
			_ = json.Unmarshal(fields["type"], &kind)
			canonical, _ := json.Marshal(fields)
			out.blocks = append(out.blocks, wireBlock{message: i, role: message.Role, kind: kind, bytes: string(canonical), cached: cached})
		}
	}
	return out
}

// positions counts the lookback positions from block from to block to, both included: a
// run of tool_use blocks, or of tool_result blocks, in one message is one position.
func positions(blocks []wireBlock, from, to int) int {
	n := 0
	for i := from; i <= to; i++ {
		prev := blocks[max(i-1, 0)]
		run := i > from && prev.message == blocks[i].message && prev.kind == blocks[i].kind &&
			(blocks[i].kind == "tool_use" || blocks[i].kind == "tool_result")
		if !run {
			n++
		}
	}
	return n
}

// firstTurnRequest runs one agent turn on history against a recording server and returns
// the raw body of its first request.
func firstTurnRequest(t *testing.T, history []llm.Message) []byte {
	t.Helper()
	bodies := make(chan []byte, 4)
	client := fixtureClient(t, testConfig(), func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		writeEvents(w, textReply("ok")...)
	})
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	cfg := testConfig()
	cfg.TotalTimeoutSec = 30
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client: client, LLM: cfg, Registry: registry, PreviewCap: 2048, RunDir: t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(), UserTurns: history,
	})
	maxSteps := 10
	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatal(err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: budget}
	for _, err := range a.Run(ic) {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	return <-bodies
}

// The next turn reads the previous turn's stored history from the cache. MEASURED on the
// lab VM, conversation 01a2223e, 2026-11-28: every new turn read exactly tools + system
// (20,873) and wrote the whole history again, because the loaded-history breakpoint sat on
// the memory block the runner puts before each user message. No later turn replays that
// block, so the entry was never read. The histories are the ladder's shape: the always-block
// first, stored turns without thinking or hints, the memory block right before the question.
func TestNextTurnReadsThePreviousTurnsHistory(t *testing.T) {
	head := []llm.Message{
		{Role: llm.RoleUser, Content: "## Skills\n- memory-aura\n\n## Operator profile\nname: Davide"},
		{Role: llm.RoleUser, Content: "come ti trovi in questo ambiente?"},
		{Role: llm.RoleAssistant, Content: "Bene, nel senso pratico."},
	}
	question := llm.Message{Role: llm.RoleUser, Content: "si finirti è da maggio che lavoro su Aura"}
	memory := func(facts string) llm.Message {
		return llm.Message{Role: llm.RoleUser, Content: "<memory_context>\n" + facts + "\n</memory_context>"}
	}
	turnN := append(append([]llm.Message(nil), head...), memory("28 facts about 27 entities"), question)
	stored := []llm.Message{
		question,
		{Role: llm.RoleAssistant, Content: "Lo segno.", ToolCalls: []llm.ToolCall{toolCall("toolu_01", "memory__memory_upsert_fact", `{"subject":"Davide"}`)}},
		{Role: llm.RoleTool, ToolCallID: "toolu_01", Content: `{"classified":[{"name":"Davide"}]}`},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("toolu_02", "memory__memory_digest", `{}`), toolCall("toolu_03", "memory__memory_facts_about", `{"entity":"Aura"}`)}},
		{Role: llm.RoleTool, ToolCallID: "toolu_02", Content: `{"facts":29}`},
		{Role: llm.RoleTool, ToolCallID: "toolu_03", Content: `{"facts":[]}`},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("toolu_04", "memory__memory_batch", `{}`)}},
		{Role: llm.RoleTool, ToolCallID: "toolu_04", Content: `{"applied":1}`},
		{Role: llm.RoleAssistant, Content: "Ah, allora conosci Aura molto meglio di me."},
	}
	turnN1 := append(append(append([]llm.Message(nil), head...), stored...),
		memory("29 facts about 27 entities"), llm.Message{Role: llm.RoleUser, Content: "difatti ti uso per fare debug da dentro"})

	before := parseWire(t, firstTurnRequest(t, turnN))
	after := parseWire(t, firstTurnRequest(t, turnN1))
	if before.tools != after.tools || before.system != after.system {
		t.Fatal("tools or system differ between the two turns")
	}
	written := -1
	for i, block := range before.blocks {
		if block.cached {
			written = i
			break
		}
	}
	if written < 0 {
		t.Fatal("turn N wrote no message breakpoint")
	}
	for i := 0; i <= written; i++ {
		was, now := before.blocks[i], after.blocks[min(i, len(after.blocks)-1)]
		if i >= len(after.blocks) || was.message != now.message || was.role != now.role || was.bytes != now.bytes {
			t.Fatalf("turn N's loaded-history entry (block %d, %s) is not a prefix of turn N+1: block %d is\n turn N   %s\n turn N+1 %s",
				written, before.blocks[written].bytes, i, was.bytes, now.bytes)
		}
	}
	for i := written; i < len(after.blocks); i++ {
		if after.blocks[i].cached && positions(after.blocks, written, i) <= lookbackPositions {
			return
		}
	}
	t.Fatalf("no breakpoint of turn N+1 reaches turn N's entry at block %d within %d positions", written, lookbackPositions)
}

func TestPositionsCountToolRunsOnce(t *testing.T) {
	blocks := []wireBlock{
		{message: 0, kind: "text"},
		{message: 1, kind: "text"}, {message: 1, kind: "tool_use"}, {message: 1, kind: "tool_use"},
		{message: 2, kind: "tool_result"}, {message: 2, kind: "tool_result"}, {message: 2, kind: "text"},
	}
	if got := positions(blocks, 0, len(blocks)-1); got != 5 {
		t.Fatalf("positions = %d, want 5", got)
	}
	if got := positions(blocks, 3, 3); got != 1 {
		t.Fatalf("one block = %d positions, want 1", got)
	}
}
