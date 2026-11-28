package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// withProviderState inserts the state chunk where a provider client sends it: alone,
// once, just before the stream's finish_reason chunk.
func withProviderState(turn agenttest.FakeTurn, state string) agenttest.FakeTurn {
	last := len(turn.Chunks) - 1
	chunks := append([]llm.Chunk(nil), turn.Chunks[:last]...)
	chunks = append(chunks, llm.Chunk{ProviderState: json.RawMessage(state)}, turn.Chunks[last])
	return agenttest.FakeTurn{Chunks: chunks}
}

func echoRound(id, v string) agenttest.FakeTurn {
	return agenttest.ToolCallTurn(agenttest.MakeToolCall(id, "echo", `{"v":"`+v+`"}`))
}

// toolCallStates lists, in order, the ProviderState of every assistant tool-call
// message one recorded request carried.
func toolCallStates(req llm.Request) []string {
	var states []string
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			states = append(states, string(msg.ProviderState))
		}
	}
	return states
}

func runProviderStateScript(t *testing.T, turns ...agenttest.FakeTurn) *agenttest.FakeClient {
	t.Helper()
	fc := agenttest.NewFakeClient(turns...)
	if _, err := collect(newAgent(t, fc, llm.Config{}).Run(newIC(t, agent.BudgetOptions{}))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return fc
}

func TestToolCallMessageCarriesTheRoundsProviderState(t *testing.T) {
	fc := runProviderStateScript(t,
		withProviderState(echoRound("c1", "one"), `{"blocks":["s1"]}`),
		agenttest.TextChunks("stop", "Fatto."),
	)
	reqs := fc.RecordedRequests()
	if len(reqs) != 2 {
		t.Fatalf("model calls = %d, want 2", len(reqs))
	}
	if got := toolCallStates(reqs[1]); !reflect.DeepEqual(got, []string{`{"blocks":["s1"]}`}) {
		t.Fatalf("second request tool-call states = %q, want the first round's state", got)
	}
}

func TestEveryRoundReplaysItsOwnBlocks(t *testing.T) {
	fc := runProviderStateScript(t,
		withProviderState(echoRound("c1", "one"), `"s1"`),
		withProviderState(echoRound("c2", "two"), `"s2"`),
		withProviderState(echoRound("c3", "three"), `"s3"`),
		agenttest.TextChunks("stop", "Fatto."),
	)
	reqs := fc.RecordedRequests()
	if len(reqs) != 4 {
		t.Fatalf("model calls = %d, want 4", len(reqs))
	}
	if got := toolCallStates(reqs[3]); !reflect.DeepEqual(got, []string{`"s1"`, `"s2"`, `"s3"`}) {
		t.Fatalf("fourth request tool-call states = %q, want s1,s2,s3 in round order", got)
	}
}

// The state can hold thinking blocks: the trace row records its size and the trace file
// never holds a byte of it.
func TestTraceRecordsTheStatesSizeNeverItsBytes(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(reasoningtrace.Env, "1")
	t.Setenv("AURA_REASONING_TRACE_FILE", tracePath)
	const marker = "thinking-block-must-not-reach-the-trace"
	state := `{"blocks":["` + marker + `"]}`
	runProviderStateScript(t,
		withProviderState(echoRound("c1", "one"), state),
		agenttest.TextChunks("stop", "Fatto."),
	)
	raw, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if strings.Contains(string(raw), marker) {
		t.Fatalf("trace file holds the provider state's bytes:\n%s", raw)
	}
	var sizes []any
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("trace row %q: %v", line, err)
		}
		if row["stage"] == "agent_consume_provider_state_chunk" {
			sizes = append(sizes, row["bytes"])
		}
	}
	if !reflect.DeepEqual(sizes, []any{float64(len(state))}) {
		t.Fatalf("provider state rows bytes = %v, want one row of %d", sizes, len(state))
	}
}

func TestRoundWithoutStateLeavesTheFieldNil(t *testing.T) {
	fc := runProviderStateScript(t,
		echoRound("c1", "one"),
		agenttest.TextChunks("stop", "Fatto."),
	)
	for i, req := range fc.RecordedRequests() {
		for j, msg := range req.Messages {
			if msg.ProviderState != nil {
				t.Fatalf("request %d message %d ProviderState = %q, want nil on a stateless route", i, j, msg.ProviderState)
			}
		}
	}
}

func providerStateRequest() llm.Request {
	call := agenttest.MakeToolCall("c1", "echo", `{"v":"one"}`)
	return llm.Request{
		Model: "protected-model",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "ciao"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}, ProviderState: json.RawMessage(`{"blocks":["s1"]}`)},
			{Role: llm.RoleTool, ToolCallID: "c1", Content: "echo"},
		},
		MaxTokens: 128,
	}
}

func TestInProcessHookCannotChangeProviderState(t *testing.T) {
	for name, mutate := range map[string]func(*llm.Request){
		"replaced":        func(req *llm.Request) { req.Messages[1].ProviderState = json.RawMessage(`{"blocks":["forged"]}`) },
		"edited in place": func(req *llm.Request) { req.Messages[1].ProviderState[2] = 'X' },
		"dropped":         func(req *llm.Request) { req.Messages[1].ProviderState = nil },
	} {
		t.Run(name, func(t *testing.T) {
			req := providerStateRequest()
			hook := &recordingHook{beforeModel: func(_ context.Context, req *llm.Request) (*agent.ModelHookResult, error) {
				mutate(req)
				return nil, nil
			}}
			_, err := agent.NewHookManager(hook).BeforeModel(context.Background(), &req)
			if err == nil || !strings.Contains(err.Error(), `"messages"`) {
				t.Fatalf("BeforeModel error = %v, want the protected messages refusal", err)
			}
			if got := string(req.Messages[1].ProviderState); got != `{"blocks":["s1"]}` {
				t.Fatalf("ProviderState after refusal = %q, want the original restored", got)
			}
		})
	}
}

func TestCommandHookRewriteKeepsProviderState(t *testing.T) {
	m := newCommandHookManager(t, "rewrite_sampling", "", 2000)
	req := providerStateRequest()

	res, err := m.BeforeModel(context.Background(), &req)
	if err != nil || res != nil {
		t.Fatalf("BeforeModel res=%+v err=%v, want an in-place sampling rewrite", res, err)
	}
	if req.MaxTokens != 256 || req.Temperature != 0.7 {
		t.Fatalf("sampling rewrite not applied: max_tokens %d temperature %v", req.MaxTokens, req.Temperature)
	}
	if got := string(req.Messages[1].ProviderState); got != `{"blocks":["s1"]}` {
		t.Fatalf("ProviderState after the command rewrite = %q, want it kept", got)
	}
}

// Beside amendment #91's pin (history_reasoning_free_test.go): the state lives only in
// the run's memory. It has no column on a persisted turn and no key in the message JSON
// that every other route sends and the store reads.
func TestProviderStateNeverReachesConversationTurn(t *testing.T) {
	rt := reflect.TypeFor[conversations.Turn]()
	for f := range rt.Fields() {
		if strings.Contains(strings.ToLower(f.Name), "provider") {
			t.Fatalf("conversations.Turn.%s: provider state must never be persisted", f.Name)
		}
	}
	field, ok := reflect.TypeFor[llm.Message]().FieldByName("ProviderState")
	if !ok || field.Tag.Get("json") != "-" {
		t.Fatalf("llm.Message.ProviderState tag = %q (found %v), want json:\"-\"", field.Tag.Get("json"), ok)
	}
	stateless := providerStateRequest().Messages
	stateful := providerStateRequest().Messages
	stateless[1].ProviderState = nil
	want, err := json.Marshal(stateless)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(stateful)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("message JSON with state = %s, want byte-identical to %s", got, want)
	}
}
