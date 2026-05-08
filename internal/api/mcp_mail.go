package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/mcp"
)

const mcpMailTimeout = 30 * time.Second

type mailProviderAdapter struct {
	providerID string
	searchTool string
	readTool   string
}

func handleMCPProviderProbe(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := r.PathValue("id")
		provider, ok := findMCPProvider(providerID)
		if !ok {
			writeError(w, deps.Logger, http.StatusNotFound, "mcp provider not found")
			return
		}
		client := findMCPClientForProvider(provider, deps.MCP)
		if client == nil {
			writeJSON(w, deps.Logger, http.StatusFailedDependency, ConnectorProbeResponse{
				OK:                  false,
				ProviderID:          providerID,
				MissingCapabilities: capabilityIDs(provider.Capabilities),
				Error:               "mcp server is not connected for this provider",
			})
			return
		}

		resp := probeMCPProvider(provider, client)
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

func handleMCPProviderMailSearch(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := r.PathValue("id")
		provider, client, adapter, ok := resolveMailProvider(deps, providerID)
		if !ok {
			writeError(w, deps.Logger, http.StatusFailedDependency, "mail provider is not ready for search/read")
			return
		}
		if provider.Kind != "mail" {
			writeError(w, deps.Logger, http.StatusBadRequest, "provider is not a mail provider")
			return
		}

		args, err := readMCPArgs(r)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		callArgs := canonicalMailSearchArgs(args)
		ctx, cancel := context.WithTimeout(r.Context(), mcpMailTimeout)
		defer cancel()
		output, err := client.CallTool(ctx, adapter.searchTool, callArgs)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadGateway, err.Error())
			return
		}
		messages, raw := normalizeMailSearch(output)
		writeJSON(w, deps.Logger, http.StatusOK, MailSearchResponse{
			ProviderID:   providerID,
			ProviderTool: adapter.searchTool,
			Messages:     messages,
			Raw:          raw,
		})
	}
}

func handleMCPProviderMailRead(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := r.PathValue("id")
		provider, client, adapter, ok := resolveMailProvider(deps, providerID)
		if !ok {
			writeError(w, deps.Logger, http.StatusFailedDependency, "mail provider is not ready for search/read")
			return
		}
		if provider.Kind != "mail" {
			writeError(w, deps.Logger, http.StatusBadRequest, "provider is not a mail provider")
			return
		}

		args, err := readMCPArgs(r)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		callArgs := canonicalMailReadArgs(args)
		if strings.TrimSpace(fmt.Sprint(callArgs["message_id"])) == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "message_id is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), mcpMailTimeout)
		defer cancel()
		output, err := client.CallTool(ctx, adapter.readTool, callArgs)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadGateway, err.Error())
			return
		}
		message, raw := normalizeMailRead(output)
		writeJSON(w, deps.Logger, http.StatusOK, MailReadResponse{
			ProviderID:   providerID,
			ProviderTool: adapter.readTool,
			Message:      message,
			Raw:          raw,
		})
	}
}

func findMCPProvider(id string) (ConnectorProviderSummary, bool) {
	for _, p := range mcpProviderManifests {
		if p.ID == id {
			return p, true
		}
	}
	return ConnectorProviderSummary{}, false
}

func probeMCPProvider(provider ConnectorProviderSummary, client mcp.Server) ConnectorProbeResponse {
	advertised := advertisedToolSet(client)
	approved := intersectAdvertised(provider.ApprovedTools, advertised)
	blocked := intersectAdvertised(provider.BlockedTools, advertised)
	sort.Strings(approved)
	sort.Strings(blocked)

	resp := ConnectorProbeResponse{
		ProviderID:              provider.ID,
		ServerName:              client.Name(),
		ApprovedToolsAdvertised: approved,
		BlockedToolsAdvertised:  blocked,
	}
	if provider.Kind == "mail" {
		adapter, missing := selectMailProviderAdapter(provider.ID, advertised)
		if len(missing) == 0 {
			resp.OK = true
			resp.CapabilitiesReady = []string{"mail.search", "mail.read"}
			if adapter.searchTool != "" && adapter.readTool != "" {
				resp.ApprovedToolsAdvertised = uniqueStrings(append(resp.ApprovedToolsAdvertised, adapter.searchTool, adapter.readTool))
			}
			return resp
		}
		resp.MissingCapabilities = missing
		resp.Error = "mail provider is missing approved search/read tools"
		return resp
	}

	resp.OK = len(approved) > 0
	if !resp.OK {
		resp.MissingCapabilities = capabilityIDs(provider.Capabilities)
		resp.Error = "provider has no approved tools advertised"
	}
	return resp
}

func resolveMailProvider(deps Deps, providerID string) (ConnectorProviderSummary, mcp.ConnectedClient, mailProviderAdapter, bool) {
	provider, ok := findMCPProvider(providerID)
	if !ok || provider.Kind != "mail" {
		return ConnectorProviderSummary{}, nil, mailProviderAdapter{}, false
	}
	client := findMCPClientForProvider(provider, deps.MCP)
	if client == nil {
		return ConnectorProviderSummary{}, nil, mailProviderAdapter{}, false
	}
	adapter, missing := selectMailProviderAdapter(providerID, advertisedToolSet(client))
	if len(missing) != 0 {
		return ConnectorProviderSummary{}, nil, mailProviderAdapter{}, false
	}
	return provider, client, adapter, true
}

func findMCPClientForProvider(provider ConnectorProviderSummary, clients []mcp.ConnectedClient) mcp.ConnectedClient {
	names := provider.MCPServerNames
	if len(names) == 0 {
		names = []string{provider.ID}
	}
	for _, name := range names {
		if client := findMCPClient(name, clients); client != nil {
			return client
		}
	}
	return nil
}

func selectMailProviderAdapter(providerID string, advertised map[string]bool) (mailProviderAdapter, []string) {
	candidates := [][]string{
		{"imap_search_messages", "imap_get_message"},
		{"ews_search_messages", "ews_get_message"},
	}
	if providerID == "gmail-dedicated" {
		candidates = [][]string{{"list_emails", "get_email"}}
	}
	for _, pair := range candidates {
		if advertised[pair[0]] && advertised[pair[1]] {
			return mailProviderAdapter{providerID: providerID, searchTool: pair[0], readTool: pair[1]}, nil
		}
	}
	return mailProviderAdapter{}, []string{"mail.search", "mail.read"}
}

func advertisedToolSet(client mcp.Server) map[string]bool {
	out := make(map[string]bool)
	if client == nil {
		return out
	}
	for _, t := range client.Tools() {
		out[t.Name] = true
	}
	return out
}

func intersectAdvertised(names []string, advertised map[string]bool) []string {
	out := make([]string, 0)
	for _, name := range names {
		if advertised[name] {
			out = append(out, name)
		}
	}
	return out
}

func capabilityIDs(caps []ConnectorCapability) []string {
	out := make([]string, 0, len(caps))
	for _, cap := range caps {
		out = append(out, cap.ID)
	}
	return out
}

func canonicalMailSearchArgs(in map[string]any) map[string]any {
	out := make(map[string]any)
	copyStringArg(out, in, "account")
	copyStringArg(out, in, "mailbox")
	copyStringArg(out, in, "query")
	if limit, ok := in["limit"]; ok {
		out["limit"] = limit
	}
	return out
}

func canonicalMailReadArgs(in map[string]any) map[string]any {
	out := make(map[string]any)
	copyStringArg(out, in, "account")
	copyStringArg(out, in, "message_id")
	copyStringArg(out, in, "thread_id")
	return out
}

func copyStringArg(out, in map[string]any, key string) {
	if value, ok := in[key]; ok {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			out[key] = text
		}
	}
}

func normalizeMailSearch(output string) ([]MailMessage, string) {
	var wrapper struct {
		Messages []MailMessage `json:"messages"`
	}
	if json.Unmarshal([]byte(output), &wrapper) == nil && len(wrapper.Messages) > 0 {
		return redactMessages(wrapper.Messages), ""
	}
	var messages []MailMessage
	if json.Unmarshal([]byte(output), &messages) == nil && len(messages) > 0 {
		return redactMessages(messages), ""
	}
	return nil, redactConnectorText(output)
}

func normalizeMailRead(output string) (MailMessage, string) {
	var message MailMessage
	if json.Unmarshal([]byte(output), &message) == nil && message.ID != "" {
		return redactMessage(message), ""
	}
	var wrapper struct {
		Message MailMessage `json:"message"`
	}
	if json.Unmarshal([]byte(output), &wrapper) == nil && wrapper.Message.ID != "" {
		return redactMessage(wrapper.Message), ""
	}
	return MailMessage{}, redactConnectorText(output)
}

func redactMessages(messages []MailMessage) []MailMessage {
	out := make([]MailMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, redactMessage(msg))
	}
	return out
}

func redactMessage(msg MailMessage) MailMessage {
	msg.Snippet = redactConnectorText(msg.Snippet)
	msg.Body = redactConnectorText(msg.Body)
	return msg
}

func redactConnectorText(text string) string {
	if text == "" {
		return text
	}
	fields := strings.Fields(text)
	for i, field := range fields {
		lower := strings.ToLower(field)
		for _, key := range []string{"token=", "password=", "secret=", "api_key=", "apikey="} {
			if strings.Contains(lower, key) {
				fields[i] = redactKeyValueToken(field, key)
				break
			}
		}
	}
	if len(fields) == 0 {
		return text
	}
	return strings.Join(fields, " ")
}

func redactKeyValueToken(field, key string) string {
	idx := strings.Index(strings.ToLower(field), key)
	if idx < 0 {
		return "[redacted]"
	}
	return field[:idx+len(key)] + "[redacted]"
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
