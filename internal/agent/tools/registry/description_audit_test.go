package tools

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
)

// auditTools returns the canonical set of catalogued tool instances that the
// description-audit tests enforce against. Zero-value construction is safe
// because Description() methods are hardcoded strings and never read struct
// fields.
func auditTools() []Tool {
	return []Tool{
		&AskUserTool{},
		&TextResponseTool{},
		&SearchTool{},
		&WebTool{},
		&WikiPageTool{},
		&TaskTool{loc: time.Local},
		&SourceTool{},
		&FileTool{},
		&CreateDocumentTool{},
		&SkillTool{},
		&ExecuteCodeTool{},
		&ProposePatchTool{},
		&AgentNoteTool{},
	}
}

// TestDescriptionAuditMarkers enforces that every catalogued tool description
// starts with a recognised behavioral marker or an action verb in present tense.
//
// Allowed first-line patterns:
//   - "Destructive."   — tool that can delete or overwrite data
//   - "Read-only."     — tool that never writes to any store
//   - "Returns "       — tool whose primary output is a structured payload
//   - [A-Z][a-z]+[ ,] — action verb in present tense (Execute, Search, Manage, …)
//
// This is a forward-looking regression gate: any new catalogued tool without a
// conforming description first line fails loudly, forcing the author to pick a marker.
func TestDescriptionAuditMarkers(t *testing.T) {
	// Matches: "Destructive." | "Read-only." | "Returns " | action verb
	// (one CamelCase word followed by a space or comma, e.g. "Execute Python",
	// "Search Aura's", "Read, write", "Create, replace").
	allowed := regexp.MustCompile(`^(Destructive\.|Read-only\.|Returns |[A-Z][a-z]+[ ,])`)

	for _, tool := range auditTools() {
		firstLine := descriptionFirstLine(tool.Description())
		if !allowed.MatchString(firstLine) {
			t.Errorf(
				"%s: description first line %q does not start with an allowed behavioral marker "+
					"(Destructive. / Read-only. / Returns / action-verb). "+
					"Prepend one of the allowed markers, or extend the allowed regexp in this test.",
				tool.Name(), firstLine,
			)
		}
	}
}

// TestDescriptionAuditLenCap enforces that every catalogued tool description
// stays within 1500 bytes so the always-loaded manifest token budget stays bounded.
//
// Cap raised from 200 → 1500 on 2026-05-24 to fit the example-first rewrite:
// every tool's description now leads with concrete EXAMPLES blocks that drove
// schema-mismatch first-attempt failures to zero on the user-visible tools
// (text_response, agent_note, web, file, wiki_page, source). The consolidated
// tool surface keeps this budget bounded by folding recall/wiki graph actions
// into search and structured clarification into ask_user.
func TestDescriptionAuditLenCap(t *testing.T) {
	const cap = 1500
	for _, tool := range auditTools() {
		if n := len(tool.Description()); n > cap {
			t.Errorf("%s: description is %d bytes (> %d cap): %q",
				tool.Name(), n, cap, tool.Description())
		}
	}
}

// TestDescriptionAuditAsciiOnly enforces that tool descriptions stay in
// English (per feedback_all_prompts_in_english_only) via the structural
// proxy "no non-ASCII letters". This catches accented characters used in
// Italian / French / Spanish / German prose (à, è, ñ, ü) without
// hardcoding a per-language word list — the previous test used a regex
// `\b(gli|agli|nella|dei|delle)\b` which was the exact NL-token-list
// anti-pattern called out in feedback_no_regex_for_nlp (removed 2026-05-25).
//
// Non-ASCII punctuation like the em dash (—) or ellipsis (…) is allowed
// because those are stylistic choices that don't signal a language switch.
// Only letters with combining marks (Unicode category Mn or Mc) trigger.
func TestDescriptionAuditAsciiOnly(t *testing.T) {
	for _, tool := range auditTools() {
		desc := tool.Description()
		for _, r := range desc {
			if r > 127 && unicode.IsLetter(r) {
				t.Errorf("%s: description contains non-ASCII letter %q (likely non-English): %q",
					tool.Name(), r, desc)
				break
			}
		}
	}
}

// TestDescriptionAuditSpecificPhrases asserts that high-risk tools carry the
// exact behavioral phrases the LLM needs to handle their output correctly.
// These are the minimum discipline phrases from US-DRIFT-05.
func TestDescriptionAuditSpecificPhrases(t *testing.T) {
	cases := []struct {
		tool   Tool
		phrase string
	}{
		// execute_code: LLM must synthesize, never dump stdout.
		{&ExecuteCodeTool{}, "Read-only stdout, capped, results are INTERNAL — synthesize before replying"},
		// search_memory: results are INTERNAL; LLM must synthesize, not echo.
		{&SearchTool{}, "Unified knowledge lookup"},
		// source action=read: cap so the LLM knows what to expect.
		{&SourceTool{}, "Returns source archive bytes, 16384-byte cap"},
		// file action=read: same cap signal.
		{&FileTool{}, "Returns file bytes, 16384-byte cap"},
		// web action=search: default result count explicit.
		{&WebTool{}, "Returns top-5 results"},
	}

	for _, c := range cases {
		if !strings.Contains(c.tool.Description(), c.phrase) {
			t.Errorf(
				"%s: description missing required operational phrase %q",
				c.tool.Name(), c.phrase,
			)
		}
	}
}

// descriptionFirstLine returns the first non-empty line of a tool description,
// with leading and trailing whitespace trimmed.
func descriptionFirstLine(desc string) string {
	for line := range strings.SplitSeq(desc, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return strings.TrimSpace(desc)
}
