package packs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// The closed server variants of §7.2.1. A field of one variant inside another, or a field of
// neither, makes the entry invalid.
var (
	stdioFields  = map[string]bool{"type": true, "command": true, "args": true, "env": true, "cwd": true}
	remoteFields = map[string]bool{"type": true, "url": true, "headers": true}
)

// readAgentServers reads mcp.json under the loading rules of §7.2.2: a file-level problem
// disables MCP for the plugin and leaves the skills alone, an entry-level problem skips that
// entry and leaves the others alone.
func readAgentServers(base string) ([]Server, []Skipped, string) {
	info, err := statContained(base, "mcp.json")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, ""
	case errors.Is(err, errOutsideRoot):
		return nil, nil, "mcp.json " + errOutsideRoot.Error()
	case err != nil:
		return nil, nil, "mcp.json: " + err.Error()
	case !info.Mode().IsRegular():
		return nil, nil, "mcp.json is not a regular file"
	}
	// #nosec G304 -- statContained has just held this path inside the checkout.
	raw, err := os.ReadFile(filepath.Join(base, "mcp.json"))
	if err != nil {
		return nil, nil, "mcp.json: " + err.Error()
	}
	entries, disabled := agentMCPEntries(raw)
	if disabled != "" {
		return nil, nil, disabled
	}
	var servers []Server
	var skipped []Skipped
	for name, entry := range entries {
		server, err := agentServer(base, name, entry)
		if err != nil {
			skipped = append(skipped, Skipped{Name: name, Reason: err.Error()})
			continue
		}
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	sortSkipped(skipped)
	return servers, skipped, ""
}

// agentMCPEntries applies the file-level rules: valid JSON, exactly $schema and mcpServers, and
// the same Agent Plugins version plugin.json declared (1.0.0 is the only one Aura reads).
func agentMCPEntries(raw []byte) (map[string]json.RawMessage, string) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return nil, "mcp.json is not valid JSON"
	}
	var keys []string
	for key := range top {
		if key != "$schema" && key != "mcpServers" {
			keys = append(keys, key)
		}
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return nil, fmt.Sprintf("mcp.json has an unknown top-level field %q", keys[0])
	}
	var schema string
	if err := json.Unmarshal(top["$schema"], &schema); err != nil || schema == "" {
		return nil, "mcp.json has no $schema"
	}
	if schema != AgentPluginsMCPSchema {
		return nil, fmt.Sprintf("mcp.json targets %s, plugin.json targets 1.0.0", schemaVersion(schema))
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(top["mcpServers"], &entries); err != nil || entries == nil {
		return nil, "mcp.json: mcpServers must be an object"
	}
	return entries, ""
}

func agentServer(base, name string, raw json.RawMessage) (Server, error) {
	if err := ValidConnectorName(name); err != nil {
		return Server{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Server{}, errors.New("a server entry must be an object")
	}
	var kind string
	if err := json.Unmarshal(fields["type"], &kind); err != nil || kind == "" {
		return Server{}, errors.New("a server entry needs a type")
	}
	switch kind {
	case "stdio":
		return agentStdioServer(base, name, fields, raw)
	case "streamable-http", "sse":
		server, err := agentRemoteServer(name, kind, fields, raw)
		if err == nil && kind == "sse" {
			// A valid entry for a transport Aura has no client for (§7.2.2 rule 4).
			return Server{}, fmt.Errorf("unsupported transport %q", kind)
		}
		return server, err
	default:
		return Server{}, fmt.Errorf("unsupported type %q", kind)
	}
}

// variantFieldError names the first field outside the entry's variant.
func variantFieldError(fields map[string]json.RawMessage, own, other map[string]bool) error {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch {
		case own[key]:
		case other[key]:
			return fmt.Errorf("field %q belongs to another variant", key)
		default:
			return fmt.Errorf("unknown field %q", key)
		}
	}
	return nil
}

func agentStdioServer(base, name string, fields map[string]json.RawMessage, raw json.RawMessage) (Server, error) {
	if err := variantFieldError(fields, stdioFields, remoteFields); err != nil {
		return Server{}, err
	}
	var entry struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Cwd     string            `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Server{}, fmt.Errorf("stdio entry: %w", err)
	}
	if err := checkCommand(base, entry.Command); err != nil {
		return Server{}, err
	}
	for key := range entry.Env {
		if key == "PLUGIN_ROOT" || key == "PLUGIN_DATA" {
			return Server{}, fmt.Errorf("env must not set %s; the client supplies it (§9.2)", key)
		}
	}
	if err := checkCwd(base, entry.Cwd); err != nil {
		return Server{}, err
	}
	return Server{Name: name, Type: entry.Type, Command: entry.Command, Args: entry.Args, Env: entry.Env, Cwd: entry.Cwd}, nil
}

// checkCommand is §7.2.1's single executable token: a bare name, or a ./ path that stays in
// the plugin. No placeholder is expanded here, which is why "${PLUGIN_ROOT}/bin/x" is refused.
func checkCommand(base, command string) error {
	switch {
	case command == "":
		return errors.New("a stdio server needs a command")
	case strings.ContainsAny(command, " \t\r\n"):
		return errors.New("command must be one executable token, not a shell command")
	case strings.HasPrefix(command, "./"):
		if _, err := withinRoot(base, command); err != nil {
			return fmt.Errorf("command %w", err)
		}
		return nil
	case strings.ContainsAny(command, `/\`):
		return errors.New("command must be a bare executable name or a ./ path inside the plugin")
	}
	return nil
}

// checkCwd accepts the three forms of §7.2.1 and holds each inside its own root: the plugin
// for ./ and ${PLUGIN_ROOT}, the data directory for ${PLUGIN_DATA}. The data directory does not
// exist yet, so its containment is lexical.
func checkCwd(base, cwd string) error {
	switch {
	case cwd == "":
		return nil
	case strings.HasPrefix(cwd, "./"):
		if _, err := withinRoot(base, cwd); err != nil {
			return fmt.Errorf("cwd %w", err)
		}
		return nil
	case cwd == "${PLUGIN_ROOT}" || strings.HasPrefix(cwd, "${PLUGIN_ROOT}/"):
		if _, err := withinRoot(base, "."+strings.TrimPrefix(cwd, "${PLUGIN_ROOT}")); err != nil {
			return fmt.Errorf("cwd %w", err)
		}
		return nil
	case cwd == "${PLUGIN_DATA}" || strings.HasPrefix(cwd, "${PLUGIN_DATA}/"):
		clean := path.Clean("./" + strings.TrimPrefix(strings.TrimPrefix(cwd, "${PLUGIN_DATA}"), "/"))
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("cwd leaves the plugin data directory")
		}
		return nil
	default:
		return errors.New("cwd must be ./…, ${PLUGIN_ROOT}… or ${PLUGIN_DATA}…")
	}
}

func agentRemoteServer(name, kind string, fields map[string]json.RawMessage, raw json.RawMessage) (Server, error) {
	if err := variantFieldError(fields, remoteFields, stdioFields); err != nil {
		return Server{}, err
	}
	var entry struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Server{}, fmt.Errorf("%s entry: %w", kind, err)
	}
	if err := checkRemoteURL(entry.URL); err != nil {
		return Server{}, err
	}
	if err := checkHeaders(entry.Headers); err != nil {
		return Server{}, err
	}
	return Server{Name: name, Type: kind, URL: entry.URL, Headers: entry.Headers}, nil
}

// checkRemoteURL is §7.2.1: absolute http(s), no user information, no fragment, and HTTPS
// unless the host is exactly localhost or a loopback IP literal.
func checkRemoteURL(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil || !u.IsAbs() || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http"):
		return errors.New("url must be an absolute http or https URL")
	case u.User != nil:
		return errors.New("url must not carry user information")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return errors.New("url must not carry a fragment")
	case u.Scheme == "http" && !isLoopbackHost(u.Hostname()):
		return errors.New("non-loopback endpoints must use https")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func checkHeaders(headers map[string]string) error {
	seen := map[string]string{}
	for name, value := range headers {
		if name == "" || strings.ContainsAny(name, " \t\r\n:") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("header %q is not a valid HTTP header field", name)
		}
		folded := strings.ToLower(name)
		if prior, dup := seen[folded]; dup {
			return fmt.Errorf("header %q repeats %q under another case", name, prior)
		}
		seen[folded] = name
	}
	return nil
}
