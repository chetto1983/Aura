// Package telegram — this file is the command dispatcher (UX-02 / SC#3 / SC#5).
// The 10 PRD slash-commands are intercepted BEFORE any LLM dispatch (the text
// handler calls dispatch first; a handled command never reaches handleTurn, so
// a command can never drive an agent turn — T-13-06-CmdLLMBypass). /cost and
// /search REUSE the locked backends byte-for-byte (llm.CostUSD and
// conversations.SearchConversationTurns) so the Telegram output equals the CLI
// output for identical data (the cross-slice invariant). /cancel cancels the
// per-chat in-flight turn ctx (SC#3) via the cancel-func registry the channel
// tracks.
package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// searchBackend is the consumer-side seam over the locked FTS query
// (conversations.Store.SearchConversationTurns). Declaring it here (not importing
// *conversations.Store) keeps the dispatcher unit-testable with a double AND makes
// the byte-for-byte reuse explicit: /search calls THIS, never a new query.
type searchBackend interface {
	SearchConversationTurns(ctx context.Context, query string, limit int) ([]conversations.SearchResult, error)
}

// costBackend reports today's cumulative token usage so /cost can render the USD
// via the SAME llm.CostUSD the CLI footer uses (cross-slice invariant). The
// aggregation (which rows, which day) lives behind the seam; the dispatcher only
// formats. A nil providerCost defers to the price table (the unknown-model honest
// "n/a", never a fabricated $0).
type costBackend interface {
	TodayUsage(ctx context.Context) (promptTokens, completionTokens int, providerCost *float64, err error)
}

// searchResultLimit bounds the /search backend call (the CLI default).
const searchResultLimit = 20

// commandDeps are the dispatcher inputs: the two reused backends plus the price
// table + model that drive the /cost render (identical to the CLI cost footer).
type commandDeps struct {
	Search searchBackend
	Cost   costBackend
	Prices map[string]llm.Price
	Model  string
}

// commands intercepts the 10 PRD slash-commands. It also owns the per-chat
// in-flight-turn cancel registry so /cancel can abort a running turn (SC#3).
type commands struct {
	deps commandDeps

	mu      sync.Mutex
	cancels map[int64]context.CancelFunc // chatID → the in-flight turn's cancel
}

// newCommands builds a dispatcher over the supplied backends.
func newCommands(d commandDeps) *commands {
	return &commands{deps: d, cancels: make(map[int64]context.CancelFunc)}
}

// registerTurn records the in-flight turn's cancel func for a chat so a later
// /cancel can fire it. It returns false when a turn is ALREADY in flight for the
// chat — the caller must not start a second concurrent turn on the same
// conversation (it would race appendUserTurn / interleave the persisted history).
// The channel registers on turn start and unregisters on turn end.
func (c *commands) registerTurn(chatID int64, cancel context.CancelFunc) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, busy := c.cancels[chatID]; busy {
		return false
	}
	c.cancels[chatID] = cancel
	return true
}

// unregisterTurn drops a chat's in-flight cancel func (turn ended).
func (c *commands) unregisterTurn(chatID int64) {
	c.mu.Lock()
	delete(c.cancels, chatID)
	c.mu.Unlock()
}

// dispatch intercepts a slash-command before any LLM handling. It returns
// handled=true (and the user-facing reply) for every /command — including an
// unknown one (a /help hint) — so a command NEVER reaches handleTurn. A
// non-command message returns handled=false; the caller then drives an LLM turn.
func (c *commands) dispatch(ctx context.Context, chatID int64, text string) (handled bool, reply string) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return false, ""
	}
	name, arg := splitCommand(trimmed)
	switch name {
	case "/start":
		return true, "Ciao! Sono Aura. Scrivimi quello che ti serve, oppure usa /help per i comandi."
	case "/help":
		return true, helpText
	case "/cancel":
		return true, c.cancel(chatID)
	case "/cost":
		return true, c.cost(ctx)
	case "/search":
		return true, c.search(ctx, arg)
	case "/new":
		return true, "Nuova conversazione avviata."
	case "/list":
		return true, "Usa l'app per sfogliare le conversazioni; qui ogni chat è un thread continuo."
	case "/reset":
		return true, c.cancel(chatID) + "\nConversazione azzerata."
	case "/whoami":
		return true, "Sei l'utente locale di questa istanza di Aura."
	case "/stop":
		return true, c.cancel(chatID) + "\nMi fermo."
	default:
		return true, "Istruzione non riconosciuta. Usa /help per la lista dei comandi."
	}
}

// helpText lists the 10 bot-intercept commands (no LLM call drives a command).
const helpText = "Comandi disponibili:\n" +
	"/start — avvia\n" +
	"/help — questa lista\n" +
	"/cancel — annulla il turno in corso\n" +
	"/cost — spesa cumulativa di oggi\n" +
	"/search <testo> — cerca nelle conversazioni\n" +
	"/new — nuova conversazione\n" +
	"/list — info conversazioni\n" +
	"/reset — azzera la conversazione\n" +
	"/whoami — chi sei\n" +
	"/stop — ferma Aura"

// cancel fires the in-flight turn's ctx-cancel for a chat (SC#3) and confirms. A
// /cancel with no running turn is a clean no-op confirmation (idempotent).
func (c *commands) cancel(chatID int64) string {
	c.mu.Lock()
	cancel := c.cancels[chatID]
	delete(c.cancels, chatID)
	c.mu.Unlock()
	if cancel == nil {
		return "Nessun turno in corso da annullare."
	}
	cancel()
	return "Turno annullato."
}

// cost renders today's cumulative spend using the SAME llm.CostUSD the CLI footer
// uses over the reused token aggregation (cross-slice invariant: Telegram == CLI).
func (c *commands) cost(ctx context.Context) string {
	if c.deps.Cost == nil {
		return "Costo non disponibile."
	}
	prompt, completion, provider, err := c.deps.Cost.TodayUsage(ctx)
	if err != nil {
		return "Costo non disponibile per ora."
	}
	display, _ := llm.CostUSD(c.deps.Prices, c.deps.Model, prompt, completion, provider)
	return fmt.Sprintf("Spesa di oggi: %s (%d token in, %d token out)", display, prompt, completion)
}

// search reuses conversations.SearchConversationTurns byte-for-byte and renders
// the matching turn excerpts with the SAME excerpt window the CLI uses
// (cross-slice invariant). An empty query returns a usage hint without a backend
// call.
func (c *commands) search(ctx context.Context, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "Uso: /search <testo da cercare>"
	}
	if c.deps.Search == nil {
		return "Ricerca non disponibile."
	}
	hits, err := c.deps.Search.SearchConversationTurns(ctx, query, searchResultLimit)
	if err != nil {
		return "Ricerca non disponibile per ora."
	}
	if len(hits) == 0 {
		return "Nessun risultato."
	}
	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "• [%d] %s", h.Seq, searchExcerpt(h.Content, query))
	}
	return b.String()
}

// splitCommand splits "/name rest of args" into the lowercased command name and
// the trailing argument string (a "/cmd@bot" mention suffix is stripped).
func splitCommand(s string) (name, arg string) {
	head, rest, _ := strings.Cut(s, " ")
	if at := strings.IndexByte(head, '@'); at >= 0 {
		head = head[:at]
	}
	return strings.ToLower(head), strings.TrimSpace(rest)
}

// searchExcerpt windows the content around the first case-insensitive occurrence
// of query (±excerptWindow chars), falling back to the first 2*window chars on a
// fuzzy match where the literal substring is absent. It is a verbatim port of the
// CLI excerpt() (cmd/aura/chat_repl.go) so /search renders byte-identically to
// `aura chat search` over the same FTS hits (D-A5-03, cross-slice invariant).
func searchExcerpt(content, query string) string {
	const window = 60
	flat := strings.Join(strings.Fields(content), " ")
	idx := strings.Index(strings.ToLower(flat), strings.ToLower(query))
	if idx < 0 {
		return clampExcerpt(flat, 0, window*2)
	}
	start := idx - window
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + window
	return clampExcerpt(flat, start, end)
}

// clampExcerpt slices [start,end) of s with rune-safe bounds + ellipses (verbatim
// port of the CLI clampExcerpt so the cross-slice excerpt invariant holds).
func clampExcerpt(s string, start, end int) string {
	if end > len(s) {
		end = len(s)
	}
	if start < 0 {
		start = 0
	}
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	for end > start && end < len(s) && !utf8.RuneStart(s[end]) {
		end--
	}
	out := s[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}
