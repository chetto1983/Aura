package agui

import (
	"net/http"

	"github.com/chetto1983/aura/internal/identity"
)

// adminOnlySettingKeys decide which credential, which route and which model the whole
// deployment runs on. Every identity holds governance.write (D-01), so writing them also
// takes identity.create, the capability that makes an identity an admin.
var adminOnlySettingKeys = map[string]struct{}{
	"AURA_OPENROUTER_MANAGEMENT_KEY":   {},
	"AURA_OPENROUTER_SERVICES_CAP_USD": {},
	"AURA_LLM_PROVIDER":                {},
	"AURA_LLM_MODEL":                   {},
	"AURA_LLM_BASE_URL":                {},
}

// mintedSettingKeys are written only by the reconciler: nobody types the services key.
var mintedSettingKeys = map[string]struct{}{"OPENROUTER_API_KEY": {}}

// callTimeSettingKeys are read from the store on every use, so a saved value is live at once.
var callTimeSettingKeys = map[string]struct{}{
	"AURA_OPENROUTER_MANAGEMENT_KEY":   {},
	"AURA_OPENROUTER_SERVICES_CAP_USD": {},
}

func isCallTimeSetting(key string) bool {
	_, ok := callTimeSettingKeys[key]
	return ok
}

// authorizeSettingWrite refuses, and answers for, a write the caller may not make: a minted key
// from anyone, an admin-only key — or, with requireAdmin, any key — from a member. It returns
// true when the write may go ahead.
func (s *Server) authorizeSettingWrite(w http.ResponseWriter, r *http.Request, actor string, requireAdmin bool, keys ...string) bool {
	for _, key := range keys {
		if _, minted := mintedSettingKeys[key]; minted {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": key + " is minted by Aura and cannot be set"})
			return false
		}
		if _, adminOnly := adminOnlySettingKeys[key]; adminOnly {
			requireAdmin = true
		}
	}
	if !requireAdmin {
		return true
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
