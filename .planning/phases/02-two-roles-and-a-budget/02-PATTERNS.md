# Phase 2: Two Roles and a Budget - Pattern Map

**Mapped:** 2026-09-08
**Files analyzed:** 20 (new or modified, backend + frontend)
**Analogs found:** 20 / 20

Migration numbering (measured, not deduced): highest on disk is
`0120_worker_steer_scopes.{up,down}.sql`. **The next free slot is `0121`.** Per CLAUDE.md/C-04
this is only the number as of this read — re-run `ls internal/db/migrations/ | tail -1`
immediately before generating the migration file, do not trust this document at execution time.

All analogs below verified `git ls-files` tracked (not a `.gsd`/mirror path) before citation.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/db/migrations/0121_*.{up,down}.sql` (rewrite `*` grants, retire wildcard) | migration | batch | `internal/db/migrations/0026_*` (explicit `local` grants) — pattern only, not read this pass; cite by name from RESEARCH Q4 | role-match |
| `internal/db/migrations/012N_identity_llm_key.{up,down}.sql` (new table) | migration | CRUD | `internal/db/migrations/0100_identity_mcp_oauth.up.sql` | exact |
| `internal/identity/` — capability name constants (single declaration point) | config/model | CRUD | `internal/identity/store.go` (`ValidateCapabilityName`, `HasCapability`) | exact |
| `internal/identity/store.go` — `HasCapability` wildcard removal + fail-closed | service | request-response | same file, in place | exact |
| new admin-set-at-provisioning grant step | service | event-driven | `internal/agui/onboarding_provision.go` (existing provisioning saga) | exact |
| `internal/identitykey/store.go` (new package; encrypted OpenRouter key store) | service/model | CRUD | `internal/mcpoauth/store.go` + `store_integration_test.go` | exact |
| `internal/identitykey/store_integration_test.go` | test | CRUD | `internal/mcpoauth/store_integration_test.go` | exact |
| `internal/openrouterprovision/client.go` (new package; mint/patch/delete/read keys) | service | request-response | `internal/llm/spend.go` + `internal/llm/pricing_source.go` (`FetchModelPrice`) | exact |
| `internal/openrouterprovision/client_test.go` | test | request-response | `internal/llm/spend_test.go` (`keyServer` httptest helper) | exact |
| `internal/agui/onboarding_provision.go` split (568 LOC → gains key-mint leg) | service | event-driven | itself, refactor-on-touch: split into `onboarding_provision.go` + `onboarding_provision_credit.go` | exact (self) |
| `internal/agui/deprovision.go` — add `sagaStepOpenRouterKey` revoke leg | service | event-driven | same file's existing `sagaStepAuthula`/`sagaStepObjectStore` steps (`deprovision.go:148-238`) | exact |
| `internal/agui/deprovision_route.go` (new HTTP wrapper over the saga) | controller/route | request-response | `internal/agui/audit_api.go` (`registerAuditRoutes`, handler shape) | exact |
| `internal/agui/credit_api.go` (new; admin cap read/set + reset interval) | controller/route | CRUD | `internal/agui/audit_api.go` (`handleGrantCapability`/`handleRevokeCapability` mutation shape) | exact |
| `internal/agui/idempotency_http.go` — register the 2 new mutating routes | config | request-response | same file, `httpMutationRoutes` map, in place | exact |
| `cmd/aura/serve_webui_musr.go` — mount new routes | route wiring | request-response | same file, in place (mounts `registerAuditRoutes`) | exact |
| `cmd/aura/llm_client.go` — add `creditExhaustedClient` sentinel | service | request-response | same file's existing `llmNotConfiguredClient` | exact |
| `cmd/aura/llm_client_test.go` (new — file does not exist today) | test | request-response | pattern from `internal/llm/spend_test.go`'s error-shape assertions | role-match (no direct analog file) |
| `internal/runner/runner_llm_runtime.go` — per-identity snapshot resolver + cache | service | request-response | same file's existing `llmSnapshot`/`withLLMRuntimeSnapshot` | exact |
| `internal/swarm/swarm.go` — worker inherits parent snapshot (already does via `rc.Runtime.Snapshot()`, `swarm.go:288-291`) — verify/extend | service | event-driven | same file, in place | exact |
| `internal/cron/handlers/handler.go` — resolve snapshot from job's owning identity (headless, no ctx principal) | service | event-driven | same file's `newAgentWorker` (`handler.go:114-140`), same `deps.Runtime.Snapshot()` shape | exact |
| `scripts/check_capability_declaration.sh` (new; RBAC-02 static assertion) | test/tooling | batch | `scripts/check_ci_go_packages.sh` | role-match |
| `web/src/settings/CapabilityAdminPanel.tsx` → replaced by `web/src/settings/IdentityRoster.tsx` | component | request-response | itself (in-place rewrite) + `web/src/admin/AdminSection.tsx` chrome | exact |
| `web/src/settings/IdentityRoster.tsx` removal action + `ConfirmDialog` typed-confirm | component | request-response | `web/src/components/ui/confirm-dialog.tsx` | exact |
| `web/src/settings/CreditPanel.tsx` (new; per-identity cap/reset/gauge) | component | request-response | `web/src/chat/ContextBudgetGauge.tsx` (fill-bar shape) | exact |
| `web/src/settings/SpendOverview.tsx` (new; KPI tiles + sparklines + ranked list) | component | request-response | `web/src/chat/displays/ChartDisplay.tsx` (zero-dependency chart convention) | role-match |
| `web/src/admin/useAdmin.ts` — extend roster query shape (cap/remaining/spend/reset) | hook | request-response | same file, in place | exact |
| `web/src/admin/adminApi.ts` — new endpoints (credit get/set, remove identity, overview) | service | request-response | same file, in place (existing grant/revoke calls) | exact |
| `web/src/onboarding/OnboardingWizard.tsx` — 4 phases → 3 (drop `capabilities`) | component | request-response | same file, in place (`onboardingWizardModel.ts:6-8`) | exact |
| `web/src/onboarding/CapabilityPicker.tsx` | (deleted — dead code) | — | — | n/a — delete, do not migrate |
| `web/src/onboarding/ReviewStep.tsx` — replace capability badges with 2 static rows | component | request-response | same file, in place | exact |
| `web/src/chat/ExternalStoreChat_messages.tsx` — CRED-05 refusal copy | component | request-response | same file, in place (`MessagePrimitive.Error` slot, lines ~210-213) | exact |

## Pattern Assignments

### `internal/identitykey/store.go` (service/model, CRUD)

**Analog:** `internal/mcpoauth/store.go` (verified tracked)

**Imports pattern** (`store.go:16-35`):
```go
import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
)
```

**Domain separation constant** (`store.go:38-42`):
```go
// keyDerivationInfo domain-separates this store's wrapping key from every other key
// derived from the same AURA_AUTHULA_SECRET. Sharing an info string with
// internal/objectstore would mean one leaked key opens both stores...
const keyDerivationInfo = "aura-mcp-oauth-identity-key-v1"
```
For the new store: use a distinct info string, e.g. `"aura-identity-llm-key-v1"` — never reuse
`mcpoauth`'s string; that's the entire point of the domain-separation comment.

**Constructor / AEAD setup** (`store.go:95-112`):
```go
func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	if pool == nil {
		return nil, errors.New("mcpoauth: nil pool")
	}
	key, err := deriveKey(authulaSecretHex)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: gcm: %w", err)
	}
	return &Store{pool: pool, aead: aead}, nil
}
```

**Load pattern (RLS via `db.WithIdentityTx`, sentinel error on no-rows)** (`store.go:120-141`):
```go
func (s *Store) Load(ctx context.Context, serverName string) (Grant, error) {
	identity, err := requireIdentity(ctx)
	...
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		var qerr error
		row, qerr = q.GetIdentityMCPOAuth(ctx, sqlc.GetIdentityMCPOAuthParams{...})
		return qerr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, fmt.Errorf("%w: %q", ErrNoGrant, serverName)
	}
	...
}
```
New store needs an equivalent `ErrNoKey` sentinel, the same `db.WithIdentityTx` RLS-scoped
transaction, and `ON DELETE CASCADE` on `identity_id` (per D-12/RESEARCH's confirmed pattern).

**Listing selects no ciphertext** — RESEARCH explicitly calls out
`internal/db/sqlc/identity_mcp_oauth.sql.go:86`'s listing query selects no ciphertext. The new
store's admin-roster listing query (cap + hash only, never the encrypted key) must do the same —
copy that query shape when writing the new sqlc query, not the `Load` query shape.

---

### `internal/identitykey/store_integration_test.go` (test, CRUD)

**Analog:** `internal/mcpoauth/store_integration_test.go` — `db_integration` tag, RLS + cascade
assertions. Mirror its structure: provision two identities, assert RLS blocks cross-identity
reads, assert `DELETE identity` cascades the key row.

---

### `internal/openrouterprovision/client.go` (service, request-response)

**Analog:** `internal/llm/spend.go` (full file read, 102 lines)

**Signature convention — client/baseURL/key as parameters, never a package default**
(`spend.go:53`):
```go
func FetchSpend(ctx context.Context, client *http.Client, baseURL, apiKey string) (Spend, error) {
```
Every new Provisioning-API function (`MintKey`, `PatchKey`, `DeleteKey`, `GetKey`) must take the
same three parameters, per RESEARCH Q6 — this is what keeps the unit tier daemon-free.

**Sentinel-error convention** (`spend.go:16-21`):
```go
var ErrSpendUnavailable = errors.New("provider spend unavailable")
var ErrSpendNotApplicable = errors.New("provider spend not applicable to this backend")
```
D-13/CRED-09 says the new client should expose the *existing* `ErrSpendNotApplicable` for the
local-backend exemption (do not invent a second "not applicable" sentinel) — import
`internal/llm` and reuse it, do not redeclare it in the new package.

**Wire-shape doc comment convention** (`spend.go:32-40`) — document the verified field names and
cite the measurement date/line, exactly as `keyWire` does; RESEARCH's M-01..M-14 in CONTEXT.md
are the citation source for the new `keyWire`/`mintRequest`/`mintResponse` structs.

---

### `internal/openrouterprovision/client_test.go` (test, request-response)

**Analog:** `internal/llm/spend_test.go` (full file read, first 60 lines)

**httptest fixture convention** (`spend_test.go:14-33`):
```go
const keyPayload = `{"data":{"label":"sk-or-v1-0c3...ec6","is_management_key":false,
  "limit":null,"limit_remaining":null,"usage":2.722773972,...}}`

func keyServer(t *testing.T, seenAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/key" { http.NotFound(w, r); return }
		if seenAuth != nil { *seenAuth = r.Header.Get("Authorization") }
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(keyPayload))
	}))
	t.Cleanup(srv.Close)
	return srv
}
```
The new test file needs one such fixture PER verb (`POST /keys`, `PATCH /keys/{hash}`,
`DELETE /keys/{hash}`, `GET /keys`), matching the response shapes 02-OPENROUTER-API.md
documents (this is the Wave-0 gap RESEARCH flags explicitly: "A shared httptest fixture for the
OpenRouter Provisioning API ... does not exist yet").

**Assertion convention** — assert the outgoing `Authorization` header equals the expected key
(`spend_test.go:44-47`), and assert a zero/false value is returned as DATA not error
(`spend_test.go:51-54`, the "zero daily spend is DATA, not an absence" comment) — the same
discipline applies to a zero-cap key: `limit: 0` must decode as a real `0`, not an error.

---

### `internal/agui/deprovision.go` — new saga step (service, event-driven)

**Analog:** the file's own existing steps, same file (`deprovision.go:148-238` per RESEARCH,
header read `deprovision.go:1-60`)

The saga's documented contract (`deprovision.go:9-24`) — soft-delete two-phase, `Deactivate`
immediate, `Purge` tears down every plane in reverse order, each step idempotent (`Delete`/`Deny`
by-id 404=success, `RemoveAll`, FK-cascade). A new `sagaStepOpenRouterKey` (revoke the identity's
key, verify via `GET /keys/{hash}` → 404 per CRED-08/M-10) must follow the exact same idempotency
contract as `sagaStepAuthula`/`sagaStepObjectStore`: a consumer-side port interface, nil-port
skips the plane, `Delete`-then-verify-404 counts as success on a repeat call.

**Port interface convention** (`deprovision.go:37-49`):
```go
type IdentityDeactivator interface {
	MarkDeactivated(ctx context.Context, identityID string, purgeAfter time.Time) error
	ListPurgeable(ctx context.Context, now time.Time) ([]DeprovisionTarget, error)
	ResolveTarget(ctx context.Context, identityID string) (DeprovisionTarget, error)
}
```
Declare a symmetric `OpenRouterKeyRevoker` interface (`RevokeKey(ctx, identityID string) error`),
consumer-side, so `deprovision_test.go` can inject a fake with no live provider — mirrors the
existing `SessionTerminator`/`JobTerminator` shape immediately below it in the same file.

---

### `internal/agui/deprovision_route.go` (new, controller, request-response)

**Analog:** `internal/agui/audit_api.go` (full header + `registerAuditRoutes` read)

**Route registration pattern** (`audit_api.go:76-82`):
```go
func (s *Server) registerAuditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("GET /api/admin/identities", s.handleAdminIdentities)
	mux.HandleFunc("POST /api/admin/identities/{id}/capabilities", s.handleGrantCapability)
	mux.HandleFunc("DELETE /api/admin/identities/{id}/capabilities/{capability}", s.handleRevokeCapability)
	mux.HandleFunc("GET /api/admin/audit", s.handleAdminAudit)
}
```
New file adds a sibling `registerIdentityRemovalRoutes(mux)` with
`DELETE /api/admin/identities/{id}` — mounted from `cmd/aura/serve_webui_musr.go` alongside the
existing `registerAuditRoutes` call.

**Consumer-side seam / Set*-after-construct wiring convention** (`audit_api.go:35-45,58-64`):
```go
type identityAdmin interface {
	ListIdentities(ctx context.Context) ([]identity.Identity, error)
	...
}
func (s *Server) SetIdentityAdmin(a identityAdmin) { s.idAdmin = a }
```
The removal route needs its own narrow port (`identityRemover` — one method,
`Deprovision(ctx, identityID string) error`, backed by the `deprovision.go` saga) wired the same
way: declared consumer-side in the new file, satisfied by a composition-root adapter, 503 until
`Set*` is called (matches the existing `SetAuditStore`/`SetIdentityAdmin` precedent exactly).

**Gating note:** the doc comment at `audit_api.go:18-19` states the four `/api/admin/*` routes
are gated server-side by `RequireCapability(governance.write)` at the parent-mux mount
(`cmd/aura/serve_webui_musr.go`) — **but D-01/D-02 requires `identity.delete` for removal, a
DIFFERENT capability than `governance.write`.** The new removal route must NOT simply reuse the
existing mux-level gate; it needs its own `RequireCapability(identity.delete)` wrapper, and this
is worth flagging to the planner explicitly since the closest analog's gate is the wrong
capability to copy verbatim.

---

### `internal/agui/idempotency_http.go` — register new mutating routes (config)

**Analog:** same file, in place (full excerpt read, lines 1-90+)

**Registration convention** (`idempotency_http.go:52-62`):
```go
var httpMutationRoutes = map[string]mutationRouteMeta{
	"POST /agent/run": httpMutationMeta("agent_run"),
	...
	"POST /api/admin/identities/{id}/capabilities":                httpMutationMeta("capability_grant"),
	"DELETE /api/admin/identities/{id}/capabilities/{capability}": httpMutationMeta("capability_revoke"),
	...
}
```
Add two entries: `"DELETE /api/admin/identities/{id}": httpMutationMeta("identity_remove")` and
`"POST /api/admin/identities/{id}/credit": httpMutationMeta("identity_credit_set")` (or whatever
exact path the planner assigns the credit-cap route) — RESEARCH Q5 explicitly calls out that
"any new mutating admin route must be registered there too."

---

### `cmd/aura/llm_client.go` — `creditExhaustedClient` sentinel (service, request-response)

**Analog:** same file's `llmNotConfiguredClient`, full file read (65 lines)

**Sentinel shape to copy verbatim** (`llm_client.go:23-63`):
```go
const (
	llmNotConfiguredCode = "llm_not_configured"
	llmNotConfiguredHint = "set OPENROUTER_API_KEY in .env or the environment, then retry"
)

type llmNotConfiguredClient struct{}
type llmNotConfiguredError struct{}

func newLLMClient(cfg llm.Config) llm.Client {
	if strings.TrimSpace(cfg.APIKey) == "" && !allowsKeylessLLMBaseURL(cfg.BaseURL) {
		return llmNotConfiguredClient{}
	}
	return openai_compat.New(cfg)
}

func (llmNotConfiguredClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, llmNotConfiguredError{}
}

func (llmNotConfiguredError) Error() string {
	payload := struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}{Error: llmNotConfiguredCode, Hint: llmNotConfiguredHint}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"error":"llm_not_configured","hint":"..."}`
	}
	return string(data)
}
```
`creditExhaustedClient` copies this exactly: `creditExhaustedCode = "credit_exhausted"`, a hint
matching the UI-SPEC copy ("Ask an administrator to add credit under Settings → Identities"),
same `Stream` short-circuit before any network call. Construction seam: the per-identity
resolver (seam A, `runner_llm_runtime.go`) is where the zero-cap check happens and where this
sentinel gets returned instead of `openai_compat.New(cfg)`.

**Test file — Wave 0 gap:** `cmd/aura/llm_client_test.go` does not exist. Create it new,
following the `Stream()` error-shape assertion pattern from `spend_test.go` (JSON-decode the
error string, assert `error`/`hint` fields) since there's no existing Go test file in `cmd/aura`
for this sentinel pattern to extend.

---

### `internal/runner/runner_llm_runtime.go` — per-identity snapshot resolver (service, request-response)

**Analog:** same file, in place (full file read, 32 lines)

**Existing seam to extend, not replace** (`runner_llm_runtime.go:9-23`):
```go
type llmRuntimeSnapshotContextKey struct{}

func withLLMRuntimeSnapshot(ctx context.Context, snapshot llm.RuntimeSnapshot) context.Context {
	return context.WithValue(ctx, llmRuntimeSnapshotContextKey{}, snapshot)
}

func (r *Runner) llmSnapshot(ctx context.Context) llm.RuntimeSnapshot {
	if snapshot, ok := ctx.Value(llmRuntimeSnapshotContextKey{}).(llm.RuntimeSnapshot); ok {
		return snapshot
	}
	if r == nil || r.runtime == nil {
		return llm.RuntimeSnapshot{}
	}
	return r.runtime.Snapshot()
}
```
Per RESEARCH's "Seam A" recommendation: the new work is a resolver that builds a
per-identity `llm.RuntimeSnapshot` (one `openai_compat.Client` per identity, cached — key bound
at `Client` construction, per `client.go:37-66`'s `option.WithAPIKey(cfg.APIKey)`) and calls
`withLLMRuntimeSnapshot(ctx, snapshot)` before `runner.go:214`'s existing call site. **Nothing
below `llmSnapshot` changes** — this is the whole finding RESEARCH's opening section states.

---

### `internal/cron/handlers/handler.go` — resolve from job's owning identity (headless path)

**Analog:** same file's `newAgentWorker`, lines 114-140 (already read)

**Existing runtime-resolution shape to extend** (`handler.go:123-127`):
```go
client, cfg := deps.Client, deps.LLM
if deps.Runtime != nil {
	runtime := deps.Runtime.Snapshot()
	client, cfg = runtime.Client, runtime.Config
}
```
This is the one path RESEARCH flags as NOT covered by seam A unchanged — there is no HTTP
principal on a cron job's context. The resolver needs a variant that takes the job's owning
identity id directly (not from ctx) and produces the same `client, cfg` pair before
`newAgentWorker`'s `agent.NewLlmAgent` call — same output shape, different input source.

---

### `internal/swarm/swarm.go` — worker snapshot inheritance (already correct shape, verify)

**Analog:** same file, lines 280-300 (already read)

```go
client, cfg := rc.Client, rc.LLM
if rc.Runtime != nil {
	runtime := rc.Runtime.Snapshot()
	client, cfg = runtime.Client, runtime.Config
}
```
Identical shape to the cron handler above. Per RESEARCH: "A worker runs on behalf of the
parent turn's identity and must inherit the parent's snapshot" — this code already inherits
`rc.Runtime.Snapshot()`, so as long as `rc.Runtime` is populated with the per-identity snapshot
by the caller (seam A upstream), this file likely needs NO change — confirm during planning
rather than assuming a rewrite is needed here.

---

### Frontend: `web/src/settings/IdentityRoster.tsx` (replaces `CapabilityAdminPanel.tsx`)

**Analog:** `web/src/settings/CapabilityAdminPanel.tsx` (160 LOC, in-place rewrite target) +
`web/src/admin/AdminSection.tsx` (65 LOC, reused chrome, unchanged)

Reuse `AdminSection`'s loading/error/empty guard verbatim (per UI-SPEC §Component Inventory:
"do not rebuild this chrome"). Reuse `CapabilityAdminPanel.tsx`'s existing conventions cited by
UI-SPEC: `break-all font-mono` for identity name/email (`CapabilityAdminPanel.tsx:75` area, the
`" (you)"` suffix pattern), `role="list"` of `Card` rows.

---

### Frontend: `web/src/settings/CreditPanel.tsx` (new)

**Analog:** `web/src/chat/ContextBudgetGauge.tsx` (full file read, first 60 lines) — verified
path is `web/src/chat/ContextBudgetGauge.tsx`, **not** `web/src/settings/` as the task prompt's
citation implied; corrected here.

**Fill-bar shape to copy exactly** (`ContextBudgetGauge.tsx:1-60`):
```tsx
import {
  CONTEXT_CRITICAL_PERCENT,
  CONTEXT_NEAR_FULL_PERCENT,
  contextPercent,
  formatTokens,
  gaugeTier,
  ...
} from './footerMetrics';

export function ContextBudgetGauge({ usedTokens, windowTokens, conversationId }: ContextBudgetGaugeProps) {
  const percent = contextPercent(usedTokens, windowTokens);
  const tier = gaugeTier(percent);
  const fillClass =
    tier === 'critical' ? 'bg-danger' : tier === 'near' ? 'bg-warning' : 'bg-accent';

  return (
    <div className="flex min-w-[10rem] flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[0.75rem] font-medium uppercase tracking-wider text-text-faint">
          {t('footer.context')}
        </span>
        <span className="font-mono text-xs text-text">
          {t('footer.gaugeValue', { used: formatTokens(usedTokens), window: formatTokens(windowTokens), percent })}
        </span>
      </div>
      {/* fill bar div with role="progressbar", aria-valuenow, fillClass width */}
    </div>
  );
}
```
Per UI-SPEC §Credit panel item 3: reuse this exact three-tier structure (`CONTEXT_NEAR_FULL_PERCENT`
= 70, `CONTEXT_CRITICAL_PERCENT` = 90 renamed/reused for the credit gauge by the same semantics,
not new thresholds), the `role="progressbar"` + `aria-valuenow` pair, and the
`"{{spend}} / {{cap}} · {{percent}}%"` readout format mirroring `footer.gaugeValue`'s i18n key
shape exactly (new key, same interpolation structure).

---

### Frontend: `web/src/settings/SpendOverview.tsx` (new)

**Analog (convention, not reused code):** `web/src/chat/displays/ChartDisplay.tsx` — cited by
UI-SPEC as "zero-dependency bar chart, CSS-width bars, no charting library" with an explicit
`// no charting library — never recharts` comment. The KPI sparklines must be inline SVG
`<polyline>` elements, following this same zero-dependency convention — read the file's actual
CSS-width-bar technique before implementing the sparkline, do not introduce any chart package.

---

### Frontend: `web/src/components/ui/confirm-dialog.tsx` (base for removal dialog)

**Analog:** the file itself (89 LOC), extended not replaced — takes `children`, so the typed
"Type {{email}} to confirm" input (UI-SPEC §Copywriting Contract, §Roster) is passed as a child,
not a new dialog component. Read this file in full during planning/execution before wiring the
removal flow — not re-read here to avoid duplicate-range cost, but flagged as a mandatory read
for the executor since the whole removal-dialog contract depends on its exact prop shape.

---

## Shared Patterns

### Encrypted per-identity credential storage
**Source:** `internal/mcpoauth/store.go` (AES-256-GCM + HKDF-derived KEK from
`AURA_AUTHULA_SECRET` + RLS via `db.WithIdentityTx` + `ON DELETE CASCADE`)
**Apply to:** `internal/identitykey/store.go` (new package). Domain-separate the HKDF info
string; never reuse `mcpoauth`'s `"aura-mcp-oauth-identity-key-v1"`.

### httptest-first client testing (no daemon, no network)
**Source:** `internal/llm/spend.go` + `spend_test.go`'s `keyServer` helper
**Apply to:** `internal/openrouterprovision/client.go` and its test file — every Provisioning-API
call takes `*http.Client, baseURL, apiKey string` as explicit parameters.

### Client-level refusal sentinel (fail before any network call)
**Source:** `cmd/aura/llm_client.go`'s `llmNotConfiguredClient`
**Apply to:** the new `creditExhaustedClient` — same shape, same seam, machine-readable
`{"error":..., "hint":...}` payload.

### RuntimeSnapshot-carries-client, resolved once per turn
**Source:** `internal/runner/runner_llm_runtime.go` + `internal/llm/runtime.go`
**Apply to:** the per-identity resolver (seam A) — every downstream caller (`runner.go`,
`swarm.go`, `internal/cron/handlers/handler.go`) already reads through one of two functions
(`llmSnapshot`/`trackerLLMSnapshot`) or the `rc.Runtime.Snapshot()` / `deps.Runtime.Snapshot()`
pattern; put the per-identity resolution above those call sites, do not touch the hot path below.

### Admin mutation route registration (idempotency)
**Source:** `internal/agui/idempotency_http.go`'s `httpMutationRoutes` map
**Apply to:** every new mutating admin route (`DELETE /api/admin/identities/{id}`, the credit-cap
set route) — must add an entry here or the mutation is unprotected against replay, per RESEARCH
Q5's explicit callout.

### Consumer-side port + `Set*`-after-construct wiring, nil-port-skips-plane
**Source:** `internal/agui/deprovision.go`'s `IdentityDeactivator`/`SessionTerminator`/
`JobTerminator` interfaces and `internal/agui/audit_api.go`'s `SetAuditStore`/`SetIdentityAdmin`
**Apply to:** the new `OpenRouterKeyRevoker` saga port and the new removal route's `identityRemover`
port — declare consumer-side (in the file that calls the method), wire from the composition root
after `NewServer`, 503 until wired.

### AdminSection chrome reuse (loading/error/empty)
**Source:** `web/src/admin/AdminSection.tsx`
**Apply to:** `IdentityRoster.tsx`, `CreditPanel.tsx`, `SpendOverview.tsx` — do not rebuild this
chrome; all three new/rewritten panels mount inside it per UI-SPEC §Component Inventory.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `scripts/check_capability_declaration.sh` | test/tooling | batch | No prior grep-based CI shell assertion exists for "capability name literals appear in exactly one package" — `scripts/check_ci_go_packages.sh` is the closest structural shape (a shell script run in CI asserting an invariant over the source tree) but its actual assertion logic is unrelated and must be written fresh. |
| `web/src/settings/SpendOverview.tsx`'s KPI-delta-vs-prior-period diffing logic | transform | batch | No existing frontend code computes a second time-windowed API call and diffs it client-side; `ChartDisplay.tsx` only covers the zero-dependency rendering convention, not this data-fetching shape. |
| `internal/openrouterprovision` package as a whole (new package, not an extension of an existing one) | service | request-response | RESEARCH explicitly flags `internal/llm/pricing_source.go` (426 LOC) as "the file to watch if the provisioning client is put there rather than in its own package" — recommend a new package per the 600-LOC ceiling discipline; there is no existing package this cleanly extends. |

## Metadata

**Analog search scope:** `internal/mcpoauth/`, `internal/llm/`, `internal/agui/`, `internal/identity/`,
`internal/runner/`, `internal/swarm/`, `internal/cron/handlers/`, `internal/agent/`, `cmd/aura/`,
`web/src/settings/`, `web/src/admin/`, `web/src/onboarding/`, `web/src/chat/`,
`web/src/components/ui/`.
**Files scanned (read in full or targeted range):** `internal/mcpoauth/store.go` (140 lines),
`internal/llm/spend.go` (60 lines) + `spend_test.go` (60 lines), `internal/identity/store.go`
(targeted, ~120-160), `internal/agui/audit_api.go` (90 lines), `internal/agui/deprovision.go`
(60 lines), `internal/agui/idempotency_http.go` (80 lines), `cmd/aura/llm_client.go` (65 lines,
full), `internal/runner/runner_llm_runtime.go` (32 lines, full), `internal/swarm/swarm.go`
(targeted 280-300), `internal/cron/handlers/handler.go` (targeted 110-140),
`internal/agent/llm_agent_construct.go` (targeted 1-60), `web/src/chat/ContextBudgetGauge.tsx`
(targeted 1-60), plus LOC counts for `CapabilityAdminPanel.tsx`, `AdminSection.tsx`,
`useAdmin.ts`, `confirm-dialog.tsx`, `ReviewStep.tsx`, `ChartDisplay.tsx`.
**Migration directory tail (measured):** `0120_worker_steer_scopes.{up,down}.sql` — next slot
`0121`, re-verify at execution time per CLAUDE.md C-04.
**Pattern extraction date:** 2026-09-08.
