package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func messagesOf(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, _ := body["messages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		out = append(out, object(t, m))
	}
	return out
}

func blocksOf(t *testing.T, message map[string]any) []map[string]any {
	t.Helper()
	raw, _ := message["content"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, b := range raw {
		out = append(out, object(t, b))
	}
	return out
}

func typesOf(blocks []map[string]any) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b["type"].(string))
	}
	return out
}

// replayRequest is a tool round on the anthropic route: a replayed assistant state, its
// result, and the per-round hint after it.
func replayRequest() llm.Request {
	state := `[{"type":"thinking","thinking":"earlier thought","signature":"sig-0"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"query":"stale"}}]`
	return llm.Request{Model: "claude-sonnet-5-5", Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "Be careful."},
		{Role: llm.RoleUser, Content: "secret user content"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("toolu_1", "lookup", `{"query":"x"}`)}, ProviderState: json.RawMessage(state)},
		{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "secret tool output"},
		{Role: llm.RoleUser, Content: "<budget>hint</budget>"},
	}}
}

func TestSystemIsHoistedAndToolsBecomeResults(t *testing.T) {
	req := llm.Request{Model: "claude-sonnet-5-5", Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "Rule A"}, {Role: llm.RoleSystem, Content: "Rule B"},
		{Role: llm.RoleUser, Content: "First"},
		{Role: llm.RoleAssistant, Content: "Checking", ToolCalls: []llm.ToolCall{toolCall("call_1", "lookup", `{"k":"v"}`)}},
		{Role: llm.RoleTool, ToolCallID: "call_1", Content: "Found"},
		{Role: llm.RoleUser, Content: "Next"},
	}}
	body := send(t, testConfig(), req)
	system := body["system"].([]any)
	if len(system) != 2 || object(t, system[0])["text"] != "Rule A" || object(t, system[1])["text"] != "Rule B" {
		t.Fatalf("system %#v", system)
	}
	messages := messagesOf(t, body)
	if len(messages) != 3 || messages[0]["role"] != "user" || messages[1]["role"] != "assistant" || messages[2]["role"] != "user" {
		t.Fatalf("messages %#v", messages)
	}
	assistant := blocksOf(t, messages[1])
	if !reflect.DeepEqual(typesOf(assistant), []string{"text", "tool_use"}) || assistant[0]["text"] != "Checking" {
		t.Fatalf("assistant %#v", assistant)
	}
	if assistant[1]["id"] != "call_1" || assistant[1]["name"] != "lookup" || !reflect.DeepEqual(assistant[1]["input"], map[string]any{"k": "v"}) {
		t.Fatalf("tool_use %#v", assistant[1])
	}
	results := blocksOf(t, messages[2])
	if !reflect.DeepEqual(typesOf(results), []string{"tool_result", "text"}) || results[0]["tool_use_id"] != "call_1" || results[1]["text"] != "Next" {
		t.Fatalf("results %#v", results)
	}
	if inner := results[0]["content"].([]any); len(inner) != 1 || object(t, inner[0])["text"] != "Found" {
		t.Fatalf("tool_result content %#v", results[0]["content"])
	}
}

func TestCompactionMarkerStaysAUserMessage(t *testing.T) {
	req := llm.Request{Model: "claude-sonnet-5-5", Messages: []llm.Message{
		{Role: llm.RoleUser, Content: "Summary of earlier rounds", ToolCallID: "__aura_compaction__"},
		{Role: llm.RoleAssistant, Content: "Understood."},
		{Role: llm.RoleUser, Content: "Go on"},
	}}
	messages := messagesOf(t, send(t, testConfig(), req))
	first := blocksOf(t, messages[0])
	if len(messages) != 3 || !reflect.DeepEqual(typesOf(first), []string{"text"}) || first[0]["text"] != "Summary of earlier rounds" {
		t.Fatalf("messages %#v", messages)
	}
}

func TestInterleavedBlocksKeepTheirOrder(t *testing.T) {
	events := reply(thinkingBlock(0, "plan", "sig-a"), textBlock(1, "Let me check."), toolBlock(2, "toolu_a", "lookup", `{"q":`, `"one"}`),
		thinkingBlock(3, "again", "sig-b"), toolBlock(4, "toolu_b", "lookup", `{"q":"two"}`))
	client, sent := recordingClient(t, testConfig(), append(events, messageDelta("tool_use", 30), messageStop()))
	var calls []llm.ToolCall
	var state json.RawMessage
	for _, chunk := range drain(t, client, userRequest("go")) {
		if chunk.ToolCall != nil {
			calls = append(calls, *chunk.ToolCall)
		}
		if chunk.ProviderState != nil {
			state = chunk.ProviderState
		}
	}
	<-sent
	req := userRequest("go")
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls, ProviderState: state},
		llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_a", Content: "1"}, llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_b", Content: "2"})
	drain(t, client, req)
	assistant := blocksOf(t, messagesOf(t, (<-sent).body)[1])
	if !reflect.DeepEqual(typesOf(assistant), []string{"thinking", "text", "tool_use", "thinking", "tool_use"}) {
		t.Fatalf("replayed order %v", typesOf(assistant))
	}
	if assistant[0]["signature"] != "sig-a" || assistant[0]["thinking"] != "plan" || assistant[3]["signature"] != "sig-b" || assistant[1]["text"] != "Let me check." {
		t.Fatalf("replayed blocks %#v", assistant)
	}
	if !reflect.DeepEqual(assistant[2]["input"], map[string]any{"q": "one"}) || assistant[4]["id"] != "toolu_b" {
		t.Fatalf("replayed tool_use %#v", assistant)
	}
}

func TestOmittedThinkingRoundTrips(t *testing.T) {
	events := append(reply(thinkingBlock(0, "", "sig-omitted"), toolBlock(1, "toolu_1", "lookup", `{}`)), messageDelta("tool_use", 3), messageStop())
	client, sent := recordingClient(t, testConfig(), events)
	var calls []llm.ToolCall
	var state json.RawMessage
	for _, chunk := range drain(t, client, userRequest("go")) {
		if chunk.ToolCall != nil {
			calls = append(calls, *chunk.ToolCall)
		}
		if chunk.ProviderState != nil {
			state = chunk.ProviderState
		}
	}
	<-sent
	req := userRequest("go")
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls, ProviderState: state}, llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "done"})
	drain(t, client, req)
	thinking := blocksOf(t, messagesOf(t, (<-sent).body)[1])[0]
	if !reflect.DeepEqual(thinking, map[string]any{"type": "thinking", "thinking": "", "signature": "sig-omitted"}) {
		t.Fatalf("omitted thinking replayed as %#v", thinking)
	}
}

func TestStateIsIgnoredWhenCallsWereShortened(t *testing.T) {
	state := json.RawMessage(`[{"type":"thinking","thinking":"t","signature":"s"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}},{"type":"tool_use","id":"toolu_2","name":"lookup","input":{}}]`)
	for name, calls := range map[string][]llm.ToolCall{
		"deduped":   {toolCall("toolu_1", "lookup", `{"q":1}`)},
		"rewritten": {toolCall("toolu_1", "lookup", `{"q":1}`), toolCall("toolu_2_1", "lookup", `{"q":2}`)},
		"reordered": {toolCall("toolu_2", "lookup", `{"q":2}`), toolCall("toolu_1", "lookup", `{"q":1}`)},
	} {
		t.Run(name, func(t *testing.T) {
			req := userRequest("go")
			req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls, ProviderState: state})
			assistant := blocksOf(t, messagesOf(t, send(t, testConfig(), req))[1])
			if len(assistant) != len(calls) || assistant[0]["type"] != "tool_use" || assistant[0]["id"] != calls[0].ID {
				t.Fatalf("rebuilt assistant %#v", assistant)
			}
		})
	}
	req := userRequest("go")
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Content: "Hm", ToolCalls: []llm.ToolCall{toolCall("toolu_1", "lookup", "{}")}, ProviderState: json.RawMessage(`{not json`)})
	if got := typesOf(blocksOf(t, messagesOf(t, send(t, testConfig(), req))[1])); !reflect.DeepEqual(got, []string{"text", "tool_use"}) {
		t.Fatalf("broken state replayed: %v", got)
	}
}

// texts collects every "text" field anywhere in the body.
func texts(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			if s, ok := value.(string); ok && key == "text" {
				*out = append(*out, s)
			}
			texts(value, out)
		}
	case []any:
		for _, value := range x {
			texts(value, out)
		}
	}
}

func TestBlankTextNeverReachesTheWire(t *testing.T) {
	state := json.RawMessage(`[{"type":"text","text":"  "},{"type":"tool_use","id":"toolu_2","name":"lookup","input":{}}]`)
	req := llm.Request{Model: "claude-sonnet-5-5", Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "  "},
		{Role: llm.RoleUser, Content: ""},
		{Role: llm.RoleUser, Content: "real question"},
		{Role: llm.RoleAssistant, Content: " \n "},
		{Role: llm.RoleAssistant, Content: "\t", ToolCalls: []llm.ToolCall{toolCall("toolu_1", "lookup", "{}")}},
		{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "  "},
		{Role: llm.RoleUser, Content: "\n"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("toolu_2", "lookup", "{}")}, ProviderState: state},
		{Role: llm.RoleTool, ToolCallID: "toolu_2", Content: ""},
	}}
	body := send(t, testConfig(), req)
	if _, ok := body["system"]; ok {
		t.Fatalf("blank system sent: %v", body["system"])
	}
	var all []string
	texts(body, &all)
	for _, text := range all {
		if strings.TrimSpace(text) == "" {
			t.Fatalf("blank text on the wire: %q in %v", text, all)
		}
	}
	messages := messagesOf(t, body)
	roles := make([]string, 0, len(messages))
	for _, m := range messages {
		roles = append(roles, m["role"].(string))
	}
	if !reflect.DeepEqual(roles, []string{"user", "assistant", "user", "assistant", "user"}) {
		t.Fatalf("roles %v", roles)
	}
	for _, i := range []int{2, 4} {
		result := blocksOf(t, messages[i])[0]
		if inner := result["content"].([]any); object(t, inner[0])["text"] != "(no output)" {
			t.Fatalf("empty tool result sent as %#v", result)
		}
	}
}

var wireID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func TestToolIDsAreSanitizedOnBothSides(t *testing.T) {
	long := strings.Repeat("x", 62) + ".tail:1"
	ids := []string{"functions.x:0", long, "a.b", "a:b", "toolu_01-valid"}
	req := userRequest("go")
	calls := make([]llm.ToolCall, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, toolCall(id, "lookup", "{}"))
	}
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls})
	for _, id := range ids {
		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: "ok"})
	}
	messages := messagesOf(t, send(t, testConfig(), req))
	uses, results := blocksOf(t, messages[1]), blocksOf(t, messages[2])
	seen := map[string]bool{}
	for i, id := range ids {
		wire := uses[i]["id"].(string)
		if !wireID.MatchString(wire) || results[i]["tool_use_id"] != wire || seen[wire] {
			t.Fatalf("id %q became %q, result %v", id, wire, results[i]["tool_use_id"])
		}
		seen[wire] = true
	}
	if uses[4]["id"] != "toolu_01-valid" {
		t.Fatalf("a valid id was rewritten: %v", uses[4]["id"])
	}
}

func TestNonObjectInputBecomesEmpty(t *testing.T) {
	args := []string{"", "[1,2]", "null", "not json", `"text"`, "42", `{"ok":true}`}
	req := userRequest("go")
	calls := make([]llm.ToolCall, 0, len(args))
	for i, arg := range args {
		calls = append(calls, toolCall("toolu_"+string(rune('a'+i)), "lookup", arg))
	}
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls})
	uses := blocksOf(t, messagesOf(t, send(t, testConfig(), req))[1])
	for i := range args[:len(args)-1] {
		if !reflect.DeepEqual(uses[i]["input"], map[string]any{}) {
			t.Fatalf("args %q sent as input %#v", args[i], uses[i]["input"])
		}
	}
	if !reflect.DeepEqual(uses[len(args)-1]["input"], map[string]any{"ok": true}) {
		t.Fatalf("object input %#v", uses[len(args)-1]["input"])
	}
}

// markers lists, for every block carrying cache_control, where it sits.
func markers(t *testing.T, body map[string]any) []string {
	t.Helper()
	var out []string
	for i, tl := range body["tools"].([]any) {
		if object(t, tl)["cache_control"] != nil {
			out = append(out, "tool:"+object(t, tl)["name"].(string)+":"+string(rune('0'+i)))
		}
	}
	for _, s := range body["system"].([]any) {
		if object(t, s)["cache_control"] != nil {
			out = append(out, "system:"+object(t, s)["text"].(string))
		}
	}
	for _, m := range messagesOf(t, body) {
		for _, b := range blocksOf(t, m) {
			if cc := b["cache_control"]; cc != nil {
				if !reflect.DeepEqual(cc, map[string]any{"type": "ephemeral", "ttl": "5m"}) {
					t.Fatalf("cache_control %#v", cc)
				}
				label := b["type"].(string)
				if text, ok := b["text"].(string); ok {
					label += ":" + text
				}
				if id, ok := b["tool_use_id"].(string); ok {
					label += ":" + id
				}
				if id, ok := b["id"].(string); ok {
					label += ":" + id
				}
				out = append(out, label)
			}
		}
	}
	return out
}

func cachedRequest(stable, loaded int) llm.Request {
	state := json.RawMessage(`[{"type":"thinking","thinking":"t","signature":"s"},{"type":"tool_use","id":"t1","name":"lookup","input":{}},{"type":"tool_use","id":"t2","name":"lookup","input":{}}]`)
	return llm.Request{Model: "claude-sonnet-5-5", ToolsCacheControl: "ephemeral", StablePrefixMessages: stable, LoadedHistoryMessages: loaded,
		Tools: []llm.ToolDef{tool("lookup", "Look up.", ""), tool("other", "Other.", "")},
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "S"},
			{Role: llm.RoleUser, Content: "q1"},
			{Role: llm.RoleAssistant, Content: "a1"},
			{Role: llm.RoleUser, Content: "q2"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("t1", "lookup", "{}"), toolCall("t2", "lookup", "{}")}, ProviderState: state},
			{Role: llm.RoleTool, ToolCallID: "t1", Content: "r1"},
			{Role: llm.RoleTool, ToolCallID: "t2", Content: "r2"},
			{Role: llm.RoleUser, Content: "hint"},
		}}
}

func TestCacheBreakpointsLandOnToolsSystemLoadedHistoryAndTheStablePrefix(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stable, loaded int
		want           []string
	}{
		{"tool round", 7, 3, []string{"tool:other:1", "system:S", "text:a1", "tool_result:t2"}},
		{"coinciding positions", 4, 4, []string{"tool:other:1", "system:S", "text:q2"}},
		{"fresh conversation", 2, 1, []string{"tool:other:1", "system:S", "text:q1"}},
		{"no positions", 0, 0, []string{"tool:other:1", "system:S"}},
		{"position on an assistant state", 5, 0, []string{"tool:other:1", "system:S", "tool_use:t2"}},
		{"positions out of range", 99, -1, []string{"tool:other:1", "system:S"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := markers(t, send(t, testConfig(), cachedRequest(tc.stable, tc.loaded)))
			if !reflect.DeepEqual(got, tc.want) || len(got) > 4 {
				t.Fatalf("markers %v, want %v", got, tc.want)
			}
		})
	}
	req := cachedRequest(7, 3)
	req.ToolsCacheControl = ""
	if got := markers(t, send(t, testConfig(), req)); !reflect.DeepEqual(got, []string{"text:a1", "tool_result:t2"}) {
		t.Fatalf("without the tools marker: %v", got)
	}
}

func TestCacheBreakpointSkipsThinking(t *testing.T) {
	req := cachedRequest(0, 0)
	req.Messages[4].ProviderState = json.RawMessage(`[{"type":"tool_use","id":"t1","name":"lookup","input":{}},{"type":"tool_use","id":"t2","name":"lookup","input":{}},{"type":"thinking","thinking":"late","signature":"s"}]`)
	req.StablePrefixMessages = 5
	if got := markers(t, send(t, testConfig(), req)); !reflect.DeepEqual(got, []string{"tool:other:1", "system:S", "tool_use:t2"}) {
		t.Fatalf("markers %v", got)
	}
}

type fakeLoader map[string]llm.VerifiedContentPart

func (l fakeLoader) LoadContentPart(_ context.Context, _, _, id string) (llm.VerifiedContentPart, error) {
	part, ok := l[id]
	if !ok {
		return part, errors.New("no such part")
	}
	return part, nil
}

func TestMediaReachesTheModelAsReferenceText(t *testing.T) {
	loader := fakeLoader{"ref1": {ID: "ref1", MIMEType: "image/png", FallbackText: "Photo of a receipt", Bytes: []byte("\x89PNG")}}
	req := llm.Request{Model: "claude-sonnet-5-5", LoadedHistoryMessages: 3,
		ContentProjection: &llm.ContentProjection{Loader: loader, ReferenceIDs: []string{"ref1"}},
		ToolMedia: map[string][]llm.ProjectedRequestPart{"t1": {
			{Type: "media", MIMEType: "image/png", Bytes: []byte("\x89PNG")},
			{Type: "reference", MIMEType: "application/pdf", Text: "Chart summary", ReferenceOnly: true},
		}},
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "S"}, {Role: llm.RoleUser, Content: "old"}, {Role: llm.RoleAssistant, Content: "a"},
			{Role: llm.RoleUser, Content: "look at this"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("t1", "render", "{}")}},
			{Role: llm.RoleTool, ToolCallID: "t1", Content: "rendered"},
			{Role: llm.RoleUser, Content: "hint"},
		}}
	body := send(t, testConfig(), req)
	messages := messagesOf(t, body)
	current := blocksOf(t, messages[2])
	if !reflect.DeepEqual(typesOf(current), []string{"text", "text"}) || current[1]["text"] != "Photo of a receipt" {
		t.Fatalf("current user message %#v", current)
	}
	result := blocksOf(t, messages[4])[0]["content"].([]any)
	if len(result) != 2 || object(t, result[1])["text"] != "Chart summary" {
		t.Fatalf("tool_result content %#v", result)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), `"image"`) || strings.Contains(string(raw), `"document"`) || strings.Contains(string(raw), "base64") {
		t.Fatalf("a media block reached the wire: %s", raw)
	}
	req.LoadedHistoryMessages = 0
	if last := blocksOf(t, messagesOf(t, send(t, testConfig(), req))[4]); last[len(last)-1]["text"] != "Photo of a receipt" {
		t.Fatalf("without the loaded position the last user message carries it: %#v", last)
	}
}

func TestMediaFailuresStopTheRequest(t *testing.T) {
	for name, req := range map[string]llm.Request{
		"no loader":       {Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, ContentProjection: &llm.ContentProjection{ReferenceIDs: []string{"x"}}},
		"load fails":      {Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, ContentProjection: &llm.ContentProjection{Loader: fakeLoader{}, ReferenceIDs: []string{"x"}}},
		"no user message": {Messages: []llm.Message{{Role: llm.RoleSystem, Content: "S"}}, ContentProjection: &llm.ContentProjection{Loader: fakeLoader{"x": {ID: "x", MIMEType: "image/png", FallbackText: "img"}}, ReferenceIDs: []string{"x"}}},
		"bad role":        {Messages: []llm.Message{{Role: "developer", Content: "hi"}}},
		"tool without id": {Messages: []llm.Message{{Role: llm.RoleTool, Content: "hi"}}},
		"call without id": {Messages: []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("", "lookup", "{}")}}}},
		"unnamed tool":    {Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, Tools: []llm.ToolDef{tool("", "", "")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fixtureClient(t, testConfig(), nil).Stream(context.Background(), req); err == nil {
				t.Fatal("request accepted")
			}
		})
	}
}
