package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

var errInvalidIdempotencyKey = errors.New("invalid Idempotency-Key")

const httpOperationReplayRetention = 30 * 24 * time.Hour

type operationRegistry interface {
	Begin(context.Context, idempotency.BeginRequest) (idempotency.BeginDecision, error)
	Complete(context.Context, idempotency.CompleteRequest) error
	MarkIndeterminate(context.Context, idempotency.OperationKey, [32]byte) error
}

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
	// The sibling GET /agent/runs/{runID}/events resume route is deliberately NOT
	// inventoried: it is read-only by construction (zero write seams — no
	// SubmitAnswers, no lock, no turn start; server_run_resume.go), the same
	// method-based exclusion rationale as the graph-query/TTS/STT reads.
	"POST /agent/runs/{runID}/cancel":                             httpMutationMeta("agent_run_cancel"),
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

type httpMutationIntent struct {
	Normalizer  string          `json:"normalizer"`
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	Query       string          `json:"query,omitempty"`
	ContentType string          `json:"content_type,omitempty"`
	Body        json.RawMessage `json:"body,omitempty"`
}

func normalizeHTTPMutation(r *http.Request, meta mutationRouteMeta) (httpMutationIntent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRunBodyBytes+1))
	if err != nil {
		return httpMutationIntent{}, errors.New("read mutation body")
	}
	if len(body) > maxRunBodyBytes {
		return httpMutationIntent{}, errors.New("mutation body too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) != 0 && !json.Valid(trimmed) {
		return httpMutationIntent{}, errors.New("mutation body is not valid JSON")
	}
	return httpMutationIntent{
		Normalizer: meta.Normalize, Method: r.Method, Path: r.URL.EscapedPath(),
		Query: r.URL.Query().Encode(), ContentType: r.Header.Get("Content-Type"),
		Body: append(json.RawMessage(nil), trimmed...),
	}, nil
}

// idempotencyMutation owns the HTTP parent operation before the real handler is
// invoked. JSON responses are held until their replay envelope is durable. The
// streaming agent route cannot be replayed as bounded JSON, so its parent is
// terminally indeterminate after streaming; mutating tools derive replayable
// child operations in the agent dispatch path.
func (s *Server) idempotencyMutation(next http.Handler, meta mutationRouteMeta) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, err := parseIdempotencyKey(r)
		if err != nil {
			writeIdempotencyError(w, http.StatusBadRequest, "a single valid Idempotency-Key header is required")
			return
		}
		intent, err := normalizeHTTPMutation(r, meta)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "too large") {
				status = http.StatusRequestEntityTooLarge
			}
			writeIdempotencyError(w, status, "invalid mutation request body")
			return
		}
		fingerprint, err := idempotency.FingerprintTyped(intent)
		if err != nil {
			writeIdempotencyError(w, http.StatusBadRequest, "invalid mutation request body")
			return
		}
		identityID := identityctx.IdentityID(r.Context())
		if identityID == "" {
			// Public bootstrap/recovery mutations have no authenticated principal.
			// Give them the seeded local owner instead of accepting an unowned key.
			identityID = identityctx.LocalOperatorIdentity
		}
		operation := idempotency.Operation{
			Key:         idempotency.OperationKey{IdentityID: identityID, Scope: meta.Scope, Key: key},
			Fingerprint: fingerprint,
		}
		decision, beginErr := s.operations.Begin(r.Context(), idempotency.BeginRequest{
			Operation: operation.Key, Fingerprint: operation.Fingerprint,
		})
		if beginErr != nil && !errors.Is(beginErr, idempotency.ErrConflict) {
			writeIdempotencyError(w, http.StatusServiceUnavailable, "operation registry unavailable")
			return
		}
		if writeIdempotencyDecision(w, decision) {
			return
		}
		ctx := r.Context()
		if identityctx.IdentityID(ctx) == "" {
			ctx = identityctx.WithIdentityID(ctx, identityID)
		}
		ctx, err = idempotency.WithOperation(ctx, operation)
		if err != nil {
			_ = s.operations.MarkIndeterminate(r.Context(), operation.Key, operation.Fingerprint)
			writeIdempotencyError(w, http.StatusServiceUnavailable, "operation context unavailable")
			return
		}
		r = r.WithContext(ctx)

		if meta.Normalize == "agent_run" {
			defer func() {
				if recovered := recover(); recovered != nil {
					_ = s.operations.MarkIndeterminate(r.Context(), operation.Key, operation.Fingerprint)
					panic(recovered)
				}
			}()
			next.ServeHTTP(w, r)
			if err := s.operations.MarkIndeterminate(r.Context(), operation.Key, operation.Fingerprint); err != nil {
				slog.Error("agui: mark streaming mutation indeterminate", "err", err)
			}
			return
		}

		captured := newBoundedMutationResponse()
		defer func() {
			if recovered := recover(); recovered != nil {
				_ = s.operations.MarkIndeterminate(r.Context(), operation.Key, operation.Fingerprint)
				panic(recovered)
			}
		}()
		next.ServeHTTP(captured, r)
		if captured.overflow || (captured.body.Len() != 0 && !json.Valid(captured.body.Bytes())) {
			_ = s.operations.MarkIndeterminate(r.Context(), operation.Key, operation.Fingerprint)
			if captured.overflow {
				writeIdempotencyError(w, http.StatusInternalServerError, "mutation response exceeded replay limit")
				return
			}
			captured.copyTo(w)
			return
		}
		result := idempotency.ReplayResult{
			Body: append(json.RawMessage(nil), captured.body.Bytes()...), StatusCode: captured.status,
			Headers: replayableHeaders(captured.header), ExpiresAt: time.Now().UTC().Add(httpOperationReplayRetention),
		}
		if err := s.operations.Complete(r.Context(), idempotency.CompleteRequest{
			Operation: operation.Key, Fingerprint: operation.Fingerprint, Result: result,
		}); err != nil {
			_ = s.operations.MarkIndeterminate(r.Context(), operation.Key, operation.Fingerprint)
			writeIdempotencyError(w, http.StatusServiceUnavailable, "operation completion unavailable")
			return
		}
		captured.copyTo(w)
	})
}

type boundedMutationResponse struct {
	header      http.Header
	status      int
	body        bytes.Buffer
	overflow    bool
	wroteHeader bool
}

func newBoundedMutationResponse() *boundedMutationResponse {
	return &boundedMutationResponse{header: make(http.Header), status: http.StatusOK}
}

func (w *boundedMutationResponse) Header() http.Header { return w.header }

func (w *boundedMutationResponse) WriteHeader(status int) {
	if w.wroteHeader || status < 100 {
		return
	}
	w.status = status
	w.wroteHeader = true
}

func (w *boundedMutationResponse) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	if w.body.Len()+len(p) > idempotency.MaxReplayBodyBytes {
		w.overflow = true
		return len(p), nil
	}
	return w.body.Write(p)
}

func (w *boundedMutationResponse) Flush() {}

func (w *boundedMutationResponse) copyTo(dst http.ResponseWriter) {
	for name, values := range w.header {
		for _, value := range values {
			dst.Header().Add(name, value)
		}
	}
	dst.WriteHeader(w.status)
	if w.body.Len() != 0 {
		_, _ = dst.Write(w.body.Bytes())
	}
}

func replayableHeaders(header http.Header) map[string]string {
	result := make(map[string]string)
	for name, values := range header {
		canonical := http.CanonicalHeaderKey(name)
		if idempotency.ReplayHeaderAllowed(canonical) && len(values) != 0 {
			result[canonical] = values[0]
		}
	}
	return result
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
