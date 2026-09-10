package agui

// openrouter_reconcile.go is EnsureOpenRouterKeys: it mints every OpenRouter key the
// deployment is missing and aligns each key's limit with its owner's role. It is idempotent,
// so boot, the settings writes that can make minting possible, and the admin endpoint all run
// the same thing.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/openrouterprovision"
)

const (
	servicesKeySetting  = "OPENROUTER_API_KEY"
	servicesCapSetting  = "AURA_OPENROUTER_SERVICES_CAP_USD"
	servicesKeyName     = "aura-services"
	reconcileActor      = "aura-reconciler"
	serviceIdentityKind = "service" // migration 0049: principals that can never log in
)

// reconcileTriggerKeys are the settings whose write can make minting possible.
var reconcileTriggerKeys = map[string]struct{}{
	"AURA_OPENROUTER_MANAGEMENT_KEY": {},
	servicesCapSetting:               {},
	"AURA_LLM_PROVIDER":              {},
	"AURA_LLM_BASE_URL":              {},
}

// ErrServicesCapUnset keeps the services key unminted until the admin picks its monthly cap.
var ErrServicesCapUnset = errors.New("openrouter reconcile: the services key's monthly cap is not set")

// OpenRouterKeysResult is what one run did, for the first-run wizard, the settings writes and
// the log.
type OpenRouterKeysResult struct {
	Skipped          string   `json:"skipped,omitempty"`
	ServicesLabel    string   `json:"services_label,omitempty"`
	IdentitiesMinted []string `json:"identities_minted"`
	LimitsAligned    []string `json:"limits_aligned"`
	Errors           []string `json:"errors,omitempty"`
}

// SetOpenRouterKeys wires the reconciler. Until it is set, nothing is minted.
func (s *Server) SetOpenRouterKeys(minter *IdentityKeyMinter) { s.keyMinter = minter }

func (s *Server) registerOpenRouterKeysRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/openrouter/reconcile", s.handleReconcileOpenRouterKeys)
}

// EnsureOpenRouterKeys runs the reconciler under settingsMu.
func (s *Server) EnsureOpenRouterKeys(ctx context.Context) (OpenRouterKeysResult, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.ensureOpenRouterKeysLocked(ctx)
}

// ensureOpenRouterKeysLocked is the body. The settings handlers call it while they still hold
// settingsMu, right after a write that can make minting possible.
func (s *Server) ensureOpenRouterKeysLocked(ctx context.Context) (OpenRouterKeysResult, error) {
	res := OpenRouterKeysResult{IdentitiesMinted: []string{}, LimitsAligned: []string{}}
	if s.keyMinter == nil || s.settings == nil || s.llmRouteReloader == nil || s.idAdmin == nil {
		return res, nil
	}
	skip, err := s.keyMinter.readiness(ctx)
	if err != nil || skip != "" {
		res.Skipped = skip
		return res, err
	}
	var errs []error
	label, err := s.ensureServicesKeyLocked(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("services key: %w", err))
	}
	res.ServicesLabel = label
	ids, err := s.idAdmin.ListIdentities(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("list identities: %w", err))
	}
	for _, idn := range ids {
		if idn.Kind == serviceIdentityKind || idn.Deactivated {
			continue
		}
		if err := s.reconcileIdentity(ctx, idn.ID, &res); err != nil {
			errs = append(errs, fmt.Errorf("identity %s: %w", idn.ID, err))
		}
	}
	for _, err := range errs {
		res.Errors = append(res.Errors, SanitizeString(err.Error()))
	}
	return res, errors.Join(errs...)
}

// reconcileIdentity mints the identity's key, or aligns an existing one with its role.
func (s *Server) reconcileIdentity(ctx context.Context, identityID string, res *OpenRouterKeysResult) error {
	_, created, err := s.keyMinter.ensure(ctx, identityID, identityID)
	if err != nil {
		return err
	}
	if created {
		res.IdentitiesMinted = append(res.IdentitiesMinted, identityID)
		return nil
	}
	aligned, err := s.keyMinter.alignLimit(ctx, identityID)
	if err != nil {
		return err
	}
	if aligned {
		res.LimitsAligned = append(res.LimitsAligned, identityID)
		if s.credit != nil && s.credit.invalidate != nil {
			s.credit.invalidate.Invalidate(identityID)
		}
	}
	return nil
}

// ensureServicesKeyLocked mints the aura-services key when the settings hold none. It writes
// the key the way a settings PUT does: Prepare with the whole persisted profile, so the route
// is kept (Prepare resets every profile key it is not given), then ReplaceMany, then apply.
// The new key is revoked if either step fails, so a key the deployment never recorded does not
// stay live at the provider.
func (s *Server) ensureServicesKeyLocked(ctx context.Context) (string, error) {
	rows, err := s.settings.List(ctx)
	if err != nil {
		return "", err
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row.Key] = strings.TrimSpace(row.Value)
	}
	if values[servicesKeySetting] != "" {
		return "", nil
	}
	if values[servicesCapSetting] == "" {
		return "", ErrServicesCapUnset
	}
	limit, err := openrouterprovision.NewUSDCapFromString(values[servicesCapSetting])
	if err != nil {
		return "", fmt.Errorf("services cap: %w", err)
	}
	minted, err := s.keyMinter.minting.Mint(ctx, openrouterprovision.MintRequest{
		IdentityID: servicesKeyName, Name: servicesKeyName, Limit: &limit, LimitReset: openrouterprovision.LimitResetMonthly,
	})
	if err != nil {
		return "", err
	}
	overrides := llmProfileOverrides(rows)
	overrides[servicesKeySetting] = minted.Key
	apply, err := s.llmRouteReloader.Prepare(ctx, overrides, nil)
	if err == nil {
		_, err = s.settings.ReplaceMany(ctx, map[string]string{servicesKeySetting: minted.Key}, nil, reconcileActor)
	}
	if err != nil {
		s.keyMinter.revokeUnrecorded(ctx, minted.Record.Hash)
		return "", err
	}
	apply()
	return minted.Record.Label, nil
}

// handleReconcileOpenRouterKeys runs the reconciler on demand and returns what it did, so the
// first-run wizard can show the new keys or the provider's error. The parent mux gates it on
// identity.create.
func (s *Server) handleReconcileOpenRouterKeys(w http.ResponseWriter, r *http.Request) {
	if s.keyMinter == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "OpenRouter key minting not configured"})
		return
	}
	res, err := s.EnsureOpenRouterKeys(r.Context())
	if err != nil {
		slog.Warn("openrouter reconcile", "err", err)
	}
	writeJSON(w, res)
}

// reconcileAfterSettingsWrite runs the reconciler inside a settings handler, which already holds
// settingsMu, when one of keys can make minting possible. A failure is logged and reported in
// the result; the write itself stands.
func (s *Server) reconcileAfterSettingsWrite(ctx context.Context, keys ...string) *OpenRouterKeysResult {
	if s.keyMinter == nil || !touchesReconcileTrigger(keys) {
		return nil
	}
	res, err := s.ensureOpenRouterKeysLocked(ctx)
	if err != nil {
		slog.Warn("openrouter reconcile after a settings write", "err", err)
	}
	return &res
}

func touchesReconcileTrigger(keys []string) bool {
	for _, key := range keys {
		if _, ok := reconcileTriggerKeys[key]; ok {
			return true
		}
	}
	return false
}
