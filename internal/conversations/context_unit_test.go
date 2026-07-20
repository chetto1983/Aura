// Unit tier (no build tag): the L1/L2/L2.5 ladder logic + the offline tiktoken
// encoder. No database — the L2.5 rot-event write goes through a fake emitter. The
// DB-backed rot-event row is asserted under db_integration in context_test.go.
package conversations

import (
	"context"
	"html"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// fakeRotEmitter records L2.5 context_rot_events writes without a DB.
type fakeRotEmitter struct {
	calls []rotCall
}

type rotCall struct {
	conversationID            string
	pairsDropped, before, aft int
}

func (f *fakeRotEmitter) insertContextRotEvent(_ context.Context, convID string, pairs, before, after int) error {
	f.calls = append(f.calls, rotCall{convID, pairs, before, after})
	return nil
}

func mustEncoderRaw(t *testing.T) *tiktoken.Tiktoken {
	t.Helper()
	enc, err := encoder()
	if err != nil {
		t.Fatalf("offline encoder init (must not hit network): %v", err)
	}
	return enc
}

// TestOfflineEncoder_NoNetwork proves the cl100k_base encoder initializes from the
// vendored embedded vocab (no network fetch) and produces a plausible token count.
func TestOfflineEncoder_NoNetwork(t *testing.T) {
	enc, err := encoder()
	if err != nil {
		t.Fatalf("encoder(): %v", err)
	}
	// "hello world" is 2 tokens in cl100k_base; a network failure would have errored
	// above. A second call must reuse the cached encoder (same pointer).
	if got := countTokens(enc, "hello world"); got != 2 {
		t.Errorf("countTokens(hello world): want 2, got %d", got)
	}
	enc2, _ := encoder()
	if enc != enc2 {
		t.Error("encoder must be cached (same instance across calls)")
	}
	if got := countTokens(enc, ""); got != 0 {
		t.Errorf("countTokens(empty): want 0, got %d", got)
	}
}

// TestL1_EvictsOldToolTurns_NeverSeq1: L1 rewrites old role='tool' turns to a
// read_tool_output pointer and NEVER touches seq=1 (system).
func TestL1_EvictsOldToolTurns_NeverSeq1(t *testing.T) {
	_ = mustEncoderRaw(t)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM PROMPT"},
		{Seq: 2, Role: llm.RoleUser, Content: "do a thing"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "calling tool"},
		{Seq: 4, Role: llm.RoleTool, Content: "huge old tool output", ToolCallID: "call_old", ContentSidecarPath: "/run/sidecar/call_old"},
		{Seq: 15, Role: llm.RoleTool, Content: "recent tool output", ToolCallID: "call_new"},
	}
	got := applyL1(turns, 10) // evict tool turns with seq < (15-10)=5

	if got[0].Content != "SYSTEM PROMPT" {
		t.Errorf("L1 must NEVER touch seq=1, got %q", got[0].Content)
	}
	if !strings.Contains(got[3].Content, "read_tool_output(tool_call_id=\"call_old\")") {
		t.Errorf("old tool turn (seq 4) must be evicted to a pointer, got %q", got[3].Content)
	}
	if got[4].Content != "recent tool output" {
		t.Errorf("recent tool turn (seq 15) must NOT be evicted, got %q", got[4].Content)
	}
	// The input slice is not mutated (LoadHistory byte-identity must hold).
	if turns[3].Content != "huge old tool output" {
		t.Error("applyL1 must not mutate its input")
	}
}

func TestL1_EvictPreservesExistingReadToolOutputSpillID(t *testing.T) {
	opaqueID := "call_old-a1b2c3d4e5f60708"
	content := `preview bytes` + "\n\n" +
		`[output truncated: showing bytes 0-2048 of 10000; read more via read_tool_output(tool_call_id="` + opaqueID + `", offset=2048, limit=2048)]`
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM PROMPT"},
		{Seq: 2, Role: llm.RoleTool, Content: content, ToolCallID: "call_old"},
		{Seq: 20, Role: llm.RoleUser, Content: "newest"},
	}

	got := applyL1(turns, 5)

	if !strings.Contains(got[1].Content, `read_tool_output(tool_call_id="`+opaqueID+`")`) {
		t.Fatalf("L1 must preserve existing opaque spill id, got %q", got[1].Content)
	}
	if strings.Contains(got[1].Content, `read_tool_output(tool_call_id="call_old")`) {
		t.Fatalf("L1 rewrote pointer back to provider id, got %q", got[1].Content)
	}
}

func TestL1_EvictPreservesHTMLEscapedReadToolOutputSpillID(t *testing.T) {
	opaqueID := "call_old-escaped"
	preview := `preview bytes` + "\n\n" +
		`[output truncated: showing bytes 0-2048 of 10000; read more via read_tool_output(tool_call_id="` + opaqueID + `", offset=2048, limit=2048)]`
	content := `<tool_output source="fs_read" trust="untrusted" nonce="abc">` + "\n" +
		html.EscapeString(preview) + "\n</tool_output>"
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM PROMPT"},
		{Seq: 2, Role: llm.RoleTool, Content: content, ToolCallID: "call_old"},
		{Seq: 20, Role: llm.RoleUser, Content: "newest"},
	}

	got := applyL1(turns, 5)

	if !strings.Contains(got[1].Content, `read_tool_output(tool_call_id="`+opaqueID+`")`) {
		t.Fatalf("L1 must preserve HTML-escaped opaque spill id, got %q", got[1].Content)
	}
}

func TestL1_EvictPreservesRealTruncationFooterOverFakeEarlierPointer(t *testing.T) {
	opaqueID := "call_old-real"
	content := `tool output mentions read_tool_output(tool_call_id="fake") before the footer` + "\n\n" +
		`[output truncated: showing bytes 0-2048 of 10000; read more via read_tool_output(tool_call_id="` + opaqueID + `", offset=2048, limit=2048)]`
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM PROMPT"},
		{Seq: 2, Role: llm.RoleTool, Content: content, ToolCallID: "call_old"},
		{Seq: 20, Role: llm.RoleUser, Content: "newest"},
	}

	got := applyL1(turns, 5)

	if strings.Contains(got[1].Content, `read_tool_output(tool_call_id="fake")`) {
		t.Fatalf("fake earlier pointer won over real footer, got %q", got[1].Content)
	}
	if !strings.Contains(got[1].Content, `read_tool_output(tool_call_id="`+opaqueID+`")`) {
		t.Fatalf("L1 must preserve real truncation footer spill id, got %q", got[1].Content)
	}
}

// TestL1_DisabledWhenEvictNonPositive.
func TestL1_DisabledWhenEvictNonPositive(t *testing.T) {
	turns := []Turn{{Seq: 1, Role: llm.RoleSystem, Content: "s"}, {Seq: 5, Role: llm.RoleTool, Content: "tool", ToolCallID: "c"}}
	got := applyL1(turns, 0)
	if got[1].Content != "tool" {
		t.Errorf("evictAfter<=0 disables L1, got %q", got[1].Content)
	}
}

// TestApplyL1_PreservesNonSidecarToolAnswers pins M-01: L1 must only rewrite a
// RoleTool turn to a read_tool_output pointer when it is actually sidecar-backed (a
// spill footer or a ContentSidecarPath). An ask_user answer — and any small inline
// result — is NOT spilled, so rewriting it to a pointer creates a DEAD reference the
// model can never page back, silently destroying the user's answer after ~10 rounds.
// Such a turn must survive verbatim; a sidecar-backed neighbor must still be evicted.
func TestApplyL1_PreservesNonSidecarToolAnswers(t *testing.T) {
	footered := "preview\n\n[output truncated: showing bytes 0-2048 of 9999; read more via read_tool_output(tool_call_id=\"spill_x\", offset=2048, limit=2048)]"
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleTool, Content: "Rome", ToolCallID: "ask_user_1"},                        // ask_user answer: no sidecar
		{Seq: 3, Role: llm.RoleTool, Content: "small ok", ToolCallID: "tiny_1"},                        // small inline result: no sidecar
		{Seq: 4, Role: llm.RoleTool, Content: footered, ToolCallID: "big_1"},                           // sidecar-backed (footer)
		{Seq: 5, Role: llm.RoleTool, Content: "p", ToolCallID: "path_1", ContentSidecarPath: "/run/x"}, // sidecar-backed (path)
		{Seq: 40, Role: llm.RoleUser, Content: "now"},
	}
	got := applyL1(turns, 5) // threshold = 35; seq 2..5 all old

	if got[1].Content != "Rome" {
		t.Fatalf("a non-sidecar ask_user answer must survive verbatim, got %q", got[1].Content)
	}
	if got[2].Content != "small ok" {
		t.Fatalf("a small non-sidecar result must survive verbatim, got %q", got[2].Content)
	}
	if !strings.Contains(got[3].Content, `read_tool_output(tool_call_id="spill_x")`) {
		t.Fatalf("a footer-backed result must still be evicted to its spill pointer, got %q", got[3].Content)
	}
	if !strings.Contains(got[4].Content, "read_tool_output") || got[4].ContentSidecarPath != "" {
		t.Fatalf("a ContentSidecarPath-backed result must still be evicted (and path cleared), got %q path=%q", got[4].Content, got[4].ContentSidecarPath)
	}
}

// TestLadder_SC1_L1AloneWritesNoRotEvent: a history bloated ONLY by large tool
// outputs is brought under budget by L1 alone → ZERO context_rot_events rows (SC-1).
func TestLadder_SC1_L1AloneWritesNoRotEvent(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}

	// Build a history where the only bloat is old tool outputs. After L1 eviction
	// the pointers are tiny, so the total fits a generous cap.
	bigTool := strings.Repeat("tool output token ", 5000) // ~lots of tokens
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{Seq: 2, Role: llm.RoleUser, Content: "go"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "ok", ToolCalls: toolCallsJSON(t, "call_a")},
		{Seq: 4, Role: llm.RoleTool, Content: bigTool, ToolCallID: "call_a", ContentSidecarPath: "/run/sidecar/call_a"},
		{Seq: 20, Role: llm.RoleUser, Content: "more"},
	}
	cfg := ContextConfig{ContextWindow: 50000, MaxOutputTokens: 1000, ToolEvictAfterTurns: 10}
	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(emit.calls) != 0 {
		t.Errorf("SC-1 violated: L1 alone must write ZERO rot events, got %d", len(emit.calls))
	}
	if len(msgs) != 5 {
		t.Errorf("L1 must not drop turns, got %d", len(msgs))
	}
	if !strings.Contains(msgs[3].Content, "read_tool_output") {
		t.Errorf("the old tool turn should be evicted by L1, got %q", msgs[3].Content)
	}
}

func TestLadder_L1_CompactedOrphanToolPointerStaysModelVisible(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}

	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{Seq: 2, Role: llm.RoleUser, Content: "go"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "ok"},
		{Seq: 4, Role: llm.RoleTool, Content: strings.Repeat("tool output token ", 5000), ToolCallID: "call_a", ContentSidecarPath: "/run/sidecar/call_a"},
		{Seq: 20, Role: llm.RoleUser, Content: "more"},
	}
	cfg := ContextConfig{ContextWindow: 50000, MaxOutputTokens: 1000, ToolEvictAfterTurns: 10}
	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("L1 compacted orphan pointer should remain visible, got %d messages: %+v", len(msgs), msgs)
	}
	if msgs[3].Role == llm.RoleTool {
		t.Fatalf("compacted orphan pointer must not remain an orphan tool role: %+v", msgs[3])
	}
	if !strings.Contains(msgs[3].Content, "read_tool_output") {
		t.Fatalf("old tool output pointer missing from managed history: %+v", msgs[3])
	}
}

// TestLadder_L2_5_DropsOldestPair_WritesOneRotEvent: history past the hard cap (and
// NOT reducible by L1 alone, because the bloat is user/assistant text) drops the
// oldest pair, writes exactly one rot event, and leaves the non-system remainder even.
func TestLadder_L2_5_DropsOldestPair_WritesOneRotEvent(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}

	big := strings.Repeat("word ", 3000) // bloat that L1 cannot touch (not tool turns)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: "recent q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "recent a"},
	}
	// hard_cap small enough to require dropping the first pair but keep the second.
	cfg := ContextConfig{ContextWindow: 20000 + 13000 + 4000, MaxOutputTokens: 1, ToolEvictAfterTurns: 100}
	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(emit.calls) != 1 {
		t.Fatalf("L2.5 must write exactly one rot event, got %d", len(emit.calls))
	}
	if emit.calls[0].pairsDropped < 1 {
		t.Errorf("pairs_dropped must be >= 1, got %d", emit.calls[0].pairsDropped)
	}
	if emit.calls[0].before <= emit.calls[0].aft {
		t.Errorf("tokens_before (%d) must exceed tokens_after (%d)", emit.calls[0].before, emit.calls[0].aft)
	}
	// System turn preserved; non-system remainder even.
	if msgs[0].Role != llm.RoleSystem {
		t.Errorf("system L0 must be preserved, got role %q", msgs[0].Role)
	}
	if (len(msgs)-1)%2 != 0 {
		t.Errorf("non-system remainder must be even, got %d non-system msgs", len(msgs)-1)
	}
}

// TestLadder_OverHardCap_Unreducible_ReturnsError: when only the system turn (or a
// single oversized turn) remains, L2.5 cannot reduce → ErrContextWindowExceeded.
func TestLadder_OverHardCap_Unreducible_ReturnsError(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}

	huge := strings.Repeat("token ", 50000) // ~50k tokens, far over any positive cap below
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: huge}, // a single, undroppable, oversized turn
	}
	// A POSITIVE hard cap (window 40000 - 20000 - 13000 = 7000) that the lone
	// system turn still blows past → L2.5 has nothing to drop → explicit error.
	cfg := ContextConfig{ContextWindow: 40000, MaxOutputTokens: 1, ToolEvictAfterTurns: 10}
	_, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err == nil || !strings.Contains(err.Error(), "aura chat new") {
		t.Fatalf("want ErrContextWindowExceeded mentioning `aura chat new`, got %v", err)
	}
	if len(emit.calls) != 0 {
		t.Errorf("an unreducible history must NOT write a rot event, got %d", len(emit.calls))
	}
}

// TestHardCap covers the L2 formula edges.
func TestHardCap(t *testing.T) {
	// MaxOutputTokens below the floor → uses 20000.
	c := ContextConfig{ContextWindow: 100000, MaxOutputTokens: 5000}
	if got := c.hardCap(); got != 100000-20000-13000 {
		t.Errorf("hardCap floor: got %d", got)
	}
	// MaxOutputTokens above the floor → uses it.
	c2 := ContextConfig{ContextWindow: 100000, MaxOutputTokens: 30000}
	if got := c2.hardCap(); got != 100000-30000-13000 {
		t.Errorf("hardCap large output: got %d", got)
	}
	// Tiny window: the formula is non-positive (1000 - 20000 - 13000 = -32000).
	// M-03 floors it to a positive ContextWindow/2 (500) instead of clamping to 0,
	// so L2.5 protection stays active on small windows (the old got==0 assertion
	// encoded the protection-OFF bug this fix closes).
	c3 := ContextConfig{ContextWindow: 1000, MaxOutputTokens: 1}
	if got := c3.hardCap(); got != 1000/2 {
		t.Errorf("hardCap small-window floor: got %d, want %d", got, 1000/2)
	}
	// A degenerate window <= 0 has no usable budget → still 0 (the only remaining
	// hardCap==0 path).
	c4 := ContextConfig{ContextWindow: 0, MaxOutputTokens: 1}
	if got := c4.hardCap(); got != 0 {
		t.Errorf("hardCap degenerate window<=0: got %d, want 0", got)
	}
	// Wave 1.10: ProviderErrorReserveTokens (llama.cpp path) is an EXTRA subtrahend
	// below the SPEC Req#10 formula; zero (the default) leaves it unchanged.
	c5 := ContextConfig{ContextWindow: 100000, MaxOutputTokens: 30000, ProviderErrorReserveTokens: 4096}
	if got := c5.hardCap(); got != 100000-30000-13000-4096 {
		t.Errorf("hardCap with provider reserve: got %d, want %d", got, 100000-30000-13000-4096)
	}
	// The reserve can push a borderline window onto the small-window floor (M-03 stays
	// active): 33000 - 20000 - 13000 = 0 before the reserve, so any reserve makes the
	// formula non-positive and it floors to ContextWindow/2, never disabling L2.5.
	c6 := ContextConfig{ContextWindow: 33000, MaxOutputTokens: 1, ProviderErrorReserveTokens: 4096}
	if got := c6.hardCap(); got != 33000/2 {
		t.Errorf("hardCap reserve-driven small-window floor: got %d, want %d", got, 33000/2)
	}
}
