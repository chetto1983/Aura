// Package telegram — this file is the command dispatcher (UX-02 / SC#3 / SC#5).
// Telegram slash-commands are intercepted BEFORE any LLM dispatch (the text
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
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
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

// clearBackend is the consumer-side seam over the locked conversation hard-delete
// (conversations.Store.Delete). /clear deletes the chat's conversation row, which
// cascades to its turns, pending pauses, cache metrics and tool invocations and
// purges the sidecar dir (the migrations FK these ON DELETE CASCADE). The next
// inbound message lazily re-creates a fresh conversation (serve_channels.go
// ensuringTurn → Runner.EnsureConversation), so /clear is the Telegram-native
// "start over" — parity with the CLI's `aura chat delete`. Declared here (not an
// imported *conversations.Store) so the dispatcher stays unit-testable with a double.
type clearBackend interface {
	Delete(ctx context.Context, conversationID string) error
}

// searchResultLimit bounds the /search backend call (the CLI default).
const searchResultLimit = 20

const (
	searchPageSize       = 5
	searchCallbackUnique = "aura_search"
	searchCallbackPrefix = "srch"
)

// commandDeps are the dispatcher inputs: the two reused backends plus the price
// table + model that drive the /cost render (identical to the CLI cost footer).
type commandDeps struct {
	Search searchBackend
	Cost   costBackend
	Clear  clearBackend
	Prices map[string]llm.Price
	Model  string
}

// commands intercepts Telegram slash-commands. It also owns the per-chat
// in-flight-turn cancel registry so /cancel can abort a running turn (SC#3).
type commands struct {
	deps commandDeps

	mu          sync.Mutex
	cancels     map[int64]context.CancelFunc // chatID → the in-flight turn's cancel
	searchPages map[int64]searchPagerState
}

// newCommands builds a dispatcher over the supplied backends.
func newCommands(d commandDeps) *commands {
	return &commands{deps: d, cancels: make(map[int64]context.CancelFunc), searchPages: make(map[int64]searchPagerState)}
}

type commandReply struct {
	text   string
	markup *tele.ReplyMarkup
}

type searchPagerState struct {
	pages []string
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
	handled, out := c.dispatchRich(ctx, chatID, text)
	return handled, out.text
}

func (c *commands) dispatchRich(ctx context.Context, chatID int64, text string) (handled bool, reply commandReply) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return false, commandReply{}
	}
	name, arg := splitCommand(trimmed)
	switch name {
	case "/start":
		return true, textReply("Ciao! Sono Aura. Scrivimi quello che ti serve, oppure usa /help per i comandi.")
	case "/help":
		return true, textReply(helpText)
	case "/cancel":
		return true, textReply(c.cancel(chatID))
	case "/cost":
		return true, textReply(c.cost(ctx))
	case "/search":
		return true, c.searchRich(ctx, chatID, arg)
	case "/onboard":
		return true, textReply("Il profilo si imposta dal pannello web (Impostazioni → Profilo): un modulo con nome, lingua, dove sei e cosa fai. Da lì in poi tengo la memoria aggiornata da sola mentre lavoriamo.")
	case "/new":
		return true, textReply("In Telegram questa chat resta un thread continuo. Per ricominciare da capo usa /clear; per aprire o gestire conversazioni separate usa l'app o la CLI.")
	case "/list":
		return true, textReply("Usa l'app per sfogliare le conversazioni; qui ogni chat è un thread continuo.")
	case "/reset":
		return true, textReply(c.cancel(chatID) + "\nPer cancellare anche lo storico usa /clear.")
	case "/clear":
		return true, textReply(c.clear(ctx, chatID))
	case "/whoami":
		return true, textReply("Sei l'utente locale di questa istanza di Aura.")
	case "/stop":
		return true, textReply(c.cancel(chatID) + "\nIl bot resta attivo.")
	default:
		return true, textReply("Istruzione non riconosciuta. Usa /help per la lista dei comandi.")
	}
}

func textReply(s string) commandReply {
	return commandReply{text: s}
}

// helpText lists the bot-intercept commands (no LLM call drives a command).
const helpText = "Comandi Aura:\n" +
	"/start - saluta o completa un link di setup\n" +
	"/help - mostra questa lista\n" +
	"/cancel - annulla il turno in corso\n" +
	"/cost - mostra la spesa cumulativa di oggi\n" +
	"/search <testo> - cerca nei turni salvati\n" +
	"/onboard - indica dove impostare il profilo nel pannello web\n" +
	"/new - spiega il thread continuo Telegram\n" +
	"/list - indica dove sfogliare le conversazioni\n" +
	"/reset - annulla il turno; non cancella lo storico\n" +
	"/clear - cancella la conversazione corrente e ricomincia\n" +
	"/whoami - mostra l'identita collegata\n" +
	"/stop - annulla il turno; il bot resta attivo\n\n" +
	"In Telegram questa chat e un thread continuo. Puoi mandare testo, vocali, foto o documenti."

// cancel fires the in-flight turn's ctx-cancel for a chat (SC#3) and confirms. A
// /cancel with no running turn is a clean no-op confirmation (idempotent).
func (c *commands) cancel(chatID int64) string {
	if c.fireCancel(chatID) {
		return "Turno annullato."
	}
	return "Nessun turno in corso da annullare."
}

// fireCancel cancels (and deregisters) a chat's in-flight turn ctx, reporting
// whether a turn was actually running. It is the shared mechanism behind /cancel
// (which confirms to the user) and /clear (which must stop a turn BEFORE deleting
// the conversation row it would append to). Idempotent: a no-op when nothing runs.
func (c *commands) fireCancel(chatID int64) bool {
	c.mu.Lock()
	cancel := c.cancels[chatID]
	delete(c.cancels, chatID)
	c.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// clear is /clear: the Telegram-native "start over". It first cancels any in-flight
// turn so its goroutine stops before the row it appends to is deleted (a late
// AppendTurn after the delete would FK-fail harmlessly and be logged; cancelling
// first minimises that window). It then hard-deletes the chat's conversation, which
// cascades to turns, pending pauses, cache metrics and tool invocations and purges
// the sidecar dir. The next inbound message lazily re-creates a fresh conversation
// (ensuringTurn → Runner.EnsureConversation), so no further reset step is needed.
func (c *commands) clear(ctx context.Context, chatID int64) string {
	c.fireCancel(chatID)
	if c.deps.Clear == nil {
		return "Cancellazione della conversazione non disponibile."
	}
	if err := c.deps.Clear.Delete(ctx, convID(chatID)); err != nil {
		return "Non sono riuscita a cancellare la conversazione, riprova."
	}
	return "Conversazione cancellata. Ricominciamo da capo."
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

// searchRich reuses conversations.SearchConversationTurns byte-for-byte and renders
// the matching turn excerpts with the SAME excerpt window the CLI uses
// (cross-slice invariant). An empty query returns a usage hint without a backend
// call.
func (c *commands) searchRich(ctx context.Context, chatID int64, query string) commandReply {
	query = strings.TrimSpace(query)
	if query == "" {
		return textReply("Uso: /search <testo da cercare>")
	}
	if c.deps.Search == nil {
		return textReply("Ricerca non disponibile.")
	}
	hits, err := c.deps.Search.SearchConversationTurns(ctx, query, searchResultLimit)
	if err != nil {
		return textReply("Ricerca non disponibile per ora.")
	}
	if len(hits) == 0 {
		return textReply("Nessun risultato.")
	}
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, fmt.Sprintf("• [%d] %s", h.Seq, searchExcerpt(h.Content, query)))
	}
	pages := paginateSearchLines(lines)
	if len(pages) <= 1 {
		return textReply(strings.Join(lines, "\n"))
	}
	c.mu.Lock()
	c.searchPages[chatID] = searchPagerState{pages: pages}
	c.mu.Unlock()
	return c.renderSearchPage(chatID, 0)
}

func (c *commands) searchPage(chatID int64, page int) commandReply {
	return c.renderSearchPage(chatID, page)
}

func (c *commands) renderSearchPage(chatID int64, page int) commandReply {
	c.mu.Lock()
	state, ok := c.searchPages[chatID]
	c.mu.Unlock()
	if !ok || len(state.pages) == 0 {
		return textReply("Ricerca scaduta. Ripeti /search.")
	}
	if page < 0 {
		page = 0
	}
	if page >= len(state.pages) {
		page = len(state.pages) - 1
	}
	return commandReply{
		text:   fmt.Sprintf("%s\n\nPagina %d/%d", state.pages[page], page+1, len(state.pages)),
		markup: searchMarkup(page, len(state.pages)),
	}
}

func paginateSearchLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	pages := make([]string, 0, (len(lines)+searchPageSize-1)/searchPageSize)
	for start := 0; start < len(lines); start += searchPageSize {
		end := start + searchPageSize
		if end > len(lines) {
			end = len(lines)
		}
		pages = append(pages, strings.Join(lines[start:end], "\n"))
	}
	return pages
}

func searchMarkup(page, total int) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	prev := page - 1
	if prev < 0 {
		prev = 0
	}
	next := page + 1
	if next >= total {
		next = total - 1
	}
	mk.InlineKeyboard = [][]tele.InlineButton{
		{
			{Unique: searchCallbackUnique, Text: "Indietro", Data: searchCallbackData(prev)},
			{Unique: searchCallbackUnique, Text: fmt.Sprintf("%d/%d", page+1, total), Data: searchCallbackData(page)},
			{Unique: searchCallbackUnique, Text: "Avanti", Data: searchCallbackData(next)},
		},
		{{Unique: searchCallbackUnique, Text: "Chiudi", Data: searchCallbackCloseData()}},
	}
	return mk
}

func searchCallbackData(page int) string {
	return fmt.Sprintf("%s|%d", searchCallbackPrefix, page)
}

func searchCallbackCloseData() string {
	return searchCallbackPrefix + "|close"
}

func parseSearchCallback(data string) (page int, close bool, ok bool) {
	prefix, raw, found := strings.Cut(data, callbackSep)
	if !found || prefix != searchCallbackPrefix {
		return 0, false, false
	}
	if raw == "close" {
		return 0, true, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, false
	}
	return n, false, true
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
