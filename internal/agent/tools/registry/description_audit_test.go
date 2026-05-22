package tools

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// auditTools returns the canonical set of catalogued tool instances that the
// description-audit tests enforce against. Zero-value construction is safe
// because Description() methods are hardcoded strings and never read struct
// fields.
func auditTools() []Tool {
	return []Tool{
		&AskUserTool{},
		&AskUserClarificationTool{},
		&RequestDashboardTokenTool{},
		&TextResponseTool{},
		&SearchMemoryTool{},
		&RecallOperationalTool{},
		&RecallUserMemoryTool{},
		&RecallGodNodesTool{},
		&WikiPathTool{},
		&WikiSubgraphTool{},
		&WebTool{},
		&WikiPageTool{},
		&TaskTool{loc: time.Local},
		&SourceTool{},
		&FileTool{},
		&DocTool{},
		&ExecuteCodeTool{},
		&ExecuteShellTool{},
		&DevToolTool{},
		&DailyBriefingTool{loc: time.Local, now: time.Now},
		&SearchTool{},
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
// stays within 200 bytes so the always-loaded manifest token budget stays bounded.
func TestDescriptionAuditLenCap(t *testing.T) {
	for _, tool := range auditTools() {
		if n := len(tool.Description()); n > 200 {
			t.Errorf("%s: description is %d bytes (> 200 cap): %q",
				tool.Name(), n, tool.Description())
		}
	}
}

// TestDescriptionAuditNoItalianWords enforces that tool descriptions contain no
// Italian words. Per feedback_all_prompts_in_english_only, all instructional
// text must be in English; mixing languages degrades LLM rule-following.
func TestDescriptionAuditNoItalianWords(t *testing.T) {
	itWords := regexp.MustCompile(`\b(gli|agli|nella|dei|delle)\b`)
	for _, tool := range auditTools() {
		if itWords.MatchString(tool.Description()) {
			t.Errorf("%s: description contains Italian words: %q",
				tool.Name(), tool.Description())
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
		// execute_code and execute_shell: LLM must synthesize, never dump stdout.
		{&ExecuteCodeTool{}, "Read-only stdout, capped, results are INTERNAL — synthesize before replying"},
		{&ExecuteShellTool{}, "Read-only stdout, capped, results are INTERNAL — synthesize before replying"},
		// search_memory: results are INTERNAL; LLM must synthesize, not echo.
		{&SearchMemoryTool{}, "Returns top-10 hits, INTERNAL — synthesize"},
		// source action=read: cap so the LLM knows what to expect.
		{&SourceTool{}, "Returns source archive bytes, 16384-byte cap"},
		// file action=read: same cap signal.
		{&FileTool{}, "Returns file bytes, 16384-byte cap"},
		// web action=search: default result count explicit.
		{&WebTool{}, "Returns top-5 results"},
		// doc: emphasise that the output is a path, not inline content.
		{&DocTool{}, "Returns a workspace path, not content"},
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
