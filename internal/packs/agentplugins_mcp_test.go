package packs

import (
	"strings"
	"testing"
)

// mcpPlugin is a minimal Agent Plugins plugin whose mcp.json holds one server, plus a skill so a
// disabled MCP configuration can be seen not to take the skills with it (§7.2.2 rule 2).
func mcpPlugin(t *testing.T, mcpJSON string) Pack {
	t.Helper()
	pack, err := readOne(t, agentPlugin(t, minimalAgentManifest, map[string]string{
		"mcp.json":          mcpJSON,
		"skills/s/SKILL.md": "---\nname: s\ndescription: d\n---\nbody",
		"bin/x":             "#!/bin/sh\n",
	}))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(pack.Skills) != 1 {
		t.Fatalf("the skills went with the MCP configuration: %v", pack.Skills)
	}
	return pack
}

func oneServer(server string) string {
	return `{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": {"srv": ` + server + `}}`
}

func TestAgentPluginsMCPRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		mcpJSON  string
		read     bool   // the server is read
		skipped  string // or skipped with this reason fragment
		disabled string // or MCP is disabled with this reason fragment
	}{
		{name: "stdio", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "args": ["a"], "env": {"K": "v"}}`), read: true},
		{name: "streamable-http", mcpJSON: oneServer(`{"type": "streamable-http", "url": "https://mcp.example.com/mcp"}`), read: true},
		{name: "sse is unsupported", mcpJSON: oneServer(`{"type": "sse", "url": "https://legacy.example.com/sse"}`), skipped: `unsupported transport "sse"`},
		{
			name:     "mcp.json targeting another version",
			mcpJSON:  `{"$schema": "https://agent-plugins.org/schemas/1.1.0/mcp.schema.json", "mcpServers": {}}`,
			disabled: "targets 1.1.0",
		},
		{name: "invalid JSON", mcpJSON: `{"mcpServers":`, disabled: "not valid JSON"},
		{
			name:     "extra top-level field",
			mcpJSON:  `{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": {}, "x": 1}`,
			disabled: `"x"`,
		},
		{name: "stdio with a url", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "url": "https://x"}`), skipped: `field "url" belongs to another variant`},
		{name: "unknown type", mcpJSON: oneServer(`{"type": "ws", "url": "wss://x"}`), skipped: `unsupported type "ws"`},
		{name: "a shell command string", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx --from x"}`), skipped: "command must be one executable token"},
		{name: "a relative path that is not ./", mcpJSON: oneServer(`{"type": "stdio", "command": "bin/x"}`), skipped: "command"},
		{name: "an escaping command", mcpJSON: oneServer(`{"type": "stdio", "command": "../bin/x"}`), skipped: "command"},
		{name: "an escaping ./ command", mcpJSON: oneServer(`{"type": "stdio", "command": "./../x"}`), skipped: "outside the plugin root"},
		{name: "a placeholder in command", mcpJSON: oneServer(`{"type": "stdio", "command": "${PLUGIN_ROOT}/bin/x"}`), skipped: "command"},
		{name: "a ./ command", mcpJSON: oneServer(`{"type": "stdio", "command": "./bin/x"}`), read: true},
		{name: "a bare cwd", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "cwd": "data"}`), skipped: "cwd"},
		{name: "an escaping ./ cwd", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "cwd": "./../x"}`), skipped: "cwd"},
		{name: "an escaping root cwd", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "cwd": "${PLUGIN_ROOT}/../x"}`), skipped: "cwd"},
		{name: "an escaping data cwd", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "cwd": "${PLUGIN_DATA}/../x"}`), skipped: "cwd"},
		{name: "a data cwd", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "cwd": "${PLUGIN_DATA}/x"}`), read: true},
		{name: "a root cwd", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "cwd": "${PLUGIN_ROOT}"}`), read: true},
		{name: "a reserved env name", mcpJSON: oneServer(`{"type": "stdio", "command": "uvx", "env": {"PLUGIN_ROOT": "/x"}}`), skipped: "PLUGIN_ROOT"},
		{name: "plain http off loopback", mcpJSON: oneServer(`{"type": "streamable-http", "url": "http://example.com/mcp"}`), skipped: "non-loopback endpoints must use https"},
		{name: "plain http on loopback", mcpJSON: oneServer(`{"type": "streamable-http", "url": "http://localhost:3000/mcp"}`), read: true},
		{name: "plain http on a loopback literal", mcpJSON: oneServer(`{"type": "streamable-http", "url": "http://127.0.0.1:3000/mcp"}`), read: true},
		{name: "userinfo", mcpJSON: oneServer(`{"type": "streamable-http", "url": "https://u:p@example.com/mcp"}`), skipped: "user information"},
		{name: "fragment", mcpJSON: oneServer(`{"type": "streamable-http", "url": "https://example.com/mcp#x"}`), skipped: "fragment"},
		{name: "a relative url", mcpJSON: oneServer(`{"type": "streamable-http", "url": "/mcp"}`), skipped: "absolute"},
		{name: "headers repeated under another case", mcpJSON: oneServer(`{"type": "streamable-http", "url": "https://x.example/mcp", "headers": {"X-A": "1", "x-a": "2"}}`), skipped: "header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pack := mcpPlugin(t, tc.mcpJSON)
			switch {
			case tc.disabled != "":
				if !strings.Contains(pack.MCPDisabled, tc.disabled) {
					t.Fatalf("MCPDisabled = %q, want it to contain %q", pack.MCPDisabled, tc.disabled)
				}
				if len(pack.Servers) != 0 {
					t.Errorf("a disabled MCP configuration still read %v", pack.Servers)
				}
			case tc.read:
				if len(pack.Servers) != 1 || pack.Servers[0].Name != "srv" {
					t.Fatalf("servers = %+v, skipped = %+v; want srv read", pack.Servers, pack.SkippedServers)
				}
			default:
				if len(pack.Servers) != 0 || len(pack.SkippedServers) != 1 {
					t.Fatalf("servers = %+v, skipped = %+v; want srv skipped", pack.Servers, pack.SkippedServers)
				}
				if got := pack.SkippedServers[0].Reason; !strings.Contains(got, tc.skipped) {
					t.Errorf("reason = %q, want it to contain %q", got, tc.skipped)
				}
			}
		})
	}
}

func TestAgentPluginsMCPCarriesStdioFieldsThrough(t *testing.T) {
	t.Parallel()
	pack := mcpPlugin(t, oneServer(`{"type": "stdio", "command": "./bin/x", "args": ["--data", "${PLUGIN_DATA}/v"], "env": {"CONFIG": "${PLUGIN_ROOT}/c.json"}, "cwd": "${PLUGIN_ROOT}"}`))
	s := pack.Servers[0]
	if s.Type != "stdio" || s.Command != "./bin/x" || s.Cwd != "${PLUGIN_ROOT}" ||
		len(s.Args) != 2 || s.Args[1] != "${PLUGIN_DATA}/v" || s.Env["CONFIG"] != "${PLUGIN_ROOT}/c.json" {
		t.Errorf("server = %+v; the placeholders must arrive unexpanded, expansion is staging's", s)
	}
}

func TestAgentPluginsMCPSkipsAnUnsafeConnectorName(t *testing.T) {
	t.Parallel()
	pack := mcpPlugin(t, `{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": {
		"a__b": {"type": "stdio", "command": "uvx"},
		"x\nsource: evil": {"type": "stdio", "command": "uvx"},
		"ok": {"type": "stdio", "command": "uvx"}}}`)
	if len(pack.Servers) != 1 || pack.Servers[0].Name != "ok" {
		t.Fatalf("servers = %+v", pack.Servers)
	}
	if len(pack.SkippedServers) != 2 {
		t.Fatalf("skipped = %+v", pack.SkippedServers)
	}
	for _, s := range pack.SkippedServers {
		if !strings.Contains(s.Reason, "connector name") {
			t.Errorf("%q skipped for %q, want a connector-name reason", s.Name, s.Reason)
		}
	}
}

func TestAgentPluginsMCPDirectoryDisablesMCP(t *testing.T) {
	t.Parallel()
	root := agentPlugin(t, minimalAgentManifest, map[string]string{"mcp.json/inner": "x"})
	pack, err := readOne(t, root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(pack.MCPDisabled, "not a regular file") {
		t.Errorf("MCPDisabled = %q (§6.2)", pack.MCPDisabled)
	}
}

func TestClaudeFormatSkipsAnUnsafeConnectorName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root+"/.claude-plugin/plugin.json", `{"name": "p"}`)
	write(t, root+"/.mcp.json", `{"mcpServers": {"Gmail": {"type": "http", "url": "https://x"}, "slack": {"type": "http", "url": "https://y"}}}`)
	pack, err := readOne(t, root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(pack.Servers) != 1 || pack.Servers[0].Name != "slack" || pack.Servers[0].Type != "http" {
		t.Fatalf("servers = %+v; the declared type stays as written", pack.Servers)
	}
	if len(pack.SkippedServers) != 1 || pack.SkippedServers[0].Name != "Gmail" {
		t.Errorf("skipped = %+v", pack.SkippedServers)
	}
}
