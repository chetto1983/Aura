package agui

// credit_api.go implements GET/POST /api/admin/identities/{id}/credit (CRED-03,
// CRED-06, CRED-09), on internal/agui/audit_api.go's pattern: ports declared
// consumer-side, a Set*-after-construct wiring method, 503 until wired.
//
// GET returns the cap, reset interval, period spend and remaining, sourced from
// Task 1's credit_ledger.go over Aura's own in-band ledger (D-08) -- never from
// GET /api/v1/key's usage field, which lags a spend by 30-40s (M-07) and is
// reconciliation, never a live balance. It returns nothing about the key itself: not
// the raw key, not the hash, not the label -- the Credit panel does not show the
// label (the Top-Identities list in a later plan does, from a different source), so
// it has no business here.
//
// POST applies the CRED-03 precision rule (half-up to two decimals,
// openrouterprovision.NewUSDCapFromString) and writes the store BEFORE patching the
// provider: a provider failure after a successful store write is reconcilable (the
// store already reflects the admin's intent; a retry re-sends the same PATCH),
// whereas patching first and having the STORE write fail would leave an admin who
// saw "updated" with nothing recorded locally -- a silent divergence in the
// direction that is harder to notice. The response always says which half landed.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
	"github.com/chetto1983/aura/internal/runner"
)

// creditSpendReader is Task 1's PgSpendReader narrowed to what this file needs.
type creditSpendReader interface {
	PeriodSpend(ctx context.Context, identityID, limitReset string) (float64, error)
}

// creditKeyStore matches *identitykey.Store's own Load/Save signatures exactly (both
// ctx-scoped by whoever calls them, per that package's doc) so the concrete store
// satisfies this port with no adapter code. The credit handlers below scope ctx to
// the TARGET identity from the URL path before every call -- never to the admin
// caller's own principal.
type creditKeyStore interface {
	Load(ctx context.Context) (identitykey.Record, error)
	Save(ctx context.Context, r identitykey.Record) error
}

// creditProvider patches an identity's OpenRouter key cap/reset-interval. The
// composition-root adapter closes over the http.Client, base URL and management
// credential and calls openrouterprovision.PatchKey.
type creditProvider interface {
	PatchCap(ctx context.Context, hash string, patch openrouterprovision.KeyPatch) (openrouterprovision.KeyRecord, error)
}

// creditCacheInvalidator drops a resolver's cached client for identityID after a cap
// change, so a top-up is not defeated by a stale cached client for M-06's ~25s
// window. *runner.IdentityLLMResolver satisfies it.
type creditCacheInvalidator interface {
	Invalidate(identityID string)
}

// NewCreditInvalidator adapts a possibly-nil *runner.IdentityLLMResolver into a
// creditCacheInvalidator, exported so the composition root (cmd/aura, which cannot
// name this file's unexported interface type) can pass a concrete resolver through
// without the #2924-class typed-nil-in-interface trap: the nil check happens HERE,
// on the concrete pointer, before it is ever boxed into the interface return value —
// a nil resolver returns a TRUE nil interface, never a non-nil interface wrapping a
// nil pointer that would panic the first time credit_api.go called Invalidate on it.
func NewCreditInvalidator(r *runner.IdentityLLMResolver) creditCacheInvalidator {
	if r == nil {
		return nil
	}
	return r
}

// creditPorts bundles this file's dependencies into one Server field (server.go
// gains one line, not four).
type creditPorts struct {
	spend        creditSpendReader
	keys         creditKeyStore
	provider     creditProvider
	invalidate   creditCacheInvalidator
	backendBills bool
}

// SetCreditAPI wires the credit routes. backendBills is the deployment-wide
// classification (D-13): false means the configured LLM backend does not charge at
// all (a local llama.cpp/Ollama server), which makes CRED-09's exemption uniform
// across every identity rather than a per-identity decision. Until called, both
// routes answer 503.
func (s *Server) SetCreditAPI(spend creditSpendReader, keys creditKeyStore, provider creditProvider, invalidate creditCacheInvalidator, backendBills bool) {
	s.credit = &creditPorts{spend: spend, keys: keys, provider: provider, invalidate: invalidate, backendBills: backendBills}
}

func (s *Server) registerCreditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/identities/{id}/credit", s.handleGetCredit)
	mux.HandleFunc("POST /api/admin/identities/{id}/credit", s.handleSetCredit)
}

func (s *Server) handleGetCredit(w http.ResponseWriter, r *http.Request) {
	if s.credit == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "credit API not configured"})
		return
	}
	targetID := r.PathValue("id")
	if _, err := uuid.Parse(targetID); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid identity id"})
		return
	}
	if !s.credit.backendBills {
		// CRED-09: a deployment-wide choice, uniform across every identity -- never a
		// fabricated $0.00 for a backend that bills nothing.
		writeJSON(w, map[string]any{"identity_id": targetID, "exempt": true})
		return
	}
	if s.credit.keys == nil || s.credit.spend == nil {
		// A billing backend with no management credential/store wired: the
		// composition root's own SetCreditAPI call site never produces this
		// combination deliberately (see cmd/aura/serve_agui.go), but a defensive
		// 503 here is cheaper than a nil-pointer panic if that ever changes.
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "credit store not configured for this backend"})
		return
	}

	ctx := identityctx.WithIdentityID(r.Context(), targetID)
	rec, loadErr := s.credit.keys.Load(ctx)
	if loadErr != nil {
		if errors.Is(loadErr, identitykey.ErrNoKey) {
			writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "identity has no OpenRouter key yet"})
			return
		}
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "credit store unavailable"})
		return
	}

	spendFloat, err := s.credit.spend.PeriodSpend(ctx, targetID, rec.LimitReset)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "spend read unavailable"})
		return
	}
	cap, err := usdCapFromFloat(rec.LimitUSD)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "credit figures unavailable"})
		return
	}
	spend, err := usdCapFromFloat(spendFloat)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "credit figures unavailable"})
		return
	}
	writeJSON(w, creditGetResponse(targetID, rec.LimitReset, cap, spend))
}

// creditGetResponse builds the GET response map at the display boundary: cap and
// spend arrive already rounded to the cent (usdCapFromFloat); remaining and
// percent_used are derived here, in integer cents, so a spend exactly equal to the
// cap reads as percent_used=100 and remaining=0 (CRED-06 boundary probe) and one
// cent below reads under 100 -- integer division truncates rather than rounds, so a
// value one cent short of the cap can never round UP to 100.
func creditGetResponse(identityID, limitReset string, cap, spend openrouterprovision.USDCap) map[string]any {
	capCents := int64(cap)
	spendCents := int64(spend)
	remainingCents := max(capCents-spendCents, 0)
	percent := 100
	if capCents > 0 {
		percent = min(int(spendCents*100/capCents), 100)
		percent = max(percent, 0)
	}
	return map[string]any{
		"identity_id":    identityID,
		"exempt":         false,
		"cap":            cap,
		"reset_interval": limitReset,
		"spend":          spend,
		"remaining":      openrouterprovision.USDCap(remainingCents),
		"percent_used":   percent,
	}
}

// creditSetRequest is the POST body. Both fields are optional pointers so a caller
// can change either independently (CRED-03 adjacency probe) -- a nil field means
// "leave unchanged," matching openrouterprovision.KeyPatch's own convention.
type creditSetRequest struct {
	Cap           *string `json:"cap"`
	ResetInterval *string `json:"reset_interval"`
}

func (s *Server) handleSetCredit(w http.ResponseWriter, r *http.Request) {
	if s.credit == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "credit API not configured"})
		return
	}
	targetID := r.PathValue("id")
	if _, err := uuid.Parse(targetID); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid identity id"})
		return
	}
	if !s.credit.backendBills {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "this deployment's backend does not bill; there is no cap to set"})
		return
	}
	if s.credit.keys == nil || s.credit.provider == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "credit store not configured for this backend"})
		return
	}
	raw, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	var body creditSetRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if body.Cap == nil && body.ResetInterval == nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "at least one of cap or reset_interval is required"})
		return
	}

	var patch openrouterprovision.KeyPatch
	var appliedCapPtr *openrouterprovision.USDCap
	if body.Cap != nil {
		cap, err := openrouterprovision.NewUSDCapFromString(*body.Cap)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid cap: " + SanitizeString(err.Error())})
			return
		}
		patch.Limit = &cap
		appliedCapPtr = &cap
	}
	var appliedReset string
	if body.ResetInterval != nil {
		reset := openrouterprovision.LimitReset(strings.ToLower(strings.TrimSpace(*body.ResetInterval)))
		if !validLimitReset(reset) {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "reset_interval must be one of daily, weekly, monthly"})
			return
		}
		patch.LimitReset = &reset
		appliedReset = string(reset)
	}

	ctx := identityctx.WithIdentityID(r.Context(), targetID)
	rec, loadErr := s.credit.keys.Load(ctx)
	if loadErr != nil {
		if errors.Is(loadErr, identitykey.ErrNoKey) {
			writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "identity has no OpenRouter key yet"})
			return
		}
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "credit store unavailable"})
		return
	}

	finalLimitUSD := rec.LimitUSD
	if appliedCapPtr != nil {
		finalLimitUSD = float64(*appliedCapPtr) / 100
	}
	finalLimitReset := rec.LimitReset
	if appliedReset != "" {
		finalLimitReset = appliedReset
	}

	// SEEDED RED DEFECT (removed in the GREEN commit): PATCH called BEFORE the store
	// write, reversing the plan's own instruction. TestAdminSetCreditProviderFailureAfterStoreWrite
	// fails for real against this ordering: keys.saveCalls is 0 (Save has not run
	// yet) when the provider fails, contradicting the test's premise.
	if _, err := s.credit.provider.PatchCap(ctx, rec.Hash, patch); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{
			"error":            "cap saved locally but the provider update failed; retry to sync the provider",
			"store_applied":    true,
			"provider_applied": false,
		})
		return
	}

	if err := s.credit.keys.Save(ctx, identitykey.Record{
		Key: rec.Key, Hash: rec.Hash, Label: rec.Label,
		LimitUSD: finalLimitUSD, LimitReset: finalLimitReset,
	}); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{
			"error": "store write failed; nothing changed",
		})
		return
	}

	if s.credit.invalidate != nil {
		s.credit.invalidate.Invalidate(targetID)
	}

	displayCap, err := usdCapFromFloat(finalLimitUSD)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "credit figures unavailable"})
		return
	}
	writeJSON(w, map[string]any{
		"identity_id":      targetID,
		"cap":              displayCap,
		"reset_interval":   finalLimitReset,
		"store_applied":    true,
		"provider_applied": true,
	})
}

// validLimitReset reports whether reset is one of the three documented intervals.
// openrouterprovision's own LimitReset.valid() method is unexported, so this checks
// membership against the SAME three exported constants rather than re-declaring a
// second string literal set.
func validLimitReset(reset openrouterprovision.LimitReset) bool {
	switch reset {
	case openrouterprovision.LimitResetDaily, openrouterprovision.LimitResetWeekly, openrouterprovision.LimitResetMonthly:
		return true
	default:
		return false
	}
}

// usdCapFromFloat rounds a float64 USD amount to the nearest cent, reusing
// openrouterprovision's own half-up rounding rule rather than a second
// implementation of it (CLAUDE.md REUSABLE CODE) -- this is the DISPLAY rounding
// credit_ledger.go's own doc comment defers to this file.
func usdCapFromFloat(f float64) (openrouterprovision.USDCap, error) {
	if f < 0 {
		f = 0
	}
	return openrouterprovision.NewUSDCapFromString(strconv.FormatFloat(f, 'f', -1, 64))
}
