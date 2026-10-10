package agui

import (
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/identity"
)

// mintedSettingKeys are written only by the reconciler: nobody types the services key.
var mintedSettingKeys = map[string]struct{}{"OPENROUTER_API_KEY": {}}

// callTimeSettingKeys are read from the store on every use, so a saved value is live at once.
var callTimeSettingKeys = map[string]struct{}{
	"CLOUDFLARE_API_TOKEN":             {},
	"CLOUDFLARE_TUNNEL_TOKEN":          {},
	"AURA_OPENROUTER_MANAGEMENT_KEY":   {},
	"AURA_OPENROUTER_SERVICES_CAP_USD": {},
	// The two media models and the inline wait are read fresh on every generation
	// call (cmd/aura's mediaSettings); the asset byte ceiling is boot-only, so it is
	// deliberately absent here.
	"AURA_IMAGE_MODEL":           {},
	"AURA_VIDEO_MODEL":           {},
	"AURA_VIDEO_INLINE_WAIT_SEC": {},
}

func isCallTimeSetting(key string) bool {
	_, ok := callTimeSettingKeys[key]
	return ok
}

// callerIsAdmin fails closed: no identity seam, no principal or a failed read all mean "member".
func (s *Server) callerIsAdmin(r *http.Request) bool {
	id, ok := principalIdentityID(r)
	if !ok || s.idAdmin == nil {
		return false
	}
	admin, err := s.idAdmin.HasCapability(r.Context(), id, identity.CapIdentityCreate)
	if err != nil {
		slog.Warn("admin check failed", "path", r.URL.Path, "err", err)
		return false
	}
	return admin
}

// authorizeSettingWrite refuses, and answers for, a write the caller may not make: a minted key
// or a Cloudflare token from anyone, and any setting from a member. aura.settings has no
// identity column, so every key configures the whole deployment, and every identity holds
// governance.write (D-01): writing one takes identity.create (prd.md §3, 2026-10-10). It returns
// true when the write may go ahead.
func (s *Server) authorizeSettingWrite(w http.ResponseWriter, r *http.Request, actor string, keys ...string) bool {
	for _, key := range keys {
		if key == "CLOUDFLARE_API_TOKEN" || key == "CLOUDFLARE_TUNNEL_TOKEN" {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "use the dedicated Remote access controls"})
			return false
		}
		if _, minted := mintedSettingKeys[key]; minted {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": key + " is minted by Aura and cannot be set"})
			return false
		}
	}
	if s.idAdmin == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "identity admin not configured"})
		return false
	}
	isAdmin, err := s.idAdmin.HasCapability(r.Context(), actor, identity.CapIdentityCreate)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "capability store unavailable"})
		return false
	}
	if !isAdmin {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "only an admin can change this setting"})
		return false
	}
	return true
}
