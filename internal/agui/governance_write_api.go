package agui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// governance_write_api.go is the Phase-29 thin REST adapter over the MCPWriteProvider seam
// (MCPW-01/02/03). Six method+path-specific routes under the /api/ carve-out install a
// recipe/custom server, edit one server's env in place (redacted four-state), operator-
// trust-approve, reversibly enable/disable, and confirm-remove — every mutation atomic with
// its one mcp_audit row (the provider's WriteConfigWithAudit). There is NO business logic
// here: each handler nil-checks the write provider (503 when unwired), size-caps + decodes
// + sanitizes the body, reads the authenticated principal as the audit actor, makes ONE
// provider call, and projects to JSON — mirroring governance_api.go + onboarding_api.go.
//
// The parent-mux mount behind RequireAuth → RequireCapability(governance.write) is
// cmd/aura/serve_webui.go's job (the capability gate is inherited; an operator without the
// grant is 403 before any handler runs). Env VALUES are NEVER serialized — only redacted
// KEY chips (envChips) cross the wire (Pitfall 4 / T-29-02-01). Every wire error passes
// through sanitizeErr (no DSN/token/host leak).

// mcpWriteResponse is the common install/env/trust/lifecycle response. EnvKeys are key-only
// redacted chips (the value never crosses the wire); cliEquivalent + destination preview the
// install; warnings carry the soft required-var-still-placeholder list (the save still
// succeeded); probe is the post-write live tool-count (fail-soft, omitted when absent).
type mcpWriteResponse struct {
	Name          string           `json:"name"`
	EnvKeys       []mcpEnvChip     `json:"envKeys"`
	CLIEquivalent string           `json:"cliEquivalent,omitempty"`
	Destination   string           `json:"destination,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	Probe         *mcp.ProbeResult `json:"probe,omitempty"`
}

// mcpEnableBody / mcpTrustBody are the small mutation bodies. enable/disable carry no body
// (the verb is the intent); trust carries the operator-authored reason + the target class.
type mcpTrustBody struct {
	Class  string `json:"class"`
	Reason string `json:"reason"`
}

// registerGovernanceWriteRoutes mounts the six MCP write routes on the supplied mux using
// Go 1.22 method-pattern routing — SPECIFIC method+path siblings under the /api/ carve-out,
// never a bare /api/ (which would shadow /api/integrations/, Pitfall 5). The parent-mux
// mount behind RequireCapability(governance.write) lives in cmd/aura/serve_webui.go.
func (s *Server) registerGovernanceWriteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/governance/mcp", s.handleMCPInstall)
	mux.HandleFunc("PATCH /api/governance/mcp/{name}/env", s.handleMCPEnvEdit)
	mux.HandleFunc("POST /api/governance/mcp/{name}/trust", s.handleMCPTrust)
	mux.HandleFunc("POST /api/governance/mcp/{name}/enable", s.handleMCPEnable)
	mux.HandleFunc("POST /api/governance/mcp/{name}/disable", s.handleMCPDisable)
	mux.HandleFunc("DELETE /api/governance/mcp/{name}", s.handleMCPRemove)
	s.registerGovernanceSkillsWriteRoutes(mux)
	s.registerGovernanceSchedulerWriteRoutes(mux)
}

// handleMCPInstall serves POST /api/governance/mcp (MCPW-01): install a recipe or custom
// stdio/HTTP server. A duplicate name → 409 (no second servers.json entry); success writes
// the entry atomically + one install audit row, and the response previews the CLI-equiv +
// the ManagedConfigPath() destination and re-probes for the live tool count.
func (s *Server) handleMCPInstall(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginMCPWrite(w, r)
	if !ok {
		return
	}
	var body MCPInstallRequest
	if !decodeMCPBody(w, r, &body) {
		return
	}
	res, err := s.governanceWrite.MCP.InstallServer(r.Context(), actor, body)
	if err != nil {
		writeMCPWriteError(w, err)
		return
	}
	writeJSON(w, mcpWriteResponseFrom(res))
}

// handleMCPEnvEdit serves PATCH /api/governance/mcp/{name}/env (MCPW-02): apply the
// submitted env over the stored entry, preserving any secret left as its redacted ${KEY}
// placeholder (SetServerEnv) → atomic write + one edit audit row. The response carries ONLY
// key-only env chips + the soft required-var warning list; no raw value crosses the wire.
func (s *Server) handleMCPEnvEdit(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginMCPWrite(w, r)
	if !ok {
		return
	}
	var body struct {
		Env []string `json:"env"`
	}
	if !decodeMCPBody(w, r, &body) {
		return
	}
	res, err := s.governanceWrite.MCP.SetServerEnv(r.Context(), actor, r.PathValue("name"), body.Env)
	if err != nil {
		writeMCPWriteError(w, err)
		return
	}
	writeJSON(w, mcpWriteResponseFrom(res))
}

// handleMCPTrust serves POST /api/governance/mcp/{name}/trust (MCPW-03, D-12): the
// operator-direct trust-approve (NOT a model gate). At the HTTP boundary it REQUIRES a known
// trust class + a non-empty reason (a blank/`{}`/empty body is a 400 with no provider call —
// so a blocked server is never silently defaulted to trusted_local). It populates the
// today-empty Trust.{ApprovedBy:principal, ApprovedAt:now, Reason} → atomic write + one trust
// audit row (carrying the reason) → flips runnable → re-probes for the live tool count.
func (s *Server) handleMCPTrust(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginMCPWrite(w, r)
	if !ok {
		return
	}
	var body mcpTrustBody
	if !decodeMCPBody(w, r, &body) {
		return
	}
	class := strings.TrimSpace(body.Class)
	reason := strings.TrimSpace(body.Reason)
	if class == "" || reason == "" || !mcp.IsKnownTrust(class) {
		http.Error(w, "trust requires a known class and a non-empty reason", http.StatusBadRequest)
		return
	}
	res, err := s.governanceWrite.MCP.TrustApprove(r.Context(), actor, r.PathValue("name"), class, reason)
	if err != nil {
		writeMCPWriteError(w, err)
		return
	}
	writeJSON(w, mcpWriteResponseFrom(res))
}

// handleMCPEnable / handleMCPDisable serve the reversible lifecycle toggles (MCPW-02): one
// audit row per real transition (idempotent — re-enabling an enabled server is a no-op the
// provider still treats as one named action). No body.
func (s *Server) handleMCPEnable(w http.ResponseWriter, r *http.Request) {
	s.handleMCPSetEnabled(w, r, true)
}
func (s *Server) handleMCPDisable(w http.ResponseWriter, r *http.Request) {
	s.handleMCPSetEnabled(w, r, false)
}

func (s *Server) handleMCPSetEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	actor, ok := s.beginMCPWrite(w, r)
	if !ok {
		return
	}
	res, err := s.governanceWrite.MCP.SetEnabled(r.Context(), actor, r.PathValue("name"), enabled)
	if err != nil {
		writeMCPWriteError(w, err)
		return
	}
	writeJSON(w, mcpWriteResponseFrom(res))
}

// handleMCPRemove serves DELETE /api/governance/mcp/{name} (MCPW-02): remove the entry →
// atomic write + one remove audit row. A second delete 404s (≤1 audit row). Returns 204.
func (s *Server) handleMCPRemove(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginMCPWrite(w, r)
	if !ok {
		return
	}
	if err := s.governanceWrite.MCP.RemoveServer(r.Context(), actor, r.PathValue("name")); err != nil {
		writeMCPWriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// beginMCPWrite is the shared front door for every MCP write handler: nil-check the write
// provider (503 when unwired) and read the authenticated principal as the audit actor (401
// if absent — though the parent-mux capability gate already bound it). It returns the actor
// identity id and whether the handler may proceed.
func (s *Server) beginMCPWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.governanceWrite.MCP == nil {
		http.Error(w, "mcp write not configured", http.StatusServiceUnavailable)
		return "", false
	}
	actor, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return actor, true
}

// decodeMCPBody size-caps + decodes a JSON body into dst, writing a clean 400 on a malformed
// body. An empty body is allowed (the verb-only enable/disable use no body; trust/install
// validate downstream in the provider).
func decodeMCPBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := strictDecodeJSON(w, r, dst, decodeOpts{allowEmpty: true}); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// mcpWriteResponseFrom projects a provider MCPWriteResult onto the wire response: the env is
// reduced to key-only redacted chips (envChips — value NEVER serialized), and the probe is
// projected to its safe JSON shape when present.
func mcpWriteResponseFrom(res MCPWriteResult) mcpWriteResponse {
	out := mcpWriteResponse{
		Name:          res.Name,
		EnvKeys:       envChips(res.Server.Env),
		CLIEquivalent: res.CLIEquivalent,
		Destination:   res.Destination,
		Warnings:      res.Warnings,
	}
	if res.Probe != nil {
		// The probe already redacts its own Detail/Err; SanitizeString is the
		// belt-and-suspenders over the wire string (parity with handleMCPProbe).
		p := *res.Probe
		p.Detail = SanitizeString(p.Detail)
		p.Err = SanitizeString(p.Err)
		out.Probe = &p
	}
	return out
}

// writeMCPWriteError maps a provider write error onto the right HTTP status with a sanitized
// body: a duplicate install → 409, a missing server → 404, anything else → a sanitized 502
// (a backend/FS/DB failure is not the client's fault). The typed sentinels are declared in
// governance_write_seam.go; the concrete provider returns them.
func writeMCPWriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrMCPServerExists):
		http.Error(w, "mcp server already exists", http.StatusConflict)
	case errors.Is(err, ErrMCPServerNotFound):
		http.Error(w, "mcp server not found", http.StatusNotFound)
	default:
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
	}
}
