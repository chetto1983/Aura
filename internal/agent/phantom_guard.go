package agent

import (
	"strings"
)

// PhantomToolGuard detects "phantom tool execution" — when an LLM reply
// mentions a registered tool by name but didn't actually invoke it in
// this turn — and triggers a forced retry.
//
// Failure mode it patches (observed in the live E2E probe on 2026-05-12):
//
//	Long conversation. Earlier turns called tools successfully. The
//	current iteration returns a no-tool-call response whose content
//	references a tool ("task", "wiki_page", "search_memory") in a
//	user-facing claim, but no tool_use was emitted in this turn at all.
//	The reply is internally consistent — "ho schedulato run_now sul
//	task" — yet the action did not happen.
//
// Detection is deliberately NOT regex-based. Regexes over natural
// language are brittle (tense, language coverage, paraphrase). The
// guard instead grounds in the live tool registry: when the assistant
// content contains a tool name that was NOT invoked anywhere in this
// turn, that's a strong phantom signal. Update-proof — adding/renaming
// tools just works.
//
// The guard is opt-in via Options.PhantomToolGuard. When nil, the loop
// behaves exactly as before. When set, the loop checks at each
// no-tool-call iteration; on a hit, it commits the assistant message
// AND injects a user-side correction telling the model to either
// invoke the tool now or retract the claim. Capped at MaxRetries
// phantom-corrections per turn to bound latency.
//
// The state passed to runLoop must implement PhantomCorrector for the
// guard to take effect — type-assertion pattern keeps the core State
// interface unchanged. conversation.Context satisfies it via its
// existing AddUserMessage method.
type PhantomToolGuard struct {
	// ToolNamesFn returns the current set of registered tool names.
	// Called on every detection so registry changes (MCP server
	// registration, dynamic tools) are picked up live. When nil, the
	// guard is inert.
	ToolNamesFn func() []string
	// MaxRetries caps phantom-correction injections per turn.
	// Default (zero value) means 1 — enough to catch a single
	// hallucinated claim without doubling latency on stubborn loops.
	MaxRetries int
	// CorrectionMessage overrides the default user-side correction
	// text. Leave empty for the default bilingual message.
	CorrectionMessage string
	// MinContentChars filters out trivial replies ("Ok", "Ciao") that
	// happen to share a substring with a tool name (rare but possible
	// — e.g. "task" inside "tasks"). Zero uses the default (40).
	MinContentChars int
}

// PhantomCorrector is the optional injection surface for the guard.
// runLoop() checks via type assertion whether the State also
// implements this interface; when it does, phantom corrections can
// be injected as user-side messages. When it doesn't, phantom
// detection still logs but cannot recover within the turn.
type PhantomCorrector interface {
	AddUserMessage(content string)
}

// LooksPhantom returns true when content mentions a registered tool
// that was NOT invoked anywhere in this turn.
//
//	hasCallsInResp  — current LLM round emitted tool_use? short-circuits to false.
//	calledThisTurn  — set of tool names invoked across ALL rounds of the current turn.
//	                  When the model legitimately references a tool it already called,
//	                  no phantom. When it references one it didn't, phantom.
//
// Live-debug fix (2026-05-13): the original detector
// false-positived on didactic explanations — when the model explains how
// it works ("Chiamo `wiki_page(action='append')`"), the tool name appears
// in the prose but the model is not claiming to have executed anything.
// The injected system correction lobotomized those replies in violation
// of feedback_no_regex_for_nlp. Two heuristics reduce the false-positive
// rate without losing the real phantom-claim signal:
//
//  1. Strip code fences (``` blocks) and inline backticks before scanning.
//     Tool names inside `code` markup are virtually always didactic —
//     never paired with a real performative claim.
//  2. Require a performative first-person past-tense verb near the tool
//     name. The set of verbs is small + bilingual (Italian + English)
//     because Aura's prompt is bilingual; coverage for other languages
//     means a future user explaining in French/Spanish won't trigger,
//     which is the right default (safer to skip than over-correct).
func (g *PhantomToolGuard) LooksPhantom(content string, hasCallsInResp bool, calledThisTurn map[string]bool) bool {
	if g == nil || g.ToolNamesFn == nil || hasCallsInResp {
		return false
	}
	content = strings.TrimSpace(content)
	if len(content) < g.minContent() {
		return false
	}
	// Strip backticked / fenced spans before matching — those are
	// didactic code references, never performative claims.
	scrubbed := stripCodeMarkup(content)
	scrubbedLower := strings.ToLower(scrubbed)
	for _, name := range g.ToolNamesFn() {
		if name == "" {
			continue
		}
		if calledThisTurn[name] {
			continue
		}
		lname := strings.ToLower(name)
		// Two signals must align: (a) the tool name appears as a bare
		// word outside code markup AND (b) a past-tense first-person
		// performative verb appears within the proximity window before
		// the tool name. Both signals together = "I did X with tool" —
		// a phantom claim. Either alone is fine: didactic ("you can use
		// wiki_page to save things"), descriptive ("ho cercato online"
		// without naming a tool), or prospective ("se vuoi salvo con
		// wiki_page" — claim is in the future tense).
		if hasPerformativeNear(scrubbedLower, lname, performativeWindow) {
			return true
		}
	}
	return false
}

// performativeWindow is the character distance (looking backwards from
// the tool name occurrence) within which we require a performative verb
// to consider the mention a phantom claim. 120 chars ≈ one clause in
// Italian or English — wide enough for "Ho schedulato il task per ..."
// but narrow enough that "ho cercato online" at the start of a paragraph
// won't pair with a "wiki_page" mention 500 chars later.
const performativeWindow = 120

// stripCodeMarkup removes triple-backtick fenced blocks and single-backtick
// inline spans so tool names appearing only as didactic code references
// don't trigger the guard. Best-effort: malformed fences are tolerated;
// the goal is "remove the obvious teaching markup", not bulletproof
// Markdown parsing.
func stripCodeMarkup(s string) string {
	// Triple-backtick fenced blocks. ungreedy match.
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+3 <= len(s) && s[i:i+3] == "```" {
			j := strings.Index(s[i+3:], "```")
			if j < 0 {
				// unterminated fence — strip the rest as code.
				break
			}
			i += 3 + j + 3
			continue
		}
		if s[i] == '`' {
			j := strings.IndexByte(s[i+1:], '`')
			if j < 0 {
				// unterminated inline — keep the lone backtick literally
				b.WriteByte(s[i])
				i++
				continue
			}
			i += 1 + j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// hasPerformativeNear reports whether the tool name appears as a bare word
// in contentLower AND a past-tense first-person performative verb appears
// within `window` characters BEFORE that occurrence. Pre-lowercased input.
//
// Why "before only" and not "around": a real claim in natural prose is
// "<verb> ... <tool name>" — the verb sets up the action, the tool name
// names the instrument. The reverse ("<tool name> ... <verb>") is much
// more often descriptive: "wiki_page is the tool I used yesterday" reads
// as descriptive even though all the same words are present.
//
// Why proximity at all: without it, a single performative verb anywhere
// in a long reply lights up every tool mention as phantom — including
// mentions in legitimate prospective or descriptive context elsewhere
// in the same message. Live debugging on a 5-iteration agent run on
// 2026-05-13 caught exactly this regression.
func hasPerformativeNear(contentLower, needle string, window int) bool {
	if needle == "" {
		return false
	}
	rest := contentLower
	cursor := 0
	for {
		i := strings.Index(rest, needle)
		if i < 0 {
			return false
		}
		absIdx := cursor + i
		// Word-boundary check on the needle.
		left := absIdx == 0 || !isWordChar(contentLower[absIdx-1])
		end := absIdx + len(needle)
		right := end == len(contentLower) || !isWordChar(contentLower[end])
		if left && right {
			// Look back `window` chars for a performative verb.
			from := absIdx - window
			if from < 0 {
				from = 0
			}
			lookback := contentLower[from:absIdx]
			for _, verb := range performativeVerbs {
				if strings.Contains(lookback, verb) {
					return true
				}
			}
		}
		rest = rest[i+1:]
		cursor = absIdx + 1
	}
}

// performativeVerbs is the bilingual past-tense first-person verb list
// used by hasPerformativeNear. Kept narrow on purpose — we'd rather miss
// a phantom than fire on a normal explanation.
var performativeVerbs = []string{
	// Italian past-tense first-person + auxiliary
	"ho schedulato", "ho chiamato", "ho invocato", "ho eseguito",
	"ho creato", "ho salvato", "ho aggiunto", "ho scritto",
	"ho letto", "ho cercato", "ho cancellato", "ho rimosso",
	"ho fatto", "ho usato", "ho compilato", "ho aggiornato",
	"ho lanciato", "ho avviato", "ho processato",
	// Italian colloquial participle-led (clause start: "Eseguito X",
	// "Schedulato X" — idiomatic post-action shorthand)
	"eseguito ", "schedulato ", "salvato ", "creato ", "lanciato ",
	"chiamato ", "invocato ", "aggiunto ", "fatto ", "rimosso ",
	"cancellato ", "aggiornato ", "compilato ", "avviato ",
	// English past-tense / present perfect
	"i scheduled", "i called", "i invoked", "i executed",
	"i created", "i saved", "i added", "i wrote", "i read",
	"i searched", "i deleted", "i removed", "i ran", "i did",
	"i used", "i compiled", "i updated", "i launched",
	"i started", "i processed",
	"i've scheduled", "i've called", "i've invoked", "i've executed",
	"i've created", "i've saved", "i've added", "i've written",
	"i've used", "i've updated",
	"just scheduled", "just called", "just ran",
}

// hasPerformativeClaim is kept for backward-compatibility with any
// caller still using the global-presence check. New code uses
// hasPerformativeNear instead.
func hasPerformativeClaim(contentLower string) bool {
	for _, verb := range performativeVerbs {
		if strings.Contains(contentLower, verb) {
			return true
		}
	}
	return false
}

// CorrectionText returns the user-side message injected when phantom
// is detected. Bilingual default unless the caller overrides via
// PhantomToolGuard.CorrectionMessage.
func (g *PhantomToolGuard) CorrectionText() string {
	if g != nil && strings.TrimSpace(g.CorrectionMessage) != "" {
		return g.CorrectionMessage
	}
	return "[system] Your previous reply named a tool but did not invoke it. " +
		"That means the action did not happen. Either invoke the tool NOW, " +
		"or correct yourself in your next reply and tell the user the action " +
		"was not performed. " +
		"[sistema] La tua ultima risposta ha nominato un tool senza invocarlo — " +
		"l'azione non è avvenuta. Invoca il tool ora, oppure correggi la risposta " +
		"e di' all'utente che l'azione non è stata eseguita."
}

// RetriesAllowed returns the cap on phantom corrections per turn.
// Default 1. Zero or negative MaxRetries means "use the default."
func (g *PhantomToolGuard) RetriesAllowed() int {
	if g == nil {
		return 0
	}
	if g.MaxRetries > 0 {
		return g.MaxRetries
	}
	return 1
}

func (g *PhantomToolGuard) minContent() int {
	if g != nil && g.MinContentChars > 0 {
		return g.MinContentChars
	}
	return 40
}

// containsAsWord reports whether needle appears in haystack with
// non-word characters (or boundary) on both sides. needle must be
// non-empty. Both args must be lowercased by the caller — this is
// the hot path; we don't want to ToLower on every call.
func containsAsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	rest := haystack
	for {
		i := strings.Index(rest, needle)
		if i < 0 {
			return false
		}
		left := i == 0 || !isWordChar(rest[i-1])
		end := i + len(needle)
		right := end == len(rest) || !isWordChar(rest[end])
		if left && right {
			return true
		}
		rest = rest[i+1:]
	}
}

func isWordChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	}
	return false
}
