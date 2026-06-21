package agui

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/mcp"
)

// ErrMCPServerExists / ErrMCPServerNotFound are the consumer-side write sentinels the
// handlers map to HTTP status (409 / 404). The concrete provider (cmd/aura) returns these
// — wrapping the manager-package ErrServerNotFound — so the handler need not import the
// manager package just to classify a duplicate or a missing server.
var (
	ErrMCPServerExists   = errors.New("mcp server already exists")
	ErrMCPServerNotFound = errors.New("mcp server not found")
)

// governance_write_seam.go declares the consumer-side narrow provider interface for the
// Phase-29 MCP WRITE surface (MCPW-01/02/03) plus its off-constructor injection setter
// (D-A2-02), mirroring governance_seam.go for the read board. The interface is declared
// HERE (the consumer) so each write handler depends only on the method it calls; the
// daemon composition root (cmd/aura) wires a concrete provider via SetGovernanceWriteProviders
// after NewServer. A Server with the write provider unwired answers every MCP write route
// 503 — the handlers nil-check the field exactly as the read handlers do.
//
// Each concrete method runs the plan-01/task-1 atomic wrapper (WriteConfigWithAudit:
// temp config write → InsertMCPAuditTx inside db.WithTx → os.Rename on commit), so a
// config mutation cannot land without its one immutable mcp_audit row (D-04). The handler
// stays wire-aware (size-cap, decode, sanitize, envChips key-only projection) and the
// provider stays FS/DB/probe-aware.

// MCPInstallRequest is the install body: either a recipe `Name` (resolved against the
// built-in catalog) or a custom stdio/HTTP server. Env carries the operator-supplied
// values (a secret may arrive as its redacted ${KEY} placeholder, preserved by the merge).
type MCPInstallRequest struct {
	Name    string   `json:"name"`
	Recipe  string   `json:"recipe,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	Type    string   `json:"type,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// MCPWriteResult is the common provider result the handlers project to the wire. Server is
// the resulting managed server (the handler emits ONLY its env KEY chips, never a value);
// CLIEquivalent + Destination preview the install for the response; Warnings carry the soft
// required-var-still-placeholder list (informational — the save still succeeded); Probe is
// the post-install/post-trust live tool-count (fail-soft, per-row).
type MCPWriteResult struct {
	Name          string
	Server        mcp.ManagedServer
	CLIEquivalent string
	Destination   string
	Warnings      []string
	Probe         *mcp.ProbeResult
}

// MCPWriteProvider is the operator-only MCP config-mutation surface (MCPW-01/02/03). Each
// method persists atomically with its audit row (WriteConfigWithAudit) and is reachable
// only behind RequireCapability(governance.write) — there is NO model-facing path. Install
// rejects a duplicate name (ErrServerExists → 409); env-edit preserves untouched secrets;
// trust-approve populates the today-empty ApprovedBy/At/Reason + re-probes; enable/disable
// is idempotent (one audit row per real transition); remove 404s a second delete.
type MCPWriteProvider interface {
	InstallServer(ctx context.Context, actorIdentityID string, req MCPInstallRequest) (MCPWriteResult, error)
	SetServerEnv(ctx context.Context, actorIdentityID, name string, submitted []string) (MCPWriteResult, error)
	TrustApprove(ctx context.Context, actorIdentityID, name, class, reason string) (MCPWriteResult, error)
	SetEnabled(ctx context.Context, actorIdentityID, name string, enabled bool) (MCPWriteResult, error)
	RemoveServer(ctx context.Context, actorIdentityID, name string) error
}

// GovernanceWriteProviders bundles the Phase-29 write providers so the composition root
// wires them in one call (parity with GovernanceProviders). A nil field → its routes 503.
type GovernanceWriteProviders struct {
	MCP MCPWriteProvider
}

// SetGovernanceWriteProviders wires the Phase-29 governance WRITE providers (MCPW-01/02/03).
// Set by the daemon composition root after NewServer; until set, the MCP write routes
// (registered in registerGovernanceWriteRoutes) answer 503. Kept off the constructor so
// existing NewServer callers/tests stay unchanged (D-A2-02 narrow seam).
func (s *Server) SetGovernanceWriteProviders(p GovernanceWriteProviders) { s.governanceWrite = p }
