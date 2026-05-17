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
