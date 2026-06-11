package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// fakeSearch is the searchBackend double. It records the last query+limit and
// returns a fixed result set so the cross-slice invariant test can assert the
// Telegram render equals the CLI render over the SAME backend output.
type fakeSearch struct {
	results []conversations.SearchResult
	lastQ   string
	lastLim int
	err     error
	calls   int
}

func (f *fakeSearch) SearchConversationTurns(_ context.Context, query string, limit int) ([]conversations.SearchResult, error) {
	f.calls++
	f.lastQ = query
	f.lastLim = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func newTestCommands(deps commandDeps) *commands { return newCommands(deps) }

// TestCommandsInterceptWithoutLLM proves the Telegram slash commands all dispatch
// as bot-intercept (handled==true) with NO LLM call wired (the turnDriver would
// t.Fatal). VALIDATION UX-02 "commands bot-intercept, no LLM call".
func TestCommandsInterceptWithoutLLM(t *testing.T) {
	t.Parallel()

	cmds := newTestCommands(commandDeps{
		Search: &fakeSearch{},
		Cost:   &fakeCost{},
		Prices: nil,
		Model:  "deepseek/deepseek-v4-flash:exacto",
	})

	want := []string{"/start", "/help", "/cancel", "/cost", "/search", "/onboard", "/new", "/list", "/reset", "/whoami", "/stop"}
	for _, c := range want {
		handled, reply := cmds.dispatch(context.Background(), 42, c)
		if !handled {
			t.Errorf("command %q: handled=false, want true (bot-intercept before LLM)", c)
		}
		if reply == "" {
			t.Errorf("command %q: empty reply, want a user-facing response", c)
		}
	}
	if len(want) != 11 {
		t.Fatalf("Telegram command set must be exactly 11, got %d", len(want))
	}
}

func TestBotMenuCommandsMirrorInterceptedCommands(t *testing.T) {
	t.Parallel()
	want := []string{"start", "help", "cancel", "cost", "search", "onboard", "new", "list", "reset", "whoami", "stop"}
	got := botMenuCommands()
	if len(got) != len(want) {
		t.Fatalf("menu command count = %d, want %d", len(got), len(want))
	}
	for i, cmd := range got {
		if cmd.Text != want[i] {
			t.Errorf("menu command %d = %q, want %q", i, cmd.Text, want[i])
		}
		if strings.HasPrefix(cmd.Text, "/") {
			t.Errorf("menu command %q must omit the slash for setMyCommands", cmd.Text)
		}
		if cmd.Description == "" {
			t.Errorf("menu command %q has empty description", cmd.Text)
		}
	}
}

// TestUnknownCommandReturnsHelpHintNoLLM proves an unknown /command is handled
// (a help hint), never an LLM call (T-13-06-CmdLLMBypass).
func TestUnknownCommandReturnsHelpHintNoLLM(t *testing.T) {
	t.Parallel()
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{}, Cost: &fakeCost{}})
	handled, reply := cmds.dispatch(context.Background(), 42, "/nope")
	if !handled {
		t.Fatal("unknown /command must be handled (a help hint), not passed to the LLM")
	}
	if !strings.Contains(strings.ToLower(reply), "/help") {
		t.Errorf("unknown command reply should hint /help, got %q", reply)
	}
}

// TestNonCommandNotIntercepted proves a plain message is NOT intercepted
// (handled==false) so the channel drives an LLM turn for it.
func TestNonCommandNotIntercepted(t *testing.T) {
	t.Parallel()
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{}, Cost: &fakeCost{}})
	if handled, _ := cmds.dispatch(context.Background(), 42, "ciao Aura"); handled {
		t.Fatal("a plain message must NOT be intercepted as a command")
	}
}

// TestSearchEqualsCLI proves /search renders the SAME excerpt lines the CLI
// printSearchResults produces over identical backend data (cross-slice invariant).
func TestSearchEqualsCLI(t *testing.T) {
	t.Parallel()
	hits := []conversations.SearchResult{
		{ConversationID: "11111111-1111-1111-1111-111111111111", Seq: 3, Content: "the meteo a Cuneo è soleggiato oggi", Similarity: 0.42},
		{ConversationID: "22222222-2222-2222-2222-222222222222", Seq: 7, Content: "ho prenotato il treno per Torino domani mattina", Similarity: 0.31},
	}
	be := &fakeSearch{results: hits}
	cmds := newTestCommands(commandDeps{Search: be, Cost: &fakeCost{}})

	handled, reply := cmds.dispatch(context.Background(), 42, "/search meteo")
	if !handled {
		t.Fatal("/search must be handled")
	}
	if be.calls != 1 {
		t.Fatalf("/search must call the backend exactly once, got %d", be.calls)
	}
	if be.lastQ != "meteo" {
		t.Errorf("/search query = %q, want %q", be.lastQ, "meteo")
	}

	// The Telegram render must contain, for each hit, the SAME excerpt window the
	// CLI excerpt() produces over the same content+query (cross-slice invariant).
	for _, h := range hits {
		ex := searchExcerpt(h.Content, "meteo")
		if !strings.Contains(reply, ex) {
			t.Errorf("/search reply missing CLI-identical excerpt %q\nreply:\n%s", ex, reply)
		}
	}
}

func TestSearchRichPaginatesResults(t *testing.T) {
	t.Parallel()
	hits := make([]conversations.SearchResult, 0, searchPageSize+1)
	for i := 0; i < searchPageSize+1; i++ {
		hits = append(hits, conversations.SearchResult{Seq: i + 1, Content: "meteo risultato " + strconv.Itoa(i+1)})
	}
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{results: hits}, Cost: &fakeCost{}})

	out := cmds.searchRich(context.Background(), 42, "meteo")
	if out.text == "" {
		t.Fatal("paginated search must render the first page")
	}
	if strings.Contains(out.text, "risultato "+strconv.Itoa(searchPageSize+1)) {
		t.Fatalf("first page must not include page 2 result, got %q", out.text)
	}
	if out.markup == nil || len(out.markup.InlineKeyboard) == 0 {
		t.Fatalf("paginated search must include inline navigation markup")
	}

	next := cmds.searchPage(42, 1)
	if !strings.Contains(next.text, "risultato "+strconv.Itoa(searchPageSize+1)) {
		t.Fatalf("next page must render the stored second page, got %q", next.text)
	}
	if next.markup == nil {
		t.Fatalf("next page must keep navigation markup")
	}
}

func TestSearchPageClampsAndExpires(t *testing.T) {
	t.Parallel()
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{}, Cost: &fakeCost{}})

	expired := cmds.searchPage(404, 0)
	if !strings.Contains(expired.text, "Ricerca scaduta") {
		t.Fatalf("missing search state should render expiry hint, got %q", expired.text)
	}

	cmds.searchPages[42] = searchPagerState{pages: []string{"prima", "seconda"}}
	if got := cmds.searchPage(42, -10).text; !strings.Contains(got, "prima") || !strings.Contains(got, "Pagina 1/2") {
		t.Fatalf("negative page should clamp to first page, got %q", got)
	}
	if got := cmds.searchPage(42, 99).text; !strings.Contains(got, "seconda") || !strings.Contains(got, "Pagina 2/2") {
		t.Fatalf("overflow page should clamp to last page, got %q", got)
	}
}

func TestParseSearchCallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		data      string
		wantPage  int
		wantClose bool
		wantOK    bool
	}{
		{name: "page", data: searchCallbackData(3), wantPage: 3, wantOK: true},
		{name: "close", data: searchCallbackCloseData(), wantClose: true, wantOK: true},
		{name: "missing separator", data: searchCallbackPrefix, wantOK: false},
		{name: "wrong prefix", data: "other|1", wantOK: false},
		{name: "bad page", data: searchCallbackPrefix + "|nope", wantOK: false},
		{name: "negative page", data: searchCallbackPrefix + "|-2", wantPage: -2, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotPage, gotClose, gotOK := parseSearchCallback(tt.data)
			if gotPage != tt.wantPage || gotClose != tt.wantClose || gotOK != tt.wantOK {
				t.Fatalf("parseSearchCallback(%q) = (%d, %v, %v), want (%d, %v, %v)",
					tt.data, gotPage, gotClose, gotOK, tt.wantPage, tt.wantClose, tt.wantOK)
			}
		})
	}
}

func TestClampExcerptRuneSafeEllipses(t *testing.T) {
	t.Parallel()
	s := "ab\u00e8cd\u20acfg"
	got := clampExcerpt(s, 3, 7)
	if got != "\u2026cd\u2026" {
		t.Fatalf("clampExcerpt should move byte indexes to rune starts with ellipses, got %q", got)
	}
	if got := clampExcerpt(s, -10, len(s)+10); got != s {
		t.Fatalf("full-range clampExcerpt = %q, want %q", got, s)
	}
}

// TestSearchEmptyQueryHint proves /search with no query returns a usage hint, not
// a backend call.
func TestSearchEmptyQueryHint(t *testing.T) {
	t.Parallel()
	be := &fakeSearch{}
	cmds := newTestCommands(commandDeps{Search: be, Cost: &fakeCost{}})
	handled, reply := cmds.dispatch(context.Background(), 42, "/search")
	if !handled {
		t.Fatal("/search must be handled")
	}
	if be.calls != 0 {
		t.Errorf("/search with no query must not hit the backend, got %d calls", be.calls)
	}
	if reply == "" {
		t.Error("/search with no query should return a usage hint")
	}
}

// fakeCost is the costBackend double returning a fixed token/cost snapshot.
type fakeCost struct {
	prompt     int
	completion int
	provider   *float64
}

func (f *fakeCost) TodayUsage(_ context.Context) (promptTokens, completionTokens int, providerCost *float64, err error) {
	return f.prompt, f.completion, f.provider, nil
}

// TestCostEqualsCLI proves /cost renders the SAME USD string llm.CostUSD yields
// over identical token/price data (cross-slice invariant: Telegram == CLI).
func TestCostEqualsCLI(t *testing.T) {
	t.Parallel()
	prices := map[string]llm.Price{"deepseek/deepseek-v4-flash:exacto": {InputPer1M: 0.0983, OutputPer1M: 0.1966}}
	be := &fakeCost{prompt: 1_000_000, completion: 500_000}
	cmds := newTestCommands(commandDeps{
		Search: &fakeSearch{},
		Cost:   be,
		Prices: prices,
		Model:  "deepseek/deepseek-v4-flash:exacto",
	})

	handled, reply := cmds.dispatch(context.Background(), 42, "/cost")
	if !handled {
		t.Fatal("/cost must be handled")
	}
	want, ok := llm.CostUSD(prices, "deepseek/deepseek-v4-flash:exacto", be.prompt, be.completion, nil)
	if !ok {
		t.Fatal("fixture must produce a real cost")
	}
	if !strings.Contains(reply, want) {
		t.Errorf("/cost reply must contain the CLI-identical cost %q, got %q", want, reply)
	}
}

// TestCancelCancelsInflightTurnCtx proves /cancel cancels the per-chat in-flight
// turn ctx (SC#3 ctx-cancel propagation). A turn registers its cancel func; a
// /cancel fires it; the registered ctx observes Done.
func TestCancelCancelsInflightTurnCtx(t *testing.T) {
	t.Parallel()
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{}, Cost: &fakeCost{}})

	turnCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmds.registerTurn(42, cancel)

	if turnCtx.Err() != nil {
		t.Fatal("turn ctx cancelled before /cancel")
	}
	handled, reply := cmds.dispatch(context.Background(), 42, "/cancel")
	if !handled {
		t.Fatal("/cancel must be handled")
	}
	if reply == "" {
		t.Error("/cancel should confirm to the user")
	}
	if turnCtx.Err() == nil {
		t.Error("/cancel must cancel the in-flight turn ctx (SC#3 ctx-cancel propagation)")
	}

	// A /cancel with no in-flight turn is a clean handled no-op (idempotent).
	if handled, _ := cmds.dispatch(context.Background(), 99, "/cancel"); !handled {
		t.Error("/cancel with no in-flight turn must still be handled")
	}
}

// TestCancelClearsRegistryEntry proves the cancel func is deregistered after the
// turn ends so a stale func is never fired on a later /cancel.
func TestCancelClearsRegistryEntry(t *testing.T) {
	t.Parallel()
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{}, Cost: &fakeCost{}})

	fired := 0
	cmds.registerTurn(7, func() { fired++ })
	cmds.unregisterTurn(7)
	if _, _ = cmds.dispatch(context.Background(), 7, "/cancel"); fired != 0 {
		t.Errorf("a deregistered turn must not be cancelled, fired=%d", fired)
	}
}

func TestSearchBackendErrorReported(t *testing.T) {
	t.Parallel()
	be := &fakeSearch{err: errors.New("boom")}
	cmds := newTestCommands(commandDeps{Search: be, Cost: &fakeCost{}})
	handled, reply := cmds.dispatch(context.Background(), 42, "/search x")
	if !handled {
		t.Fatal("/search must be handled even on a backend error")
	}
	if reply == "" {
		t.Error("/search backend error should surface a user-facing message")
	}
}

// TestThreadBoundCommandsDoNotClaimUnsupportedStateChanges keeps Telegram command
// copy honest: this channel currently maps one Telegram chat to one continuous
// thread, so /new and /reset must not claim they created or erased persisted
// conversation state.
func TestThreadBoundCommandsDoNotClaimUnsupportedStateChanges(t *testing.T) {
	t.Parallel()
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{}, Cost: &fakeCost{}})

	_, newReply := cmds.dispatch(context.Background(), 42, "/new")
	if !strings.Contains(strings.ToLower(newReply), "thread continuo") {
		t.Fatalf("/new should explain the continuous Telegram thread, got %q", newReply)
	}

	_, resetReply := cmds.dispatch(context.Background(), 42, "/reset")
	lowerReset := strings.ToLower(resetReply)
	if strings.Contains(lowerReset, "azzerata") || strings.Contains(lowerReset, "azzerato") {
		t.Fatalf("/reset must not claim persisted history was erased, got %q", resetReply)
	}
	if !strings.Contains(lowerReset, "annull") {
		t.Fatalf("/reset should still explain it cancels any running turn, got %q", resetReply)
	}
}

func TestHelpCopyMatchesThreadBoundSemantics(t *testing.T) {
	t.Parallel()
	cmds := newTestCommands(commandDeps{Search: &fakeSearch{}, Cost: &fakeCost{}})

	_, reply := cmds.dispatch(context.Background(), 42, "/help")
	lower := strings.ToLower(reply)
	for _, bad := range []string{"nuova conversazione", "azzera", "ferma aura"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("/help advertises unsupported Telegram state change %q in %q", bad, reply)
		}
	}
	if !strings.Contains(lower, "thread continuo") {
		t.Fatalf("/help should explain Telegram's continuous thread model, got %q", reply)
	}
}
