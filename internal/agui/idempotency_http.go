package agui

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/chetto1983/aura/internal/idempotency"
)

var errInvalidIdempotencyKey = errors.New("invalid Idempotency-Key")

// mutationKeyPolicy documents where an adapter obtains its caller-stable key.
type mutationKeyPolicy string

const keyPolicyRequiredHeader mutationKeyPolicy = "required_header"

// mutationRouteMeta is the fail-closed inventory consumed by coverage tests and
// the HTTP adapter. Normalize names a typed normalizer; it is deliberately Aura-
// owned metadata, never accepted from a request body.
type mutationRouteMeta struct {
	Scope     idempotency.Scope
	Normalize string
	KeyPolicy mutationKeyPolicy
}

// httpMutationRoutes inventories every state-changing AG-UI route. POST endpoints
// that are explicitly read/transform-only (graph query, TTS, STT, availability
// probes) are excluded because HTTP method alone is not mutation classification.
var httpMutationRoutes = map[string]mutationRouteMeta{
	"POST /agent/run": httpMutationMeta("agent_run"),
	"POST /api/admin/identities/{id}/capabilities":                httpMutationMeta("capability_grant"),
	"DELETE /api/admin/identities/{id}/capabilities/{capability}": httpMutationMeta("capability_revoke"),
	"POST /api/approvals/{token}/resolve":                         httpMutationMeta("approval_resolve"),
	"POST /api/assets/presign":                                    httpMutationMeta("asset_presign"),
	"POST /api/assets/{id}/finalize":                              httpMutationMeta("asset_finalize"),
	"POST /api/assets/{id}/promote":                               httpMutationMeta("asset_promote"),
	"POST /api/assets/{id}/retry":                                 httpMutationMeta("asset_retry"),
	"DELETE /api/assets/{id}":                                     httpMutationMeta("asset_delete"),
	"POST /api/auth/bootstrap/operator":                           httpMutationMeta("bootstrap_operator"),
	"POST /api/auth/password-reset/start":                         httpMutationMeta("password_reset_start"),
	"POST /api/auth/password-reset/question":                      httpMutationMeta("password_reset_question"),
	"POST /api/auth/password-reset/verify":                        httpMutationMeta("password_reset_verify"),
	"POST /api/auth/password-reset/complete":                      httpMutationMeta("password_reset_complete"),
	"POST /api/connect/pim/accounts":                              httpMutationMeta("pim_account_create"),
	"DELETE /api/connect/pim/accounts/{id}":                       httpMutationMeta("pim_account_delete"),
	"POST /api/connect/pim/accounts/{id}/auth/cancel":             httpMutationMeta("pim_auth_cancel"),
	"POST /api/connect/pim/accounts/{id}/auth/start":              httpMutationMeta("pim_auth_start"),
	"POST /api/connect/pim/accounts/{id}/logout":                  httpMutationMeta("pim_logout"),
	"POST /api/connect/whatsapp/logout":                           httpMutationMeta("whatsapp_logout"),
	"POST /api/conversations":                                     httpMutationMeta("conversation_create"),
	"POST /api/conversations/{id}/archive":                        httpMutationMeta("conversation_archive"),
	"POST /api/conversations/{id}/branches/{branchSeq}/select":    httpMutationMeta("conversation_branch_select"),
	"DELETE /api/conversations/{id}":                              httpMutationMeta("conversation_delete"),
	"POST /api/conversations/{id}/edit":                           httpMutationMeta("conversation_edit"),
	"POST /api/conversations/{id}/export-delete":                  httpMutationMeta("conversation_export_delete"),
	"POST /api/conversations/{id}/rename":                         httpMutationMeta("conversation_rename"),
	"POST /api/conversations/{id}/unarchive":                      httpMutationMeta("conversation_unarchive"),
	"POST /api/documents":                                         httpMutationMeta("document_create"),
	"PATCH /api/documents/{id}":                                   httpMutationMeta("document_patch"),
	"DELETE /api/documents/{id}":                                  httpMutationMeta("document_delete"),
	"POST /api/governance/mcp":                                    httpMutationMeta("mcp_install"),
	"PATCH /api/governance/mcp/{name}/env":                        httpMutationMeta("mcp_env_edit"),
	"POST /api/governance/mcp/{name}/trust":                       httpMutationMeta("mcp_trust"),
	"POST /api/governance/mcp/{name}/enable":                      httpMutationMeta("mcp_enable"),
	"POST /api/governance/mcp/{name}/disable":                     httpMutationMeta("mcp_disable"),
	"DELETE /api/governance/mcp/{name}":                           httpMutationMeta("mcp_remove"),
	"POST /api/governance/scheduler/{id}/approve":                 httpMutationMeta("scheduler_approve"),
	"POST /api/governance/scheduler/{id}/run":                     httpMutationMeta("scheduler_run"),
	"DELETE /api/governance/scheduler/{id}":                       httpMutationMeta("scheduler_cancel"),
	"PATCH /api/governance/scheduler/{id}":                        httpMutationMeta("scheduler_edit"),
	"POST /api/governance/skills":                                 httpMutationMeta("skill_create"),
	"POST /api/governance/skills/install":                         httpMutationMeta("skill_install"),
	"POST /api/governance/skills/{name}/archive":                  httpMutationMeta("skill_archive"),
	"POST /api/governance/skills/{name}/restore":                  httpMutationMeta("skill_restore"),
	"PATCH /api/governance/skills/{name}":                         httpMutationMeta("skill_update"),
	"DELETE /api/governance/skills/{name}":                        httpMutationMeta("skill_delete"),
	"POST /api/onboarding/profile/start":                          httpMutationMeta("onboarding_profile_start"),
	"POST /api/onboarding/start":                                  httpMutationMeta("onboarding_start"),
	"POST /api/onboarding/{sessionToken}/profile":                 httpMutationMeta("onboarding_profile"),
	"POST /api/onboarding/{sessionToken}/provision":               httpMutationMeta("onboarding_provision"),
	"POST /api/onboarding/{sessionToken}/step":                    httpMutationMeta("onboarding_step"),
	"POST /api/settings/telegram/link":                            httpMutationMeta("settings_telegram_link"),
	"PUT /api/settings/{key}":                                     httpMutationMeta("setting_put"),
	"DELETE /api/settings/{key}":                                  httpMutationMeta("setting_delete"),
	"POST /api/shares":                                            httpMutationMeta("share_create"),
	"PATCH /api/shares/{id}/snapshot":                             httpMutationMeta("share_snapshot"),
	"DELETE /api/shares/{id}":                                     httpMutationMeta("share_revoke"),
	"POST /api/storage/orphans/cleanup":                           httpMutationMeta("storage_cleanup"),
}

func httpMutationMeta(normalizer string) mutationRouteMeta {
	return mutationRouteMeta{Scope: idempotency.ScopeHTTPMutation, Normalize: normalizer, KeyPolicy: keyPolicyRequiredHeader}
}

func parseIdempotencyKey(r *http.Request) (string, error) {
	values, present := r.Header[http.CanonicalHeaderKey("Idempotency-Key")]
	if !present || len(values) != 1 {
		return "", errInvalidIdempotencyKey
	}
	key := values[0]
	if strings.TrimSpace(key) == "" || len(key) > idempotency.MaxOperationKeyBytes || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return "", errInvalidIdempotencyKey
	}
	return key, nil
}

// writeIdempotencyDecision handles every non-acquired registry decision. Its
// response bodies are stable, bounded JSON and never expose keys or fingerprints.
func writeIdempotencyDecision(w http.ResponseWriter, decision idempotency.BeginDecision) bool {
	switch decision.Decision {
	case idempotency.DecisionAcquired:
		return false
	case idempotency.DecisionReplay:
		if decision.Replay != nil {
			for name, value := range decision.Replay.Headers {
				w.Header().Set(name, value)
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.Header().Set("Idempotency-Replayed", "true")
		status := http.StatusOK
		if decision.Replay != nil && decision.Replay.StatusCode != 0 {
			status = decision.Replay.StatusCode
		}
		w.WriteHeader(status)
		if decision.Replay != nil && len(decision.Replay.Body) != 0 {
			_, _ = w.Write(decision.Replay.Body)
		}
	case idempotency.DecisionResultExpired:
		writeIdempotencyError(w, http.StatusGone, "operation completed but its replay result is no longer retained")
	case idempotency.DecisionConflict:
		writeIdempotencyError(w, http.StatusConflict, "operation key conflicts with a different request")
	case idempotency.DecisionInProgress:
		retry := decision.RetryAfter
		if retry <= 0 {
			retry = 1
		}
		if retry > idempotency.MaxRetryAfter {
			retry = idempotency.MaxRetryAfter
		}
		seconds := int(math.Ceil(retry.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeIdempotencyError(w, http.StatusConflict, "operation is still in progress")
	case idempotency.DecisionIndeterminate:
		writeIdempotencyError(w, http.StatusUnprocessableEntity, "operation outcome is indeterminate; do not retry automatically")
	default:
		writeIdempotencyError(w, http.StatusServiceUnavailable, "operation registry unavailable")
	}
	return true
}

func writeIdempotencyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func validateHTTPMutationInventory() error {
	for route, meta := range httpMutationRoutes {
		if meta.Scope != idempotency.ScopeHTTPMutation || meta.Normalize == "" || meta.KeyPolicy != keyPolicyRequiredHeader {
			return fmt.Errorf("mutation route %q has incomplete idempotency metadata", route)
		}
	}
	return nil
}
