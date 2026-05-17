package telegramadapter

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// fakeBotCall is one captured Bot API request body — we only need text for the
// status-pane E2E (entities are independently covered by the fixture package).
type fakeBotCall struct {
	method string
	text   string
}

// E2E: tools fire while the LLM streams content. Verify
//   - status-pane edits land BEFORE the first narrative token
//   - footer is prepended to every streaming edit once content arrives
//   - footer is DROPPED from the final edit (clean answer for the user)
func TestStatusPane_E2E_FooterPrependedDuringStreamThenStrippedOnFinal(t *testing.T) {
	calls, srv := newFakeBotServer(t)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatalf("tele.NewBot: %v", err)
	}

	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "test",
	}})
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}

	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	pane := newStatusPane(clk.now, func(text string, ents tele.Entities) error {
		_, e := tb.Edit(placeholder, text, ents)
		return e
	})

	// Simulate the agent runtime firing the same EventToolStart /
	// EventToolEnd that invocation_builder forwards to the pane.
	pane.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	pane.OnToolEnd("c1", true, 1500*time.Millisecond, "")

	// Advance the clock so FooterMarkdown reports a known elapsed.
	clk.t = time.Unix(1_700_000_000, 0).Add(2300 * time.Millisecond)

	// Build a token stream long enough to cross streamingMinThreshold so
	// Outbound triggers a content flush (calls EnterContentMode + prepends
	// the footer). Then Done with no tool calls → final clean edit.
	content := "Ho cercato la guida fiscale italiana e la scadenza per la dichiarazione 2026 è fissata al 30 novembre."
	tokens := []llm.Token{
		{Content: content},
		{Done: true},
	}
	ch := make(chan llm.Token, len(tokens))
	for _, tok := range tokens {
		ch <- tok
	}
	close(ch)

	out := NewOutbound(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	resp, delivered, err := out.ConsumeStream(ctx, ch, "123", placeholder, pane)
	if err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}
	if !delivered {
		t.Fatalf("delivered = false, want true (content was non-empty, no tool calls)")
	}
	if !strings.Contains(resp.Content, "dichiarazione 2026") {
		t.Fatalf("resp.Content lost the narrative payload: %q", resp.Content)
	}

	editCalls := filterCalls(calls.snapshot(), "editMessageText")
	if len(editCalls) < 2 {
		t.Fatalf("expected at least 2 edits (status-pane + content + final), got %d:\n%s",
			len(editCalls), formatCalls(editCalls))
	}

	// First edit body should be the status pane (header + blockquote line).
	first := editCalls[0].text
	if !strings.Contains(first, "🛠 Sto lavorando…") {
		t.Fatalf("first edit was not the status pane header:\n%s\n---all edits:---\n%s",
			first, formatCalls(editCalls))
	}
	if !strings.Contains(first, "web_search") {
		t.Fatalf("first edit lost the tool name:\n%s", first)
	}

	// The FINAL edit must contain the answer text and must NOT contain the
	// status header — the footer is stripped on the clean-answer edit.
	last := editCalls[len(editCalls)-1].text
	if !strings.Contains(last, "dichiarazione 2026") {
		t.Fatalf("final edit lost the answer:\n%s", last)
	}
	if strings.Contains(last, "🛠") {
		t.Fatalf("final edit still carries the status pane (must be stripped on final):\n%s", last)
	}
	if strings.Contains(last, "strumenti usati") {
		t.Fatalf("final edit still carries the footer text (must be stripped on final):\n%s", last)
	}

	// At least one mid-stream edit between first and last must have carried
	// the footer prefix — that's the live "🛠 N strumenti usati …" signal
	// the user sees while content is streaming.
	footerFound := false
	for _, ed := range editCalls[:len(editCalls)-1] {
		if strings.Contains(ed.text, "strumento usato") || strings.Contains(ed.text, "strumenti usati") {
			footerFound = true
			break
		}
	}
	if !footerFound {
		t.Fatalf("no mid-stream edit carried the FooterMarkdown prefix:\n%s", formatCalls(editCalls))
	}
}

// REGRESSION (the "non funziona" bug shipped in 782df120, fixed today):
// Reasoning LLMs (GPT-5, DeepSeek-R1, OpenRouter routed) emit Reasoning tokens
// BEFORE tool_calls land. The original Outbound.flush() treated reasoning as
// content, called pane.EnterContentMode() prematurely, and locked the pane
// out of every subsequent OnToolStart. After the fix:
//   - Outbound does NOT call EnterContentMode on reasoning-only flushes (with
//     a pane attached)
//   - Pane renders reasoning via the cotProvider injection
//   - When the tool dispatches after reasoning, the tool name appears in
//     subsequent pane edits (OnToolStart no longer guarded by contentMode)
//   - When narrative content eventually arrives, Outbound takes over and
//     prepends FooterMarkdown — which is non-empty because the tool entry
//     was actually recorded
func TestStatusPane_E2E_ReasoningBeforeToolDoesNotHijackPlaceholder(t *testing.T) {
	calls, srv := newFakeBotServer(t)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatalf("tele.NewBot: %v", err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "test",
	}})
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}

	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	pane := newStatusPane(clk.now, func(text string, ents tele.Entities) error {
		_, e := tb.Edit(placeholder, text, ents)
		return e
	})

	// --- Round 1: reasoning lands, then the LLM emits tool_calls on Done ---
	// Two reasoning tokens crossing the streamingReasoningMinThreshold so the
	// OLD code would have called EnterContentMode on the first one.
	round1Tokens := []llm.Token{
		{Reasoning: "Devo cercare in memoria"},
		{Reasoning: " informazioni su phantom guard prima di rispondere"},
		{Done: true, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_memory"}}},
	}
	ch1 := make(chan llm.Token, len(round1Tokens))
	for _, tok := range round1Tokens {
		ch1 <- tok
	}
	close(ch1)

	out := NewOutbound(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	clk.advance(editThrottle) // allow first edit through
	resp1, delivered1, err := out.ConsumeStream(ctx, ch1, "123", placeholder, pane)
	if err != nil {
		t.Fatalf("round 1 ConsumeStream: %v", err)
	}
	if delivered1 {
		t.Fatalf("round 1 delivered = true, want false (no content was sent)")
	}
	if !resp1.HasToolCalls {
		t.Fatalf("round 1 should report HasToolCalls=true")
	}

	// Simulate the agent loop forwarding tool events (as invocation_builder
	// does via OnEvent).
	clk.advance(editThrottle)
	pane.OnToolStart("c1", "search_memory", []string{"query"})
	clk.advance(editThrottle)
	pane.OnToolEnd("c1", true, 600*time.Millisecond, "")

	// --- Round 2: narrative content ---
	round2Tokens := []llm.Token{
		{Content: "Phantom guard è la protezione anti-allucinazione di Aura. Verifica che il modello non confermi tool che non ha realmente chiamato."},
		{Done: true},
	}
	ch2 := make(chan llm.Token, len(round2Tokens))
	for _, tok := range round2Tokens {
		ch2 <- tok
	}
	close(ch2)
	clk.t = time.Unix(1_700_000_000, 0).Add(2_500 * time.Millisecond)
	resp2, delivered2, err := out.ConsumeStream(ctx, ch2, "123", placeholder, pane)
	if err != nil {
		t.Fatalf("round 2 ConsumeStream: %v", err)
	}
	if !delivered2 {
		t.Fatalf("round 2 delivered = false, want true")
	}
	if !strings.Contains(resp2.Content, "Phantom guard") {
		t.Fatalf("round 2 lost the answer text: %q", resp2.Content)
	}

	editCalls := filterCalls(calls.snapshot(), "editMessageText")
	if len(editCalls) < 3 {
		t.Fatalf("expected ≥3 edits across both rounds, got %d:\n%s",
			len(editCalls), formatCalls(editCalls))
	}

	// Assertion 1: reasoning text reached the placeholder during round 1
	// (some chunk — throttle may cut off later chunks before Done arrives;
	// that's fine — the user still sees Aura is reasoning).
	reasoningSeen := false
	for _, ed := range editCalls {
		if strings.Contains(ed.text, "🧠") && strings.Contains(ed.text, "Devo cercare") {
			reasoningSeen = true
			break
		}
	}
	if !reasoningSeen {
		t.Fatalf("reasoning text never reached the placeholder (cotProvider didn't fire):\n%s",
			formatCalls(editCalls))
	}

	// Assertion 1b: the status header showed up AFTER reasoning — this is
	// the key fix. Pre-fix, EnterContentMode would have locked the pane
	// out before any OnToolStart could land, so this header would never
	// appear and the user would see only the reasoning text.
	statusHeaderSeen := false
	for _, ed := range editCalls {
		if strings.Contains(ed.text, "🛠 Sto lavorando") {
			statusHeaderSeen = true
			break
		}
	}
	if !statusHeaderSeen {
		t.Fatalf("status pane header never appeared (OLD bug — pane hijacked by reasoning flush):\n%s",
			formatCalls(editCalls))
	}

	// Assertion 2: tool name must appear in at least one edit — proves
	// OnToolStart's append wasn't dropped by the contentMode guard.
	toolNameFound := false
	for _, ed := range editCalls {
		if strings.Contains(ed.text, "search_memory") {
			toolNameFound = true
			break
		}
	}
	if !toolNameFound {
		t.Fatalf("tool name never appeared — OnToolStart was hijacked by contentMode guard:\n%s",
			formatCalls(editCalls))
	}

	// Assertion 3: at least one mid-stream edit in round 2 must carry the
	// footer ("strumento usato") — proves roundHistory was populated
	// because OnToolStart actually appended the entry.
	footerSeen := false
	for _, ed := range editCalls[:len(editCalls)-1] {
		if strings.Contains(ed.text, "strumento usato") {
			footerSeen = true
			break
		}
	}
	if !footerSeen {
		t.Fatalf("footer never appeared mid-stream — roundHistory empty? OnToolStart silently dropped?\n%s",
			formatCalls(editCalls))
	}

	// Assertion 4: final edit is clean (no 🧠, no 🛠, no footer).
	last := editCalls[len(editCalls)-1].text
	if !strings.Contains(last, "Phantom guard") {
		t.Fatalf("final edit lost the answer:\n%s", last)
	}
	if strings.Contains(last, "🧠") || strings.Contains(last, "🛠") || strings.Contains(last, "strumento usato") {
		t.Fatalf("final edit still carries chrome (should be clean answer):\n%s", last)
	}
}

// REGRESSION ("CoT a volte si perde sulle conversazioni lunghe", reported 2026-05-17):
// When round 1 emits content >= 30 chars + tool calls, Outbound calls
// EnterContentMode and the pane sets contentMode=true. Before the
// ResetForNewRound fix, that state persisted for the rest of the turn —
// round 2's reasoning would call pane.Refresh() but flushLocked early-
// returned on contentMode, so the user saw round-1 content frozen on
// screen during round-2's reasoning streaming.
//
// After the fix: every ConsumeStream entry calls pane.ResetForNewRound(),
// so round 2's reasoning IS visible to the user via the pane.
func TestStatusPane_E2E_MultiRoundContentDoesNotHideLaterReasoning(t *testing.T) {
	calls, srv := newFakeBotServer(t)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatalf("tele.NewBot: %v", err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "test",
	}})
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}

	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	pane := newStatusPane(clk.now, func(text string, ents tele.Entities) error {
		_, e := tb.Edit(placeholder, text, ents)
		return e
	})
	out := NewOutbound(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// --- Round 1: reasoning + content (>=30 chars) + tool calls.
	// The content forces Outbound to call EnterContentMode, which BEFORE
	// the fix would lock the pane out forever.
	round1 := []llm.Token{
		{Reasoning: "Cerco subito in memoria"},
		{Content: "Sto cercando informazioni rilevanti nella memoria, attendi..."},
		{Done: true, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "search_memory"}}},
	}
	ch1 := make(chan llm.Token, len(round1))
	for _, t := range round1 {
		ch1 <- t
	}
	close(ch1)
	clk.advance(editThrottle)
	resp1, _, err := out.ConsumeStream(ctx, ch1, "123", placeholder, pane)
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if !resp1.HasToolCalls {
		t.Fatalf("round 1 should have tool calls")
	}

	// Agent loop forwards tool events.
	clk.advance(editThrottle)
	pane.OnToolStart("c1", "search_memory", []string{"query"})
	clk.advance(editThrottle)
	pane.OnToolEnd("c1", true, 500*time.Millisecond, "")

	// --- Round 2: reasoning ONLY (long enough to matter to the user),
	// followed by tool calls again. The user is supposed to see the new
	// reasoning during this round.
	round2 := []llm.Token{
		{Reasoning: "Adesso analizzo i risultati della ricerca per capire cosa rispondere"},
		{Reasoning: " — sembra ci sia una pagina rilevante, vediamo se serve fetcharla"},
		{Done: true, ToolCalls: []llm.ToolCall{{ID: "c2", Name: "web_fetch"}}},
	}
	ch2 := make(chan llm.Token, len(round2))
	for _, t := range round2 {
		ch2 <- t
	}
	close(ch2)
	clk.advance(editThrottle)
	resp2, _, err := out.ConsumeStream(ctx, ch2, "123", placeholder, pane)
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if !resp2.HasToolCalls {
		t.Fatalf("round 2 should have tool calls")
	}
	clk.advance(editThrottle)
	pane.OnToolStart("c2", "web_fetch", []string{"url"})
	clk.advance(editThrottle)
	pane.OnToolEnd("c2", true, 800*time.Millisecond, "")

	// --- Round 3: final clean answer.
	round3 := []llm.Token{
		{Content: "Risposta finale all'utente, il fetch ha rivelato che la pagina contiene la spiegazione richiesta."},
		{Done: true},
	}
	ch3 := make(chan llm.Token, len(round3))
	for _, t := range round3 {
		ch3 <- t
	}
	close(ch3)
	clk.advance(editThrottle)
	_, _, err = out.ConsumeStream(ctx, ch3, "123", placeholder, pane)
	if err != nil {
		t.Fatalf("round 3: %v", err)
	}

	editCalls := filterCalls(calls.snapshot(), "editMessageText")
	if len(editCalls) < 4 {
		t.Fatalf("expected ≥4 edits across 3 rounds, got %d:\n%s",
			len(editCalls), formatCalls(editCalls))
	}

	// THE KEY ASSERTION: round 2's reasoning must appear in at least one
	// edit. Pre-fix, this was invisible because pane was locked into
	// contentMode after round 1's content flush.
	round2ReasoningSeen := false
	for _, ed := range editCalls {
		if strings.Contains(ed.text, "Adesso analizzo i risultati") {
			round2ReasoningSeen = true
			break
		}
	}
	if !round2ReasoningSeen {
		t.Fatalf("round 2 reasoning was lost (THE bug — contentMode sticky across rounds):\n%s",
			formatCalls(editCalls))
	}

	// The tool name from round 2 should also surface.
	round2ToolSeen := false
	for _, ed := range editCalls {
		if strings.Contains(ed.text, "web_fetch") {
			round2ToolSeen = true
			break
		}
	}
	if !round2ToolSeen {
		t.Fatalf("round 2 tool name 'web_fetch' never appeared:\n%s", formatCalls(editCalls))
	}

	// Footer should report BOTH rounds worth of tools in the final
	// streaming edit.
	footerSeen := false
	for _, ed := range editCalls {
		if strings.Contains(ed.text, "2 strumenti usati") && strings.Contains(ed.text, "2 round") {
			footerSeen = true
			break
		}
	}
	if !footerSeen {
		t.Fatalf("footer never reported 2 strumenti usati in 2 round:\n%s", formatCalls(editCalls))
	}

	// Final edit is clean.
	last := editCalls[len(editCalls)-1].text
	for _, chrome := range []string{"Sto lavorando", "strumento usato", "strumenti usati", "in corso"} {
		if strings.Contains(last, chrome) {
			t.Fatalf("final edit still carries %q:\n%s", chrome, last)
		}
	}
}

// E2E: a tool-only LLM turn (no content emitted) must NOT call EnterContentMode
// — the pane retains ownership of the placeholder so the user sees the tool
// status, not a silent ⏳.
func TestStatusPane_E2E_ToolOnlyTurnLeavesPaneInControl(t *testing.T) {
	calls, srv := newFakeBotServer(t)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatalf("tele.NewBot: %v", err)
	}

	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "test",
	}})
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}

	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	pane := newStatusPane(clk.now, func(text string, ents tele.Entities) error {
		_, e := tb.Edit(placeholder, text, ents)
		return e
	})

	// One tool fires, then the LLM ends the turn with a tool call (no content).
	pane.OnToolStart("c1", "execute_shell", []string{"command"})
	clk.advance(editThrottle)

	tokens := []llm.Token{
		{Done: true, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "execute_shell"}}},
	}
	ch := make(chan llm.Token, len(tokens))
	for _, tok := range tokens {
		ch <- tok
	}
	close(ch)

	out := NewOutbound(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	resp, delivered, err := out.ConsumeStream(ctx, ch, "123", placeholder, pane)
	if err != nil {
		t.Fatalf("ConsumeStream: %v", err)
	}
	if delivered {
		t.Fatalf("delivered = true on tool-only turn, want false (no content was sent)")
	}
	if !resp.HasToolCalls {
		t.Fatalf("expected HasToolCalls true")
	}

	// The only edit should be the pane's own (no content flush happened).
	editCalls := filterCalls(calls.snapshot(), "editMessageText")
	if len(editCalls) != 1 {
		t.Fatalf("expected exactly 1 edit (pane only — no content flush), got %d:\n%s",
			len(editCalls), formatCalls(editCalls))
	}
	if !strings.Contains(editCalls[0].text, "execute_shell") {
		t.Fatalf("pane edit missing tool name:\n%s", editCalls[0].text)
	}
}

// --- helpers ---

type callLog struct {
	mu sync.Mutex
	v  []fakeBotCall
}

func (c *callLog) append(call fakeBotCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v = append(c.v, call)
}

func (c *callLog) snapshot() []fakeBotCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]fakeBotCall, len(c.v))
	copy(out, c.v)
	return out
}

func newFakeBotServer(t *testing.T) (*callLog, *httptest.Server) {
	t.Helper()
	log := &callLog{}
	var seq int
	var seqMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := pathBase(r.URL.Path)
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		text := extractTextFromBody(bodyBytes)
		log.append(fakeBotCall{method: method, text: text})

		seqMu.Lock()
		seq++
		id := seq
		seqMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"ok":true,"result":{"message_id":%d,"chat":{"id":123},"date":1760000000,"text":"ok"}}`,
			id,
		)
	}))
	return log, srv
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func filterCalls(calls []fakeBotCall, method string) []fakeBotCall {
	out := make([]fakeBotCall, 0, len(calls))
	for _, c := range calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// extractTextFromBody pulls the `text` field out of a telebot Bot API request
// body. Telebot v4 in this version sends application/json for text-only calls
// (no files attached), so a single json.Unmarshal is enough.
func extractTextFromBody(body []byte) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	s, _ := m["text"].(string)
	return s
}

func formatCalls(calls []fakeBotCall) string {
	var sb strings.Builder
	for i, c := range calls {
		fmt.Fprintf(&sb, "  [%d] %s: %q\n", i, c.method, c.text)
	}
	return sb.String()
}
