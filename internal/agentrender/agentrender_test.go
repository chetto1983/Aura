package agentrender

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

// evalAnyIntOld replicates the PRE-extraction internal/eval copy of anyInt, which
// lacked the json.Number branch and silently returned 0 for a json.Number token
// count. It lives in the parity test (not production) so the AnyInt table can
// DOCUMENT the drift the extraction fixes: the extracted AnyInt is the chat_render
// superset, so for a valid json.Number it MUST diverge from this old eval copy
// (the documented behavior fix, QA-C-04 / T-32-07-FIX).
func evalAnyIntOld(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// chatAnyIntOld replicates the PRE-extraction cmd/aura/chat_render.go copy of anyInt
// (the superset, with the json.Number branch). AnyInt must equal this for EVERY
// input — that is the D-09 parity assertion: the extracted func matches the copy it
// was promoted from, byte-for-byte across the union of inputs.
func chatAnyIntOld(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		return 0
	default:
		return 0
	}
}

// TestAnyInt locks the QUAL-03 union and the documented eval fix. For every case the
// extracted AnyInt must equal the chat_render superset (chatAnyIntOld). The
// fixesEvalCopy cases additionally assert AnyInt DIFFERS from the old eval copy
// (evalAnyIntOld) — i.e. the previously-lossy json.Number token count is now parsed.
func TestAnyInt(t *testing.T) {
	tests := []struct {
		name          string
		in            any
		want          int
		fixesEvalCopy bool // AnyInt must DIFFER from the old eval copy for this input
	}{
		{name: "int", in: int(5), want: 5},
		{name: "int64", in: int64(7), want: 7},
		{name: "float64 truncates", in: float64(3.9), want: 3},
		{name: "json.Number valid (eval FIX)", in: json.Number("11"), want: 11, fixesEvalCopy: true},
		{name: "json.Number large (eval FIX)", in: json.Number("9007199254740992"), want: 9007199254740992, fixesEvalCopy: true},
		{name: "json.Number invalid falls back to 0", in: json.Number("nope"), want: 0},
		{name: "nil", in: nil, want: 0},
		{name: "unknown type string", in: "nope", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnyInt(tt.in)
			if got != tt.want {
				t.Fatalf("AnyInt(%v) = %d, want %d", tt.in, got, tt.want)
			}
			// D-09 parity: extracted == the chat_render superset it was promoted from.
			if chat := chatAnyIntOld(tt.in); got != chat {
				t.Fatalf("AnyInt(%v) = %d diverges from chat_render copy %d", tt.in, got, chat)
			}
			old := evalAnyIntOld(tt.in)
			if tt.fixesEvalCopy {
				// The documented eval fix: a valid json.Number was silently zeroed by
				// the old eval copy; AnyInt now parses it.
				if old != 0 {
					t.Fatalf("test setup: old eval copy on %v = %d, expected the lossy 0", tt.in, old)
				}
				if got == old {
					t.Fatalf("AnyInt(%v) = %d must DIFFER from the lossy old eval copy %d (the fix)", tt.in, got, old)
				}
			} else if got != old {
				// Non-fix inputs: old eval copy and new must agree (parity).
				t.Fatalf("AnyInt(%v) = %d but old eval copy = %d; expected parity on non-json.Number input", tt.in, got, old)
			}
		})
	}
}

// TestAnyFloat locks the cost coercion union: float64/int/json.Number(valid) parse;
// json.Number(invalid)/unknown/nil fall back to (0, false). Parity with both old
// copies holds here (both copies were byte-identical for AnyFloat).
func TestAnyFloat(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   float64
		wantOK bool
	}{
		{name: "float64", in: float64(2.5), want: 2.5, wantOK: true},
		{name: "int widens", in: int(3), want: 3.0, wantOK: true},
		{name: "json.Number valid", in: json.Number("4.2"), want: 4.2, wantOK: true},
		{name: "json.Number invalid", in: json.Number("nope"), want: 0, wantOK: false},
		{name: "unknown type string", in: "nope", want: 0, wantOK: false},
		{name: "nil", in: nil, want: 0, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AnyFloat(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("AnyFloat(%v) = %v,%v want %v,%v", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestUsageFromStateDelta covers nil→zero, the numeric-widening token paths
// (int/int64/float64/json.Number), and cost present/absent/unparseable. The
// json.Number token sub-case proves the eval fix end-to-end through the struct.
func TestUsageFromStateDelta(t *testing.T) {
	t.Run("nil map yields zero Usage", func(t *testing.T) {
		if u := UsageFromStateDelta(nil); u != (llm.Usage{}) {
			t.Fatalf("nil map -> %+v, want zero Usage", u)
		}
	})
	t.Run("mixed numeric widths and float cost", func(t *testing.T) {
		u := UsageFromStateDelta(map[string]any{
			"prompt_tokens":     int(7),
			"completion_tokens": int64(3),
			"cache_hit_tokens":  float64(2),
			"cost_usd":          float64(0.5),
		})
		if u.PromptTokens != 7 || u.CompletionTokens != 3 || u.CachedTokens != 2 {
			t.Fatalf("tokens = %+v, want 7/3/2 across int/int64/float64", u)
		}
		if u.Cost == nil || *u.Cost != 0.5 {
			t.Fatalf("Cost = %v, want 0.5", u.Cost)
		}
	})
	t.Run("json.Number tokens parsed (eval FIX)", func(t *testing.T) {
		u := UsageFromStateDelta(map[string]any{
			"prompt_tokens":     json.Number("11"),
			"completion_tokens": json.Number("5"),
			"cache_hit_tokens":  json.Number("nope"),
			"cost_usd":          json.Number("4.2"),
		})
		if u.PromptTokens != 11 || u.CompletionTokens != 5 {
			t.Fatalf("json.Number tokens = %d/%d, want 11/5 (the eval fix)", u.PromptTokens, u.CompletionTokens)
		}
		if u.CachedTokens != 0 {
			t.Fatalf("non-numeric json.Number -> %d, want 0 fallback", u.CachedTokens)
		}
		if u.Cost == nil || *u.Cost != 4.2 {
			t.Fatalf("json.Number cost_usd -> %v, want 4.2", u.Cost)
		}
	})
	t.Run("absent cost stays nil", func(t *testing.T) {
		u := UsageFromStateDelta(map[string]any{"prompt_tokens": int(1)})
		if u.Cost != nil {
			t.Fatalf("absent cost_usd -> %v, want nil (no fabricated cost)", u.Cost)
		}
	})
	t.Run("unparseable cost stays nil", func(t *testing.T) {
		u := UsageFromStateDelta(map[string]any{"cost_usd": "nope"})
		if u.Cost != nil {
			t.Fatalf("string cost_usd -> %v, want nil", u.Cost)
		}
	})
}

// TestFlushRemainder covers the three render branches: empty/equal early-return (no
// emit), prefix tail-only emit, and divergent reset-and-emit-full.
func TestFlushRemainder(t *testing.T) {
	// emit mirrors the renderer's emit closure: it also appends to prose, so a
	// prefix flush extends the buffer exactly as the REPL does.
	newEmit := func(prose, out *strings.Builder) func(string) {
		return func(s string) {
			prose.WriteString(s)
			out.WriteString(s)
		}
	}
	t.Run("empty final emits nothing", func(t *testing.T) {
		var prose, out strings.Builder
		prose.WriteString("kept")
		FlushRemainder(&prose, "", newEmit(&prose, &out))
		if out.Len() != 0 {
			t.Fatalf("empty final emitted %q, want nothing", out.String())
		}
	})
	t.Run("final equals streamed emits nothing", func(t *testing.T) {
		var prose, out strings.Builder
		prose.WriteString("completo")
		FlushRemainder(&prose, "completo", newEmit(&prose, &out))
		if out.Len() != 0 {
			t.Fatalf("equal final emitted %q, want nothing", out.String())
		}
	})
	t.Run("prefix emits only the tail", func(t *testing.T) {
		var prose, out strings.Builder
		prose.WriteString("Ciao ")
		FlushRemainder(&prose, "Ciao mondo", newEmit(&prose, &out))
		if out.String() != "mondo" {
			t.Fatalf("prefix flush emitted %q, want %q", out.String(), "mondo")
		}
	})
	t.Run("divergent resets prose and emits full", func(t *testing.T) {
		var prose, out strings.Builder
		prose.WriteString("AAA")
		FlushRemainder(&prose, "ZZZ final", newEmit(&prose, &out))
		if out.String() != "ZZZ final" {
			t.Fatalf("divergent flush emitted %q, want full answer", out.String())
		}
		if prose.String() != "ZZZ final" {
			t.Fatalf("divergent flush left prose %q, want the full reset answer", prose.String())
		}
	})
}

// TestIsToolResultPreview: a tool-result Event is identified by the tool_call_id the
// LlmAgent stamps into Actions.StateDelta; everything else is not a preview.
func TestIsToolResultPreview(t *testing.T) {
	t.Run("with tool_call_id is a preview", func(t *testing.T) {
		ev := &agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"tool_call_id": "c1"}}}
		if !IsToolResultPreview(ev) {
			t.Fatal("event with tool_call_id must be a tool-result preview")
		}
	})
	t.Run("without the key is not a preview", func(t *testing.T) {
		ev := &agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"prompt_tokens": 1}}}
		if IsToolResultPreview(ev) {
			t.Fatal("event without tool_call_id must not be a preview")
		}
	})
	t.Run("nil StateDelta is not a preview", func(t *testing.T) {
		ev := &agent.Event{}
		if IsToolResultPreview(ev) {
			t.Fatal("event with nil StateDelta must not be a preview")
		}
	})
}

// TestIsTerminalToolCall: the loop-terminating text_response marks a terminal call;
// any other tool name (or no calls) is non-terminal.
func TestIsTerminalToolCall(t *testing.T) {
	mk := func(name string) llm.ToolCall {
		var tc llm.ToolCall
		tc.Function.Name = name
		return tc
	}
	tests := []struct {
		name  string
		calls []llm.ToolCall
		want  bool
	}{
		{name: "text_response is terminal", calls: []llm.ToolCall{mk("text_response")}, want: true},
		{name: "mixed with text_response is terminal", calls: []llm.ToolCall{mk("web_search"), mk("text_response")}, want: true},
		{name: "non-terminal tool", calls: []llm.ToolCall{mk("web_search")}, want: false},
		{name: "empty is non-terminal", calls: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTerminalToolCall(tt.calls); got != tt.want {
				t.Fatalf("IsTerminalToolCall(%v) = %v, want %v", tt.calls, got, tt.want)
			}
		})
	}
}
