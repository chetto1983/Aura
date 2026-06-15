package agent

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// AG-052: unknown-tool output defaults to untrusted (fail-safe). An MCP or
// otherwise-unrecognized tool's preview must be enveloped so it cannot launder
// prompt injection through trusted provenance.
func TestUntrustedSource_UnknownToolDefaultsUntrusted(t *testing.T) {
	source, wrap := untrustedSource("some_unknown_mcp_tool", tools.ToolResult{Preview: "x"})
	if !wrap {
		t.Fatal("unknown tool defaulted trusted; want untrusted (AG-052)")
	}
	if source != "some_unknown_mcp_tool" {
		t.Fatalf("source = %q, want tool name", source)
	}
}

// Explicit safe built-ins stay trusted by allowlist, not by broad default.
func TestUntrustedSource_SafeBuiltinsStayTrusted(t *testing.T) {
	for _, name := range []string{"current_time", "text_response", "ask_user", "todo_write", "tool_search", "shell_kill"} {
		if _, wrap := untrustedSource(name, tools.ToolResult{Preview: "ok"}); wrap {
			t.Fatalf("safe built-in %q was wrapped untrusted; want trusted allowlist", name)
		}
	}
}

// Known content-embedding tools remain untrusted.
func TestUntrustedSource_ContentToolsStayUntrusted(t *testing.T) {
	for _, name := range []string{"web_fetch", "web_search", "fs_read", "fs_grep", "fs_glob", "read_tool_output", "shell_exec", "shell_poll"} {
		if _, wrap := untrustedSource(name, tools.ToolResult{Preview: "x"}); !wrap {
			t.Fatalf("content tool %q was trusted; want untrusted", name)
		}
	}
}

// An explicit TrustUntrusted provenance still wins regardless of name.
func TestUntrustedSource_ExplicitProvenanceWins(t *testing.T) {
	res := tools.ToolResult{Preview: "x", Provenance: &tools.ToolResultProvenance{Source: "mcp:mail/read", Trust: tools.TrustUntrusted}}
	source, wrap := untrustedSource("current_time", res)
	if !wrap {
		t.Fatal("explicit untrusted provenance ignored")
	}
	if source != "mcp:mail/read" {
		t.Fatalf("source = %q, want provenance source", source)
	}
}
