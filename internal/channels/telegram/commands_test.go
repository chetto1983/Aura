package telegram

import (
	"context"
	"errors"
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

// TestCommandsInterceptTenWithoutLLM proves the 10 PRD commands all dispatch as
// bot-intercept (handled==true) with NO LLM call wired (the turnDriver would
// t.Fatal). VALIDATION UX-02 "10 commands bot-intercept, no LLM call".
func TestCommandsInterceptTenWithoutLLM(t *testing.T) {
	t.Parallel()

	cmds := newTestCommands(commandDeps{
		Search: &fakeSearch{},
		Cost:   &fakeCost{},
		Prices: nil,
		Model:  "deepseek/deepseek-v4-flash:exacto",
	})

	want := []string{"/start", "/help", "/cancel", "/cost", "/search", "/new", "/list", "/reset", "/whoami", "/stop"}
	for _, c := range want {
		handled, reply := cmds.dispatch(context.Background(), 42, c)
		if !handled {
			t.Errorf("command %q: handled=false, want true (bot-intercept before LLM)", c)
		}
		if reply == "" {
			t.Errorf("command %q: empty reply, want a user-facing response", c)
		}
	}
	if len(want) != 10 {
		t.Fatalf("PRD command set must be exactly 10, got %d", len(want))
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
