package mcptools

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/netguard"
)

// This is deliberately narrower than agent-browser's common options. In particular,
// restore/launch flags and filesystem destinations would bypass the browser boundary.
var browserArgumentFields = map[string]string{
	"tools_profiles": "", "get_url": "", "get_title": "", "back": "", "forward": "", "reload": "", "tab_list": "",
	"open": "url headed", "read": "url filter llms outline raw readTimeoutMs requireMd",
	"snapshot": "compact delta full includeUrls interactive depth selector",
	"click":    "human newTab selector", "fill": "selector text", "type": "clear delayMs selector text",
	"press": "key", "check": "selector", "uncheck": "selector", "select": "selector values",
	"scroll": "amount direction selector", "wait_ms": "ms", "wait_for_selector": "selector waitTimeoutMs",
	"wait_for_text": "text waitTimeoutMs", "wait_for_load": "state waitTimeoutMs", "get_text": "selector",
	"eval": "script", "close": "all", "tab_new": "label url", "tab_switch": "tab", "tab_close": "tab",
	"screenshot": "annotate format fullPage ifChanged quality selector threshold",
}

func browserSchema(raw json.RawMessage, policy bridgePolicy, tool string) json.RawMessage {
	if policy.recipeSource != mcpmanager.BrowserRecipeSource {
		return raw
	}
	var schema map[string]any
	if json.Unmarshal(raw, &schema) != nil {
		return emptyObjectSchema
	}
	fields := " session timeoutMs " + browserArgumentFields[strings.TrimPrefix(tool, "agent_browser_")] + " "
	if props, ok := schema["properties"].(map[string]any); ok {
		for key := range props {
			if !strings.Contains(fields, " "+key+" ") {
				delete(props, key)
			}
		}
	}
	if required, ok := schema["required"].([]any); ok {
		kept := required[:0]
		for _, key := range required {
			if name, ok := key.(string); ok && strings.Contains(fields, " "+name+" ") {
				kept = append(kept, key)
			}
		}
		schema["required"] = kept
	}
	schema["additionalProperties"] = false
	filtered, err := json.Marshal(schema)
	if err != nil {
		return emptyObjectSchema
	}
	return filtered
}

func validateBrowserArguments(tool string, args map[string]any) error {
	name := strings.TrimPrefix(tool, "agent_browser_")
	fields, ok := browserArgumentFields[name]
	if !ok {
		return fmt.Errorf("browser tool %q is not admitted", tool)
	}
	allowed := " session timeoutMs " + fields + " "
	for key, value := range args {
		if !strings.Contains(allowed, " "+key+" ") {
			return fmt.Errorf("browser argument %q is not allowed", key)
		}
		if key == "url" {
			raw, ok := value.(string)
			if !ok || !publicBrowserURL(raw) {
				return fmt.Errorf("browser URL is not allowed: use public http/https or about:blank")
			}
		}
	}
	return nil
}

func publicBrowserURL(raw string) bool {
	if raw == "about:blank" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" || strings.ContainsAny(raw, "\\\r\n\t") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		_, blocked := netguard.Classify(ip)
		return !blocked
	}
	// WHATWG/Chrome accepts shortened, hexadecimal and integer IPv4 spellings that
	// netip correctly rejects. Admit DNS names only, never that alternate IP grammar.
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".nip.io") || strings.HasSuffix(host, ".sslip.io") {
		return false
	}
	last := host[strings.LastIndexByte(host, '.')+1:]
	for _, r := range last {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}
