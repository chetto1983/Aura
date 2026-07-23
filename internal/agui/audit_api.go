package agui

// audit_api.go is the Phase-36 (MUSR-01) admin/user distinction backend (D-03/D-26/D-28):
//
//   - GET /api/me — SELF-scoped: the authenticated caller's own capability set, so the SPA
//     can hide admin surfaces when governance.write is absent (D-03). NOT admin-gated (a
//     user reading their OWN capabilities is not a privileged action); it inherits the
//     whole-origin RequireAuth from the parent mux.
//   - GET /api/admin/identities — admin: the identity roster + each identity's grants,
//     backing the grant/revoke picker + the audit picker.
//   - POST /api/admin/identities/{id}/capabilities — admin: grant a capability (D-26).
//   - DELETE /api/admin/identities/{id}/capabilities/{capability} — admin: revoke (D-26).
//   - GET /api/admin/audit?identity=<uuid> — admin: the per-user activity feed (D-28).
//
// The four /api/admin/* routes are gated server-side by RequireCapability(governance.write)
// at the parent-mux mount (cmd/aura/serve_webui_musr.go) — the SPA hide is cosmetic, NOT the
// security boundary (T-36-10-E). Capability names use the EXISTING governance.write (RESEARCH
// OQ3: no net-new settings.model.write). Grant/revoke go through the SAME validated
// identity.Store seam the CLI uses (D-26), and every capability mutation is audit-logged.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/google/uuid"
)

// auditReader is the D-28 admin per-user audit read seam (*PgAuditStore satisfies it).
// Declared consumer-side (D-A2-02) so the handler depends only on the one method it calls
// and audit_api_test.go can inject a fake with no pool.
type auditReader interface {
	ListActivityForIdentity(ctx context.Context, identityID string, limit, offset int) ([]AuditEvent, error)
}

// identityAdmin is the D-26 capability-management + identity-roster seam (*identity.Store
// satisfies it). Grant/Revoke route through the SAME validated store methods the CLI uses
// (name grammar + '*'-managed rejected at the store); ListIdentities/ListCapabilities back
// the admin picker. Declared consumer-side so a fake unit-tests the handlers.
//
// HasCapability (37F-13/WEBSHARE-04 SC4 row 8) is reused here — not a second seam — for
// share_api.go's handleShareCreate public-tier mint-time gate: a single POST /api/shares
// mux entry answers BOTH the internal (D-01 default, no capability) and public tier via the
// JSON body, so a tier-specific RequireCapability(share.public) cannot live at the parent
// mux (Go's ServeMux dispatches on method+path only, never on body content — see
// cmd/aura/serve_webui_share.go's file header and share_api.go's handleShareCreate doc for
// the full single-route/dual-tier constraint). The check therefore has to live in-handler,
// once the tier is known, over the SAME identity.Store method RequireCapability itself calls
// (auth.go's identityChecker.HasCapability) — reusing this seam avoids a second Set* wiring
// point at the composition root for what is structurally the identical capability read.
type identityAdmin interface {
	ListIdentities(ctx context.Context) ([]identity.Identity, error)
	ListCapabilities(ctx context.Context, identityID string) ([]string, error)
	GrantCapability(ctx context.Context, identityID, capability string) error
	RevokeCapability(ctx context.Context, identityID, capability string) error
	HasCapability(ctx context.Context, identityID, capability string) (bool, error)
}

// SetAuditStore wires the D-28 admin audit read store. Set by the daemon composition root
// after NewServer; until set, GET /api/admin/audit answers 503 (the SetSettingsStore
// precedent). Kept off the constructor so existing NewServer callers/tests stay unchanged.
func (s *Server) SetAuditStore(a auditReader) { s.audit = a }

// SetIdentityAdmin wires the D-26 capability-management + roster seam (*identity.Store).
// Set by the daemon composition root after NewServer; until set, /api/me + the admin
// identity/capability routes answer 503.
func (s *Server) SetIdentityAdmin(a identityAdmin) { s.idAdmin = a }

func (s *Server) registerAuditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("GET /api/admin/identities", s.handleAdminIdentities)
	mux.HandleFunc("POST /api/admin/identities/{id}/capabilities", s.handleGrantCapability)
	mux.HandleFunc("DELETE /api/admin/identities/{id}/capabilities/{capability}", s.handleRevokeCapability)
	mux.HandleFunc("GET /api/admin/audit", s.handleAdminAudit)
}

// meDTO is the self-scoped principal payload the SPA reads post-auth to gate admin
// surfaces: the caller's identity id + its capability names (the '*' wildcard is returned
// verbatim; the SPA treats it as "has everything", mirroring HasCapability). ContextWindow is
// the active model's total context window (tokens, llm.Config.ContextWindow via
// SetContextWindow) — the cockpit footer gauge (RuntimeFooter) reads it instead of hardcoding
// the DeepSeek-V4 1M default; 0 means unwired, and the frontend keeps its own fallback.
type meDTO struct {
	IdentityID    string   `json:"identity_id"`
	Capabilities  []string `json:"capabilities"`
	ContextWindow int      `json:"context_window"`
}

// handleMe returns the authenticated caller's own capability set. In production RequireAuth
// binds the principal; in loopback dev (no principal) it falls back to the seeded `local`
// operator (D-25) so the dev cockpit shows the admin surfaces.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.idAdmin == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "identity admin not configured"})
		return
	}
	id := principalFrom(r.Context())
	if id == "" {
		id = localIdentityID
	}
	caps, err := s.idAdmin.ListCapabilities(r.Context(), id)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "capability store unavailable"})
		return
	}
	writeJSON(w, meDTO{IdentityID: id, Capabilities: caps, ContextWindow: s.contextWindow})
}

// adminIdentityDTO is one row of the admin identity roster: the identity + its current
// grants. The user-authored Name is SanitizeString'd before the wire (T-36-10-I).
type adminIdentityDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Capabilities []string `json:"capabilities"`
}

func (s *Server) handleAdminIdentities(w http.ResponseWriter, r *http.Request) {
	if s.idAdmin == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "identity admin not configured"})
		return
	}
	ctx := r.Context()
	ids, err := s.idAdmin.ListIdentities(ctx)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "identity store unavailable"})
		return
	}
	out := make([]adminIdentityDTO, 0, len(ids))
	for _, idn := range ids {
		caps, err := s.idAdmin.ListCapabilities(ctx, idn.ID)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "capability store unavailable"})
			return
		}
		out = append(out, adminIdentityDTO{
			ID: idn.ID, Name: SanitizeString(idn.Name), Kind: idn.Kind, Capabilities: caps,
		})
	}
	writeJSON(w, map[string]any{"identities": out})
}

type capabilityBody struct {
	Capability string `json:"capability"`
}

func (s *Server) handleGrantCapability(w http.ResponseWriter, r *http.Request) {
	s.mutateCapability(w, r, true)
}

func (s *Server) handleRevokeCapability(w http.ResponseWriter, r *http.Request) {
	s.mutateCapability(w, r, false)
}

// mutateCapability is the shared grant/revoke body (D-26). It resolves the target identity
// (a valid UUID), reads the capability (JSON body on grant, path value on revoke), calls
// the SAME validated store method the CLI uses, audit-logs the change, and returns the
// updated grant set. A grammar/'*'-managed rejection from the store is a 400; other store
// failures are 502. The route is already RequireCapability(governance.write)-gated at the
// mount, so reaching here means the caller is an authorized admin (T-36-10-E).
func (s *Server) mutateCapability(w http.ResponseWriter, r *http.Request, grant bool) {
	if s.idAdmin == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "identity admin not configured"})
		return
	}
	targetID := r.PathValue("id")
	if _, err := uuid.Parse(targetID); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid identity id"})
		return
	}
	actor, ok := principalIdentityID(r)
	if !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	capName, ok := s.resolveCapabilityArg(w, r, grant)
	if !ok {
		return
	}

	var err error
	action := "grant"
	if grant {
		err = s.idAdmin.GrantCapability(r.Context(), targetID, capName)
	} else {
		action = "revoke"
		err = s.idAdmin.RevokeCapability(r.Context(), targetID, capName)
	}
	if err != nil {
		if errors.Is(err, identity.ErrWildcardManaged) || errors.Is(err, identity.ErrInvalidCapability) {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": SanitizeString(err.Error())})
			return
		}
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "capability store unavailable"})
		return
	}
	// D-26 audit trail: a capability mutation is a privileged admin action. It flows
	// through the single validated store seam and is recorded here with the actor +
	// target + capability (no secret material) so the change is attributable.
	slog.Info("aura admin: capability change", "action", action, "actor", actor, "target", targetID, "capability", capName)

	caps, err := s.idAdmin.ListCapabilities(r.Context(), targetID)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "capability store unavailable"})
		return
	}
	writeJSON(w, map[string]any{"identity_id": targetID, "capabilities": caps})
}

// resolveCapabilityArg reads the capability name for a grant (JSON body) or revoke (path
// value) and rejects an empty name with a 400.
func (s *Server) resolveCapabilityArg(w http.ResponseWriter, r *http.Request, grant bool) (string, bool) {
	if !grant {
		capName := strings.TrimSpace(r.PathValue("capability"))
		if capName == "" {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "capability required"})
			return "", false
		}
		return capName, true
	}
	raw, ok := readCappedBody(w, r)
	if !ok {
		return "", false
	}
	var body capabilityBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return "", false
	}
	capName := strings.TrimSpace(body.Capability)
	if capName == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "capability required"})
		return "", false
	}
	return capName, true
}

const (
	auditDefaultLimit = 50
	auditMaxLimit     = 200
)

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "audit store not configured"})
		return
	}
	identityID := strings.TrimSpace(r.URL.Query().Get("identity"))
	if _, err := uuid.Parse(identityID); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid identity id"})
		return
	}
	limit := clampAuditInt(r.URL.Query().Get("limit"), auditDefaultLimit, 1, auditMaxLimit)
	offset := clampAuditInt(r.URL.Query().Get("offset"), 0, 0, 1<<31-1)
	events, err := s.audit.ListActivityForIdentity(r.Context(), identityID, limit, offset)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "audit store unavailable"})
		return
	}
	// Belt-and-suspenders: server_name / skill_name / tool_name / reason / status can carry
	// user-authored text; sanitize each before the wire (T-36-10-I, mirrors sanitizeErr).
	for i := range events {
		events[i].Target = SanitizeString(events[i].Target)
		events[i].Detail = SanitizeString(events[i].Detail)
		// tool_call_id is model-generated text — same belt-and-suspenders.
		events[i].Correlation = SanitizeString(events[i].Correlation)
	}
	writeJSON(w, map[string]any{"events": events, "limit": limit, "offset": offset})
}

// clampAuditInt parses a query-int, falling back to def on absent/garbage and clamping to
// [min,max] so a hostile limit/offset can neither overrun nor go negative.
func clampAuditInt(raw string, def, min, max int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
