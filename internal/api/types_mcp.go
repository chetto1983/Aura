package api

// MCPToolInfo is one tool advertised by an MCP server. InputSchema is the
// raw JSON Schema map returned from tools/list.
type MCPToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// MCPServerSummary is one row of GET /mcp/servers. Only servers that
// connected successfully at boot show up here.
type MCPServerSummary struct {
	Name             string        `json:"name"`
	Transport        string        `json:"transport"` // "stdio" or "http"
	ToolCount        int           `json:"tool_count"`
	Tools            []MCPToolInfo `json:"tools"`
	OverrideWarnings []string      `json:"override_warnings,omitempty"`
}

// ConnectorProviderSummary is the dashboard-facing configuration card for
// approved MCP provider profiles.
type ConnectorProviderSummary struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Kind            string                `json:"kind"`    // "mail" | "database"
	Profile         string                `json:"profile"` // "personal" | "business"
	Description     string                `json:"description"`
	Status          string                `json:"status"`       // "not_configured" | "configured" | "ready" | "enabled" | "failed"
	RuntimeType     string                `json:"runtime_type"` // "container" | "stdio" | "remote_http"
	RepositoryURL   string                `json:"repository_url,omitempty"`
	HomepageURL     string                `json:"homepage_url,omitempty"`
	MCPServerNames  []string              `json:"mcp_server_names,omitempty"`
	Capabilities    []ConnectorCapability `json:"capabilities"`
	RiskBadges      []ConnectorRiskBadge  `json:"risk_badges"`
	RequiredSecrets []string              `json:"required_secrets,omitempty"`
	ApprovedTools   []string              `json:"approved_tools,omitempty"`
	BlockedTools    []string              `json:"blocked_tools,omitempty"`
	SetupHints      []string              `json:"setup_hints,omitempty"`
}

// ConnectorCapability is one Aura-level capability exposed by a provider profile.
type ConnectorCapability struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Description    string `json:"description,omitempty"`
	Enabled        bool   `json:"enabled"`
	ReviewRequired bool   `json:"review_required"`
}

// ConnectorRiskBadge lets the dashboard show risk without parsing prose.
type ConnectorRiskBadge struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Level string `json:"level"` // "low" | "medium" | "high"
}

// ConnectorProbeResponse reports whether a provider profile can satisfy
// Aura's approved capability contract against a currently connected MCP server.
type ConnectorProbeResponse struct {
	OK                      bool     `json:"ok"`
	ProviderID              string   `json:"provider_id"`
	ServerName              string   `json:"server_name,omitempty"`
	CapabilitiesReady       []string `json:"capabilities_ready,omitempty"`
	MissingCapabilities     []string `json:"missing_capabilities,omitempty"`
	ApprovedToolsAdvertised []string `json:"approved_tools_advertised,omitempty"`
	BlockedToolsAdvertised  []string `json:"blocked_tools_advertised,omitempty"`
	Error                   string   `json:"error,omitempty"`
}

// MCPInvokeResponse is the body of POST /mcp/{server}/tools/{tool}.
// OK=true means the server returned success. OK=false means either the
// server returned isError:true, the request timed out, or the transport failed.
type MCPInvokeResponse struct {
	OK      bool   `json:"ok"`
	IsError bool   `json:"is_error,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DatabaseSetupStatus is the dashboard-facing state for the guided database
// MCP wizard. Passwords are never returned.
type DatabaseSetupStatus struct {
	Configured         bool   `json:"configured"`
	Connected          bool   `json:"connected"`
	NeedsRestart       bool   `json:"needs_restart"`
	RestartRequired    bool   `json:"restart_required"`
	CanRestart         bool   `json:"can_restart"`
	BinaryPresent      bool   `json:"binary_present"`
	Command            string `json:"command,omitempty"`
	Provider           string `json:"provider,omitempty"`
	SQLitePath         string `json:"sqlite_path,omitempty"`
	Host               string `json:"host,omitempty"`
	Port               int    `json:"port,omitempty"`
	Database           string `json:"database,omitempty"`
	User               string `json:"user,omitempty"`
	SSL                bool   `json:"ssl,omitempty"`
	PasswordConfigured bool   `json:"password_configured,omitempty"`
	Error              string `json:"error,omitempty"`
}

// DatabaseSetupRequest writes the single managed ExecuteAutomation database
// MCP entry. Provider is one of sqlite, postgresql, mysql, or sqlserver.
type DatabaseSetupRequest struct {
	Provider   string `json:"provider"`
	SQLitePath string `json:"sqlite_path,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Database   string `json:"database,omitempty"`
	User       string `json:"user,omitempty"`
	Password   string `json:"password,omitempty"`
	SSL        bool   `json:"ssl,omitempty"`
}

// DatabaseSetupResponse is the body of POST /mcp/database/setup.
type DatabaseSetupResponse struct {
	OK     bool                `json:"ok"`
	Status DatabaseSetupStatus `json:"status"`
}
