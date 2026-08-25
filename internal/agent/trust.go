package agent

import (
	"crypto/rand"
	"encoding/hex"
	"html"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/chetto1983/aura/internal/agent/tools"
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
	// The steer-marker scrub (llm_agent_steer.go) runs AHEAD of the
	// trusted/untrusted branch, on the raw preview, and is never written back
	// to res.Preview (T-52-06: the dedup/progress-veto hash reads res.Preview
	// separately and must stay byte-identical). Ahead of the branch it matches
	// the LITERAL marker bytes the steer envelope minter produces — behind the
	// untrusted wrap it would only ever see the already-HTML-escaped form and
	// silently no-op (T-52-19). The TRUSTED branch is the one that matters: it
	// does no escaping of its own, so without this call a forged marker there
	// would reach history exactly as trusted as a genuine one (T-52-10).
	scrubbed := scrubSteerLookalikes(res.Preview)
	source, ok := untrustedSource(toolName, res)
	if !ok {
		return scrubbed
	}
	return wrapUntrustedToolOutput(source, scrubbed)
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

func wrapUntrustedToolOutput(source, content string) string {
	nonce := toolOutputNonce()
	escapedSource := html.EscapeString(source)
	escapedContent := html.EscapeString(norm.NFKC.String(content))
	return `<tool_output source="` + escapedSource + `" trust="untrusted" nonce="` + nonce + `">` +
		"\n" + escapedContent + "\n</tool_output>"
}

func toolOutputNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("agent: crypto/rand failed minting tool output nonce: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
