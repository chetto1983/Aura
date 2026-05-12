package agentloop

import (
	"strings"
)

// PhantomToolGuard detects "phantom tool execution" — when an LLM reply
// mentions a registered tool by name but didn't actually invoke it in
// this turn — and triggers a forced retry.
//
// Failure mode it patches (observed 2026-05-12, Wave 2.7b E2E):
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
// The state passed to agentloop.Run must implement PhantomCorrector
// for the guard to take effect — type-assertion pattern keeps the
// core State interface unchanged. conversation.Context satisfies it
// via its existing AddUserMessage method.
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
// agentloop.Run() checks via type assertion whether the State also
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
func (g *PhantomToolGuard) LooksPhantom(content string, hasCallsInResp bool, calledThisTurn map[string]bool) bool {
	if g == nil || g.ToolNamesFn == nil || hasCallsInResp {
		return false
	}
	content = strings.TrimSpace(content)
	if len(content) < g.minContent() {
		return false
	}
	contentLower := strings.ToLower(content)
	for _, name := range g.ToolNamesFn() {
		if name == "" {
			continue
		}
		if calledThisTurn[name] {
			continue
		}
		// Match the tool name only when surrounded by non-word characters
		// (or at string boundaries). This is a tiny "word-aware substring"
		// without resorting to regex — avoids matching "task" inside
		// "tasks" or "multitasking" while still catching it in
		// "Ho schedulato il task wave-X".
		if containsAsWord(contentLower, strings.ToLower(name)) {
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
//
// Examples:
//
//	containsAsWord("ho schedulato il task wave", "task") == true
//	containsAsWord("the tasks list is empty", "task")    == false (plural)
//	containsAsWord("multitasking is hard", "task")        == false (substring)
//	containsAsWord("task!", "task")                       == true (trailing punct OK)
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
