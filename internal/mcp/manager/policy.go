package manager

import (
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// Risk labels classify the danger of an MCP tool call; they drive risk-based
// allow/deny decisions in the manager's tool policy.
const (
	RiskDestructive  = "destructive"
	RiskExternalSend = "external_send"
	RiskFilesystem   = "filesystem"
	RiskNetwork      = "network"
	RiskPrivateData  = "private_data"
	RiskRead         = "read"
	RiskUnknown      = "unknown"
	RiskWrite        = "write"
)

// PolicyDecision records whether a single MCP tool is allowed to mount, along
// with its inferred risk labels and, when blocked, the reason.
type PolicyDecision struct {
	ToolName    string   `json:"toolName"`
	RiskLabels  []string `json:"riskLabels"`
	Allowed     bool     `json:"allowed"`
	BlockReason string   `json:"blockReason,omitempty"`
}

// PolicyBlock describes a configured block in a server's tool policy, by tool
// name or by risk label, with a human-readable reason for status reporting.
type PolicyBlock struct {
	ToolName string `json:"toolName,omitempty"`
	Risk     string `json:"risk,omitempty"`
	Reason   string `json:"reason"`
}

// RiskLabelsForTool infers a sorted set of risk labels for a tool from keyword
// heuristics over its name and description, merged with the server's declared
// risk labels.
func RiskLabelsForTool(def mcp.ToolDef, server mcp.ManagedServer) []string {
	text := strings.ToLower(def.Name + " " + def.Description)
	labels := map[string]struct{}{}

	add := func(label string) {
		labels[label] = struct{}{}
	}
	hasAny := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(text, word) {
				return true
			}
		}
		return false
	}

	if hasAny("read", "list", "search", "fetch", "get", "query", "find", "inspect", "retrieve", "calculate") {
		add(RiskRead)
	}
	if hasAny("write", "create", "update", "edit", "modify", "move", "delete", "remove", "send", "post", "upload", "save", "add", "reply") {
		add(RiskWrite)
	}
	if hasAny("http", "url", "download", "upload", "post", "webhook", "api") {
		add(RiskNetwork)
	}
	if hasAny("file", "folder", "directory", "path", "filesystem", "workspace") {
		add(RiskFilesystem)
	}
	if hasAny("delete", "remove", "purge", "drop", "destroy", "erase", "wipe", "truncate", "permanently") {
		add(RiskDestructive)
	}
	if hasAny("send", "post", "upload", "reply", "smtp", "whatsapp") {
		add(RiskExternalSend)
	}
	if hasAny("email", "mail", "message", "calendar", "event", "contact", "thread", "chat", "profile") {
		add(RiskPrivateData)
	}
	if len(labels) == 0 {
		add(RiskUnknown)
	}
	for _, label := range server.RiskLabels {
		label = strings.TrimSpace(label)
		if label != "" {
			add(label)
		}
	}
	return sortedKeys(labels)
}

// PolicyDecisionForTool evaluates a tool against the server's tool policy
// (allowlist, denylist, and denied risk labels) and returns the resulting
// allow/block decision.
func PolicyDecisionForTool(def mcp.ToolDef, server mcp.ManagedServer) PolicyDecision {
	decision := PolicyDecision{
		ToolName:   def.Name,
		RiskLabels: RiskLabelsForTool(def, server),
		Allowed:    true,
	}
	if len(server.ToolPolicy.Allow) > 0 && !stringSliceContains(server.ToolPolicy.Allow, def.Name) {
		decision.Allowed = false
		decision.BlockReason = "not in allowlist"
		return decision
	}
	if stringSliceContains(server.ToolPolicy.Deny, def.Name) {
		decision.Allowed = false
		decision.BlockReason = "denied by tool policy"
		return decision
	}
	for _, risk := range effectiveDenyRisk(server) {
		if stringSliceContains(decision.RiskLabels, risk) {
			decision.Allowed = false
			decision.BlockReason = "risk " + risk + " denied by tool policy"
			return decision
		}
	}
	return decision
}

// PolicyDecisionsForTools returns the PolicyDecision for each tool def under the
// given server's policy.
func PolicyDecisionsForTools(defs []mcp.ToolDef, server mcp.ManagedServer) []PolicyDecision {
	out := make([]PolicyDecision, 0, len(defs))
	for _, def := range defs {
		out = append(out, PolicyDecisionForTool(def, server))
	}
	return out
}

// PolicyCounts returns the number of explicitly allowed tools and the number of
// configured blocks (denied tools plus effective denied risk labels) for a server.
func PolicyCounts(server mcp.ManagedServer) (mounted int, blocked int) {
	return len(server.ToolPolicy.Allow), len(server.ToolPolicy.Deny) + len(effectiveDenyRisk(server))
}

// ConfiguredPolicyBlocks lists the server's tool-policy blocks (denied tool
// names and effective denied risk labels) as PolicyBlock entries.
func ConfiguredPolicyBlocks(server mcp.ManagedServer) []PolicyBlock {
	blocks := make([]PolicyBlock, 0, len(server.ToolPolicy.Deny)+len(effectiveDenyRisk(server)))
	for _, tool := range server.ToolPolicy.Deny {
		if strings.TrimSpace(tool) == "" {
			continue
		}
		blocks = append(blocks, PolicyBlock{ToolName: tool, Reason: "denied by tool policy"})
	}
	for _, risk := range effectiveDenyRisk(server) {
		blocks = append(blocks, PolicyBlock{Risk: risk, Reason: "risk " + risk + " denied by tool policy"})
	}
	return blocks
}

func effectiveDenyRisk(server mcp.ManagedServer) []string {
	labels := map[string]struct{}{
		RiskDestructive: {},
		RiskUnknown:     {},
	}
	for _, risk := range server.ToolPolicy.DenyRisk {
		risk = strings.TrimSpace(risk)
		if risk != "" {
			labels[risk] = struct{}{}
		}
	}
	return sortedKeys(labels)
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
