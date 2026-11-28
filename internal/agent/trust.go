package agent

import (
	"crypto/rand"
	"encoding/hex"
	"html"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/steer"
)

// trustedToolNames is the explicit allowlist of genuinely-safe built-ins whose
// output is deterministic/internal and never embeds external, file, web, or
// swarm-child content. Everything NOT on this list (including all MCP/unknown
// tools and every content-embedding built-in) defaults to UNTRUSTED so the
// parent prompt wraps it in the nonce envelope (AG-052: fail-safe default
// inverts the previous unknown→trusted behavior). An explicit TrustUntrusted
// provenance always wins regardless of this list.
var trustedToolNames = map[string]struct{}{
	"current_time":  {},
	"text_response": {},
	"ask_user":      {},
	"todo_write":    {},
	"tool_search":   {},
	"shell_kill":    {},
}

func renderToolResultForPrompt(toolName string, res tools.ToolResult) string {
	// The steer-marker scrub (conversations.ScrubSteerLookalikes) runs AHEAD of the
	// trusted/untrusted branch, on the raw preview, and is never written back
	// to res.Preview (T-52-06: the dedup/progress-veto hash reads res.Preview
	// separately and must stay byte-identical). Ahead of the branch it matches
	// the LITERAL marker bytes the steer envelope minter produces — behind the
	// untrusted wrap it would only ever see the already-HTML-escaped form and
	// silently no-op (T-52-19). The TRUSTED branch is the one that matters: it
	// does no escaping of its own, so without this call a forged marker there
	// would reach history exactly as trusted as a genuine one (T-52-10).
	scrubbed := conversations.ScrubSteerLookalikes(res.Preview)
	source, ok := untrustedSource(toolName, res)
	if !ok {
		return scrubbed
	}
	return FrameUntrustedPromptContent(source, scrubbed)
}

func untrustedSource(toolName string, res tools.ToolResult) (string, bool) {
	if res.Provenance != nil {
		switch res.Provenance.Trust {
		case tools.TrustUntrusted:
			source := strings.TrimSpace(res.Provenance.Source)
			if source == "" {
				source = toolName
			}
			return source, true
		case tools.TrustTrusted:
			// Explicit operator-trusted provenance (e.g. the MCP bridge) wins over
			// the name-based untrusted-by-default fallback below.
			return "", false
		}
	}
	if _, ok := trustedToolNames[toolName]; ok {
		return "", false
	}
	return toolName, true
}

// WatchRecordSource names the frame around a mail's sender and subject in a watch's conversation
// record. A plain line and the refs stay outside the frame. A path that shows a turn to a person
// goes through ShowRecord.
const WatchRecordSource = "mail_watch"

// frameTail is the rest of the envelope FrameUntrustedPromptContent writes after its source.
// The content is escaped, so a forged closing tag cannot end a match early.
const frameTail = `" trust="untrusted" nonce="[0-9a-f]+">\n(.*?)\n</tool_output>`

// recordFrame matches the envelope around the text an assistant turn records from someone else,
// wherever it sits in the turn: a mail's sender and subject, a delegated worker's report, a
// scheduled agent_job's report. Group 1 is the source, group 2 the escaped text.
var recordFrame = regexp.MustCompile(`(?s)<tool_output source="(` + regexp.QuoteMeta(WatchRecordSource) + `|` +
	regexp.QuoteMeta(steer.SourceWorker) + `|` + regexp.QuoteMeta(steer.SourceScheduler) + `)` + frameTail)

// toolOutputFrame matches a tool turn that is one envelope from end to end, as a persisted
// untrusted result is.
var toolOutputFrame = regexp.MustCompile(`(?s)\A<tool_output source="[^"]*` + frameTail + `\z`)

// PlainToolOutput gives a person the text of a tool result the model read framed, unescaped.
// A trusted result, or one persisted before the frame was (2026-11-28), has no frame and comes
// back as it is. Like ShowRecord, its output never goes back to a model.
func PlainToolOutput(content string) string {
	quoted := toolOutputFrame.FindStringSubmatch(content)
	if quoted == nil {
		return content
	}
	return unescapePromptText(quoted[1])
}

// ShowRecord rewrites a turn for a person: each record's frame gives way to its text, unescaped.
// A mail's text goes through quoteMail, which the cockpit uses to fence it; a worker's or a
// scheduled run's report reads as it did before it was framed. That text is someone else's
// again and never goes back to a model.
func ShowRecord(content string, quoteMail func(text string) string) string {
	return recordFrame.ReplaceAllStringFunc(content, func(frame string) string {
		match := recordFrame.FindStringSubmatch(frame)
		text := unescapePromptText(match[2])
		if match[1] == WatchRecordSource {
			return quoteMail(text)
		}
		return text
	})
}

// PlainRecord is ShowRecord for a path that already prints a turn as text.
func PlainRecord(content string) string {
	return ShowRecord(content, func(text string) string { return text })
}

// FrameUntrustedPromptContent wraps model-facing untrusted data in the shared
// nonce-bearing envelope used for tool output, delegated input and recorded reports.
func FrameUntrustedPromptContent(source, content string) string {
	nonce := toolOutputNonce()
	escapedSource := html.EscapeString(source)
	escapedContent := EscapePromptText(content)
	return `<tool_output source="` + escapedSource + `" trust="untrusted" nonce="` + nonce + `">` +
		"\n" + escapedContent + "\n</tool_output>"
}

// promptTextEscaper escapes the three characters that can forge or close a tag in element
// content. Quotes are left alone: they matter only inside an attribute, and escaping them
// turned every JSON a tool returned into {&#34;action&#34;:...} (measured 2026-10-31).
var promptTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// EscapePromptText neutralizes prompt-control syntax without assigning a trust class. NFKC
// runs first, so a fullwidth ＜ folds to < and is escaped with it. Memory reuses this encoding
// but deliberately not the untrusted tool-output envelope.
func EscapePromptText(content string) string {
	return promptTextEscaper.Replace(norm.NFKC.String(content))
}

// promptTextUnescaper undoes promptTextEscaper in one pass, so "&amp;lt;" reads as "&lt;" and not
// as "<". NFKC folding is not undone: a fullwidth bracket stays folded.
var promptTextUnescaper = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

func unescapePromptText(content string) string {
	return promptTextUnescaper.Replace(content)
}

func toolOutputNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("agent: crypto/rand failed minting tool output nonce: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
