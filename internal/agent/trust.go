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
	source, ok := untrustedSource(toolName, res)
	if !ok {
		return res.Preview
	}
	return wrapUntrustedToolOutput(source, res.Preview)
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
