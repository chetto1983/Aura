package telegramadapter

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tele "gopkg.in/telebot.v4"
)

// fakeClock returns a closure that advances `step` per call. Each call returns
// the previous time (so the *first* call returns startedAt). Lets tests pin
// every now() call deterministically without time.Sleep.
type fakeClock struct {
	t time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{t: start} }

func (c *fakeClock) now() time.Time {
	cur := c.t
	c.t = c.t.Add(10 * time.Millisecond)
	return cur
}

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// editRec captures the (text, ents) of every edit so tests can assert content
// and call counts without a real bot.
type editRec struct {
	text string
	ents tele.Entities
}

type editSink struct {
	calls []editRec
	err   error
}

func (s *editSink) fn() func(string, tele.Entities) error {
	return func(text string, ents tele.Entities) error {
		if s.err != nil {
			return s.err
		}
		s.calls = append(s.calls, editRec{text: text, ents: ents})
		return nil
	}
}

// --- Compose tests (Layouts A/B/C/D/E) ---

func TestStatusPane_LayoutA_AllRunning(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	p.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolStart("c2", "search_memory", []string{"query", "top_k"})
	clk.advance(editThrottle)
	p.OnToolStart("c3", "execute_shell", []string{"command"})

	if got, want := len(sink.calls), 3; got != want {
		t.Fatalf("edit count = %d, want %d (each OnToolStart past the throttle should edit)", got, want)
	}
	body := sink.calls[len(sink.calls)-1].text
	wantLines := []string{
		"🛠 Sto lavorando…",
		"▸ 3 strumenti in corso · web_search · search_memory · execute_shell",
		"🟡 web_search (args: query)",
		"🟡 search_memory (args: query, top_k)",
		"🟡 execute_shell (args: command)",
	}
	if got, want := body, strings.Join(wantLines, "\n"); got != want {
		t.Fatalf("Layout A body mismatch:\ngot:\n%s\n---\nwant:\n%s", got, want)
	}
	// Blockquote must cover lines 2..end (everything after the header).
	if len(sink.calls[len(sink.calls)-1].ents) != 1 {
		t.Fatalf("expected exactly 1 entity (expandable_blockquote), got %d", len(sink.calls[len(sink.calls)-1].ents))
	}
	ent := sink.calls[len(sink.calls)-1].ents[0]
	if ent.Type != tele.EntityEBlockquote {
		t.Fatalf("entity type = %q, want expandable_blockquote", ent.Type)
	}
	// header "🛠 Sto lavorando…\n" — UTF-16: "🛠" (surrogate pair, 2 units) + " Sto lavorando…" (15) + "\n" (1) = 18.
	if ent.Offset != 18 {
		t.Fatalf("blockquote offset = %d, want 18 (past header+newline)", ent.Offset)
	}
}

func TestStatusPane_LayoutA_SingleTool(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	p.OnToolStart("c1", "search_memory", []string{"query"})

	body := sink.calls[len(sink.calls)-1].text
	if !strings.Contains(body, "▸ search_memory in corso") {
		t.Fatalf("single-tool summary mismatch:\n%s", body)
	}
}

func TestStatusPane_LayoutB_Mixed(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	p.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolStart("c2", "search_memory", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolStart("c3", "execute_shell", []string{"command"})
	clk.advance(editThrottle)
	p.OnToolEnd("c1", true, 1200*time.Millisecond, "")

	body := sink.calls[len(sink.calls)-1].text
	wantLines := []string{
		"🛠 Sto lavorando…",
		"▸ 3 strumenti · 1 fatto · web_search · search_memory · execute_shell",
		"✅ web_search (1.2s)",
		"🟡 search_memory (args: query)",
		"🟡 execute_shell (args: command)",
	}
	if got, want := body, strings.Join(wantLines, "\n"); got != want {
		t.Fatalf("Layout B body mismatch:\ngot:\n%s\n---\nwant:\n%s", got, want)
	}
}

func TestStatusPane_LayoutC_RoundArchived(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	// Round 1: web_search done.
	p.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolEnd("c1", true, 1400*time.Millisecond, "")
	clk.advance(editThrottle)
	// Round 2: search_memory + web_fetch running.
	p.OnToolStart("c2", "search_memory", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolStart("c3", "web_fetch", []string{"url"})

	body := sink.calls[len(sink.calls)-1].text
	wantLines := []string{
		"🛠 Sto lavorando…",
		"▸ round 2 · 2 strumenti in corso · search_memory · web_fetch",
		"🟡 search_memory (args: query)",
		"🟡 web_fetch (args: url)",
		"— round 1 (1400ms): ✅ web_search",
	}
	if got, want := body, strings.Join(wantLines, "\n"); got != want {
		t.Fatalf("Layout C body mismatch:\ngot:\n%s\n---\nwant:\n%s", got, want)
	}
}

func TestStatusPane_LayoutD_ContentModeFooter(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	// 2 rounds, 4 tools total.
	p.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolStart("c2", "search_memory", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolEnd("c1", true, 1500*time.Millisecond, "")
	p.OnToolEnd("c2", true, 800*time.Millisecond, "")
	p.OnToolStart("c3", "web_fetch", []string{"url"})
	clk.advance(editThrottle)
	p.OnToolStart("c4", "read_skill", []string{"name"})
	clk.advance(editThrottle)
	p.OnToolEnd("c3", true, 600*time.Millisecond, "")
	p.OnToolEnd("c4", true, 200*time.Millisecond, "")

	// Advance the simulated wall clock to a known footer-elapsed value.
	clk.t = time.Unix(1_700_000_000, 0).Add(2800 * time.Millisecond)
	p.EnterContentMode()

	footerText, footerEnts := p.Footer()
	wantFooter := "🛠 4 strumenti usati in 2 round · 2.8s"
	if footerText != wantFooter {
		t.Fatalf("Footer text = %q, want %q", footerText, wantFooter)
	}
	if len(footerEnts) != 1 || footerEnts[0].Type != tele.EntityItalic {
		t.Fatalf("footer entity = %+v, want single italic", footerEnts)
	}
	// EnterContentMode must suppress further edits.
	editsBefore := len(sink.calls)
	clk.advance(editThrottle)
	p.OnToolStart("c5", "web_search", nil) // should be ignored — contentMode is set
	if len(sink.calls) != editsBefore {
		t.Fatalf("edit was emitted after EnterContentMode (calls before=%d, after=%d)", editsBefore, len(sink.calls))
	}
}

func TestStatusPane_LayoutE_Failure(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	p.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	p.OnToolStart("c2", "execute_shell", []string{"command"})
	clk.advance(editThrottle)
	p.OnToolEnd("c1", true, 900*time.Millisecond, "")
	clk.advance(editThrottle)
	p.OnToolEnd("c2", false, 50*time.Millisecond, "Error: shell command failed (exit=127): no such command\nstack...")

	body := sink.calls[len(sink.calls)-1].text
	wantLines := []string{
		"🛠 Sto lavorando…",
		"▸ round 2 · in corso",
		"— round 1 (900ms): ✅ web_search · ❌ execute_shell",
	}
	if got, want := body, strings.Join(wantLines, "\n"); got != want {
		t.Fatalf("Layout E body mismatch:\ngot:\n%s\n---\nwant:\n%s", got, want)
	}
}

// --- Throttle / coalescing ---

func TestStatusPane_CoalescesWithinThrottleWindow(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	// 4 parallel OnToolStart events with no clock advance between them
	// (all within the same throttle window).
	p.OnToolStart("c1", "web_search", []string{"query"})
	p.OnToolStart("c2", "search_memory", []string{"query"})
	p.OnToolStart("c3", "read_skill", []string{"name"})
	p.OnToolStart("c4", "read_memory", []string{"id"})

	// First OnToolStart edited (lastEdit was zero); the next 3 fall inside
	// the throttle window → coalesced → no edit.
	if got, want := len(sink.calls), 1; got != want {
		t.Fatalf("expected exactly 1 edit (3 coalesced), got %d", got)
	}
}

func TestStatusPane_NotModifiedGuard(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	p.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	// Replay the same state — body will be identical → no edit.
	// Trigger a flush by calling MarkEdited (resets lastEdit) and another
	// neutral op. Easiest: directly drive flushLocked via a second start
	// that we then revert; instead use an internal trick by simulating an
	// event that produces the same body. Here we just call OnToolStart with
	// the same ID — produces a different state. Skip and assert directly.

	// Inline assertion: re-invoke composeLocked twice; body equal → second
	// flushLocked must not edit. Simulate via private helper sequence:
	p.mu.Lock()
	_, _ = p.composeLocked() // no state mutation
	editsBefore := len(sink.calls)
	// Force a flush attempt by clearing lastEdit (within mutex).
	p.lastEdit = time.Time{}
	p.flushLocked()
	p.mu.Unlock()
	if len(sink.calls) != editsBefore {
		t.Fatalf("flushLocked emitted edit with identical body (calls before=%d after=%d)", editsBefore, len(sink.calls))
	}
}

// --- Edge cases ---

func TestStatusPane_LongToolNamesTruncate(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	for i, name := range []string{
		"mcp_github__create_issue",
		"mcp_github__list_repos",
		"mcp_github__add_comment",
		"mcp_github__close_issue",
	} {
		p.OnToolStart(string(rune('a'+i)), name, []string{"arg"})
		clk.advance(editThrottle)
	}

	body := sink.calls[len(sink.calls)-1].text
	if !strings.Contains(body, "… 1 altri") {
		t.Fatalf("expected '… 1 altri' truncation for 4-tool collapsed line, got:\n%s", body)
	}
}

func TestStatusPane_PreviewSanitizedToFirstLineAndLength(t *testing.T) {
	long := strings.Repeat("x", 200)
	multiline := "first line is fine\nsecond should be dropped\n" + long
	got := sanitizePreview(multiline)
	if got != "first line is fine" {
		t.Fatalf("sanitizePreview multiline = %q, want first line only", got)
	}
	got = sanitizePreview(long)
	if len([]rune(got)) != statusPreviewMaxLen {
		t.Fatalf("sanitizePreview long: len=%d, want %d", len([]rune(got)), statusPreviewMaxLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("sanitizePreview long should end with ellipsis: %q", got)
	}
	if sanitizePreview("   ") != "" {
		t.Fatalf("sanitizePreview whitespace-only should return empty")
	}
}

func TestStatusPane_NoToolsNoEdit(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())
	// No events fire — pane should never edit.
	if len(sink.calls) != 0 {
		t.Fatalf("pane edited without any events: %d", len(sink.calls))
	}
	footer, _ := p.Footer()
	if footer != "" {
		t.Fatalf("Footer with zero tools = %q, want empty", footer)
	}
}

func TestStatusPane_FinalizeMarksOrphansFailed(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	sink := &editSink{}
	p := newStatusPane(clk.now, sink.fn())

	p.OnToolStart("c1", "web_search", []string{"query"})
	clk.advance(editThrottle)
	// c1 never gets OnToolEnd.
	p.Finalize()

	// Footer should now report 1 tool + 1 round (the orphan was archived).
	clk.advance(time.Second)
	footer, _ := p.Footer()
	if !strings.Contains(footer, "1 strumento usato in 1 round") {
		t.Fatalf("Finalize did not archive orphan: footer=%q", footer)
	}
	// Subsequent OnToolStart after Finalize is a no-op.
	editsBefore := len(sink.calls)
	clk.advance(editThrottle)
	p.OnToolStart("c2", "web_fetch", []string{"url"})
	if len(sink.calls) != editsBefore {
		t.Fatalf("Finalize did not block further edits")
	}
}

func TestStatusPane_ConcurrentSafeUnderRace(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	var edits atomic.Int64
	editFn := func(text string, ents tele.Entities) error {
		edits.Add(1)
		return nil
	}
	p := newStatusPane(clk.now, editFn)

	done := make(chan struct{}, 4)
	for i := range 4 {
		go func() {
			defer func() { done <- struct{}{} }()
			id := string(rune('a' + i))
			p.OnToolStart(id, "web_search", []string{"query"})
			p.OnToolEnd(id, true, 500*time.Millisecond, "")
		}()
	}
	for range 4 {
		<-done
	}
	// We don't assert exact edit count under race (throttle is wall-clock
	// based). Just confirm no panic / no data race (run with -race).
}
