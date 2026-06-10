package agent

import (
	"crypto/rand"
	"encoding/hex"
	"html"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/chetto1983/aura/internal/agent/tools"
)

var untrustedToolNames = map[string]struct{}{
	"web_fetch":        {},
	"web_search":       {},
	"fs_read":          {},
	"fs_grep":          {},
	"fs_glob":          {},
	"read_tool_output": {},
	"shell_exec":       {},
	"shell_poll":       {},
}

func renderToolResultForPrompt(toolName string, res tools.ToolResult) string {
	source, ok := untrustedSource(toolName, res)
	if !ok {
		return res.Preview
	}
	return wrapUntrustedToolOutput(source, res.Preview)
}

func untrustedSource(toolName string, res tools.ToolResult) (string, bool) {
	if res.Provenance != nil && res.Provenance.Trust == tools.TrustUntrusted {
		source := strings.TrimSpace(res.Provenance.Source)
		if source == "" {
			source = toolName
		}
		return source, true
	}
	if _, ok := untrustedToolNames[toolName]; ok {
		return toolName, true
	}
	return "", false
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
