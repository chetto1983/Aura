# Phase 37F: Conversation & Artifact Sharing / Export - Pattern Map

**Mapped:** 2026-07-15
**Files analyzed:** 33 (17 net-new, 10 modified, 6 test files with a named precedent)
**Analogs found:** 31 / 33 (exact: 16, role-match: 10, partial: 5, none: 2)
**Verified at:** HEAD `1a3252e64`, every excerpt below opened on disk

---

## How to read this

`37F-RESEARCH.md` §Code Seams already lists **where** each seam lives. This document answers the
different question: **for each file 37F writes, which existing file is the template, and what
exactly gets copied.** Every excerpt is verbatim from a file opened at HEAD — cite the `file:line`
in the plan task so the executor can diff against it.

**Two facts re-verified at map time (both hold):**
- `ls internal/db/migrations/ | tail -1` → `0039_compaction_rollout.*`. **0040 is free.** R-04 confirmed.
- `internal/share/` does not exist. Net-new confirmed.

**Three LOC margins re-measured at map time (all unchanged from RESEARCH):**
`cmd/aura/serve_webui.go` **593**, `web/src/AppShell.tsx` **591**, `web/src/i18n/resources.ts` **576**.

---

## Two findings that change the plan shape

### F-1 — The export route needs ZERO parent-mux work (RESEARCH did not surface this)

`cmd/aura/serve_webui.go:381` already mounts the whole conversations subtree:

```go
mux.Handle(conversationsRoutePrefix, aguiHandler)   // conversationsRoutePrefix = "/api/conversations/"
mux.Handle(conversationsListRoute, aguiHandler)     // conversationsListRoute   = "/api/conversations"
```

`GET /api/conversations/{id}/export` falls **inside** that prefix. It therefore inherits `RequireAuth`
automatically and needs **no** const, **no** mount, and **no** `serve_webui.go` edit — only a
`mux.HandleFunc` line in `registerConversationRoutes` (`internal/agui/conversations_api.go:48`).
**`/api/shares` is a different story** — it is a *new* sibling subtree and does need an explicit
parent-mux mount (see `serve_webui_share.go` below).

### F-2 — `SanitizeString` is an ANTI-analog for `redact.go`

`internal/agui/server_redact.go:52` is a **denylist** regex scrubber. RESEARCH's redaction rule #2
mandates **allowlist, never denylist**. Do **not** model `internal/share/redact.go` on it. The
correct analog is the client-side allowlist at `sseAdapter.ts:353-361`, ported to Go (see below).
`SanitizeString` remains correct in exactly one 37F role: the audit union already applies it to
`Target`/`Detail` (`audit_api.go:251-254`), so `share_audit` rows inherit it for free.

---

## File Classification

### Backend — Go

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `internal/share/snapshot.go` **(new)** | model/projection | transform | `web/src/chat/sseAdapter.ts:346-363` (allowlist projection) + `internal/agui/audit_store.go:27-38` (projection struct + doc) | partial |
| `internal/share/redact.go` **(new)** | utility | transform | `web/src/chat/sseAdapter.ts:353-361` — **anti-analog:** `server_redact.go:52` | partial |
| `internal/share/markdown.go` **(new)** | utility | transform | — none | none |
| `internal/share/jsonfmt.go` **(new)** | utility | transform | `internal/agui/conversations_api.go:149` `writeJSON` | partial |
| `internal/share/token.go` **(new)** | utility | transform | `internal/agui/password_reset.go:407-425` (`newRecoveryCode` + `hashRequestValue`) | **exact** |
| `internal/share/store.go` **(new)** | store | CRUD | `internal/agui/audit_store.go` (raw-pgx store) + `internal/conversations/store_identity.go:28` (owner gate) | **exact** |
| `internal/share/audit.go` **(new)** | store | event-append | `internal/agui/audit_store.go:40-46` + `0010 skill_audit` column shape | role-match |
| `internal/share/service.go` **(new)** | service | CRUD | `internal/runner/runner_delete.go:38-74` (owner-gate-first ordered lifecycle) | role-match |
| `internal/share/expiry.go` **(new)** | service seam | batch | `internal/cron/handlers/identity_purge.go:25-27` (`IdentityPurger`) | **exact** |
| `internal/agui/share_api.go` **(new)** | controller | request-response | `internal/agui/assets_api.go:13-59` | **exact** |
| `internal/agui/share_service.go` **(new)** | interface | — | `internal/agui/asset_service.go:10-27` | **exact** |
| `internal/cron/handlers/share_expiry.go` **(new)** | handler | batch | `internal/cron/handlers/identity_purge.go` (whole file) | **exact** |
| `cmd/aura/serve_webui_share.go` **(new)** | config/route mount | request-response | `cmd/aura/serve_webui_musr.go` (whole file) | **exact** |
| `internal/db/migrations/0040_*.up.sql` **(new)** | migration | — | `0035_assets_source_kind_agent.up.sql` (header) + `0034_*.up.sql` (kind widen) | **exact** |
| `internal/db/migrations/0040_*.down.sql` **(new)** | migration | — | `0034_scheduler_sandbox_reap_kind.down.sql` (delete-rows-then-narrow) | **exact** |
| `cmd/aura/serve_webui.go` **(mod, ~4 LOC)** | config | — | its own `:501/:506/:511` + `:523-534` | **exact** |
| `internal/agui/audit_store.go` **(mod, +4 SQL)** | store | CRUD | its own `:58-62` skill leg | **exact** |
| `internal/agui/audit_api.go` **(mod, +1 doc)** | controller | — | its own `:32-38` | **exact** |
| `internal/agui/server.go` **(mod, +2)** | config | — | its own `:108` + `:262` | **exact** |
| `internal/objectstore/types.go` **(mod, +3 funcs)** | utility | transform | its own `:60-62` `AssetKey` | **exact** |
| `internal/runner/runner_delete.go` **(mod, +step 4.5)** | service | event-driven | its own `:51-73` numbered steps | **exact** |
| `internal/conversations/conversations_api.go` **(mod, +1 route)** | controller | request-response | its own `:48-62` | **exact** |

### Frontend — TypeScript / React

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `web/src/shell/ShareShell.tsx` **(new)** | component | — | `web/src/shell/ArtifactsShell.tsx:23-45` | **exact** |
| `web/src/shell/useSharePanel.ts` **(new)** | hook | — | `web/src/shell/useArtifactsPanel.ts` | role-match |
| `web/src/chat/share/ShareModal.tsx` **(new)** | component | request-response | `web/src/documents/DocumentUploadDialog.tsx` (whole file) | **exact** |
| `web/src/chat/share/RevokeConfirmDialog.tsx` **(new)** | component | — | `web/src/conversations/DeleteConfirmDialog.tsx` (whole file) | **exact** |
| `web/src/routes/SharePage.tsx` **(new)** | component/route | request-response | `web/src/routes/LoginPage.tsx` + `main.tsx:14-19,40-45` | role-match |
| `web/src/chat/artifacts/renderers/assetSourceContext.ts` **(new, R-05)** | provider | — | `web/src/chat/voice/voiceModeContext.ts` (whole file) | **exact** |
| `web/src/i18n/resources.share.ts` **(new)** | config | — | `web/src/i18n/resources.compaction.ts` (whole file) | **exact** |
| `web/src/AppShell.tsx` **(mod, +4 LOC)** | component | — | its own `:514-517` | **exact** |
| `web/src/i18n/resources.ts` **(mod, +4 LOC)** | config | — | its own `:9` import + `:160`/`:437` spread | **exact** |
| `web/src/main.tsx` **(mod, +2 LOC)** | config/route | — | its own `:17-19` + `:44` | **exact** |
| `web/src/chat/artifacts/ArtifactsPanel.tsx` **(mod)** | component | — | its own `:111-129` list + `:147-160` EmptyState | **exact** |
| `web/src/chat/artifacts/renderers/useAssetContent.ts` **(mod, R-05)** | hook | file-I/O | its own `:33-36` | **exact** |

---

## Pattern Assignments

### `internal/share/token.go` (utility, transform) — analog `internal/agui/password_reset.go`

**This is the strongest analog in the phase: mint-plaintext-once + store-hash is already shipped, twice.**
Do not invent a token scheme. `password_reset.go:407-425`:

```go
func newRecoveryCode() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func hashRequestValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil && strings.TrimSpace(host) != "" {
		value = strings.TrimSpace(host)
	}
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
```

**Copy:** `[32]byte` + `rand.Read` + `base64.RawURLEncoding` verbatim — that is exactly D-13's
256-bit URL-safe opaque token. **Change one thing:** OQ5 stores `token_hash bytea` (raw 32 bytes),
so `share.Hash` returns `[32]byte`/`[]byte` from `sha256.Sum256`, **not** the base64 string
`hashRequestValue` returns. The trim/SplitHostPort preamble is IP-specific — drop it.

**Also copy the rand-failure doc discipline** from `onboarding_session.go:92-95`:

```go
// newSessionToken mints an opaque, unguessable session token (256 bits of crypto/rand,
// hex-encoded). A rand failure is surfaced so the caller fails the start request rather
// than minting a weak token.
func newSessionToken() (string, error) {
	var b [sessionTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
```

A `rand.Read` error **returns**, never falls back to a weaker source. Keep that. (Note the two
precedents disagree on encoding — hex vs base64url. **Take `password_reset.go`'s base64url:** it is
the shorter URL-bearing token, which is 37F's case.)

---

### `internal/share/redact.go` + `snapshot.go` (utility/model, transform) — analog `sseAdapter.ts:353-361`

**There is no Go analog. The only allowlist projection in the repo is in TypeScript** — and it is
the *exact* semantic 37F needs (the same 4-key artifact allowlist, L-03). Port its **shape**:

```typescript
// sseAdapter.ts:346-362 — the 4-key allowlist. Note it CONSTRUCTS the object field-by-field;
// it never copies `d` and deletes `path`.
if (frame.name === 'aura.artifact' && isArtifactDescriptor(frame.value)) {
  const d = frame.value;
  ...
  const display: DisplayPayload = {
    type: 'local_artifact',
    tool_call_id: d.tool_call_id,
    artifact: {
      filename: d.filename,
      ...(d.size_bytes !== undefined ? { size_bytes: d.size_bytes } : {}),
      ...(d.asset_id !== undefined ? { asset_id: d.asset_id } : {}),
      ...(d.mime_type !== undefined ? { mime_type: d.mime_type } : {}),
    },
  };
```

**Copy the technique, not the code:** construct `SnapshotArtifact` field-by-field from the four
allowlisted keys. Never `json.Unmarshal` into a `map[string]any` and `delete(m, "path")`.

**Copy the comment discipline** that names the trust boundary — `sseAdapter.ts:341-345`:

```typescript
// The raw host/container `path` is NEVER copied into the payload, in EITHER branch
// (asset_id present → download button; asset_id absent → degraded card): the browser
// must never receive a raw path for any authenticated session (D-13). `path` stays a
// backend/Telegram-only field (D-01).
```

37F's Go version must say the *opposite* boundary: *the recipient's browser is not a trust
boundary; this projection is the boundary.* (R-09.)

**The input struct — `internal/llm/client.go:24-41` — is what `BuildSnapshot` must NOT pass through:**

```go
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`   // ← L-01, the hard leak
	} `json:"function"`
}
```

`Function.Name` survives (D-08); `Function.Arguments`, `ID`, `ToolCallID` do not (L-01/L-09).

**Anti-analog — do NOT model on this.** `internal/agui/server_redact.go:52` is a regex **denylist**:

```go
func SanitizeString(msg string) string {
	out := secretPattern.ReplaceAllStringFunc(msg, func(match string) string { ... })
	out = urlUserinfoPattern.ReplaceAllString(out, "${1}[redacted]@")
	return tokenPattern.ReplaceAllStringFunc(out, func(match string) string { ... })
}
```

It is correct for its job (wire-error credential scrubbing, belt-and-suspenders) and **wrong** for
SC3: a denylist misses `/etc/passwd` in a shell result and every path shape nobody thought of.
Cite this file in the plan as the pattern to *avoid*, so a reviewer does not "helpfully" refactor
`redact.go` into a regex pass.

**Struct doc to mirror** — `audit_store.go:27-38` shows the house style for a projection type
(what it is, where each field comes from, and the sanitize obligation):

```go
// AuditEvent is one normalized row of the D-28 admin per-user activity feed: a single
// event unioned from the three identity-keyed audit ledgers, projected to a common shape.
// Target is the affected object (an MCP server_name / skill_name / tool_name); Detail is
// the ledger-specific note (an mcp reason / skill actor_id / tool status). Both may carry
// user-authored text, so the handler SanitizeStrings them before the wire (T-36-10-I).
type AuditEvent struct {
	Source    string    `json:"source"` // "mcp" | "skill" | "tool"
	...
}
```

`Snapshot`'s doc must state the inverse invariant: **the type has no field able to hold
args/results/paths, so the leak is a compile error, not a review miss.**

---

### `internal/share/store.go` (store, CRUD) — analog `internal/agui/audit_store.go` + `store_identity.go`

**Two analogs, each contributing a different half.**

**Half 1 — the raw-pgx store shape** (`audit_store.go:40-92`). This is the "no sqlc query is added"
precedent RESEARCH names:

```go
// PgAuditStore reads the identity-keyed audit ledgers for the admin audit UI (D-28).
type PgAuditStore struct {
	pool *pgxpool.Pool
}

// NewPgAuditStore builds the admin audit read store over the shared pool.
func NewPgAuditStore(pool *pgxpool.Pool) *PgAuditStore { return &PgAuditStore{pool: pool} }

func (s *PgAuditStore) ListActivityForIdentity(ctx context.Context, identityID string, limit, offset int) ([]AuditEvent, error) {
	rows, err := s.pool.Query(ctx, auditActivityQuery, auditIdentityKeys(identityID), identityID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("audit activity for %s: %w", identityID, err)
	}
	defer rows.Close()
	out := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.Source, &e.Action, &e.Target, &e.Detail, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return out, nil
}
```

**Copy exactly:** const-query-at-package-level, `defer rows.Close()`, `make([]T, 0, limit)`,
`%w`-wrapped errors at all three sites (`Query` / `Scan` / `rows.Err()`), and the **`rows.Err()`
check** — a missing `rows.Err()` is the classic pgx silent-truncation bug.

**Half 2 — the owner-gate + 404-on-miss** (`store_identity.go:28-53`). Every owner-scoped share
read (`ListForIdentity`, `RevokeForIdentity`) copies this:

```go
func (s *Store) GetForIdentity(ctx context.Context, conversationID, identityID string) (Conversation, error) {
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	...
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		row, gErr := q.GetConversationForIdentity(ctx, sqlc.GetConversationForIdentityParams{ID: id, IdentityID: owner})
		if gErr != nil {
			if errors.Is(gErr, pgx.ErrNoRows) {
				return fmt.Errorf("get conversation %s: %w", conversationID, ErrConversationNotFound)
			}
			return fmt.Errorf("get conversation %s: %w", conversationID, gErr)
		}
		conv = conversationFromRow(row)
		return nil
	})
	...
}
```

**Copy:** `parseUUID` on every id **before** the query; `db.WithIdentityTx(ctx, s.pool, identityID, ...)`
so the 0032 RLS backstop is live; `pgx.ErrNoRows` → a **sentinel** (`ErrShareNotFound`), which the
handler maps to 404. Its file header (`:14-23`) is the doc template — it states *why* both the WHERE
clause and RLS exist ("primary correctness path" + "kernel backstop").

> **`ResolveByToken` is the deliberate exception:** it is **not** owner-scoped (a public recipient
> has no principal), so it must **not** use `WithIdentityTx`. It reads on the plain pool with the
> OQ5 predicate. `audit_store.go:11-17` is the precedent for *documenting* a deliberate
> non-identity-scoped read, and the plan should require an equivalent block:
> ```go
> // PgAuditStore reads on a plain pool connection (app.current_identity unset), where the
> // 0032 policy is permissive-on-unset, so the join sees the target identity's rows. That
> // is correct BY DESIGN here: the ONLY caller is the admin audit handler, which is gated
> // server-side by RequireCapability(governance.write) at the route mount ... The route gate,
> // not the RLS session var, is the trust boundary for this read.
> ```
> 37F's version: *the token hash is the capability; the OQ5 predicate (`revoked_at IS NULL AND
> (expires_at IS NULL OR expires_at > now())`) is the trust boundary for this read.*

---

### `internal/share/service.go` (service, CRUD) — analog `internal/runner/runner_delete.go:38-74`

The ordered-lifecycle-with-owner-gate-first shape. `Create`/`Update`/`Revoke` all follow it:

```go
func (r *Runner) DeleteConversationLifecycle(ctx context.Context, identityID, convID string) (int64, error) {
	owner := resolveOwnerIdentity(identityID)

	// Owner gate (D-06): resolve the conversation owner-scoped. A foreign/absent id is
	// ErrConversationNotFound → (0, nil), so the surface's 403/404 split runs and NO teardown
	// touches another identity's live state.
	if _, err := r.Conv.GetForIdentity(ctx, convID, owner); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("delete lifecycle: owner gate: %w", err)
	}
	// 1. Cancel active work ...
	// 2. Expire pending pauses ... best-effort → slog.Warn, never a blocked delete
	// 5. Delete persistence, owner-scoped
	return r.Conv.DeleteForIdentity(ctx, convID, owner)
}
```

**Copy the three structural moves:**
1. **Owner gate runs FIRST, before any side effect.** `share.Create` calls
   `conv.GetForIdentity(ctx, convID, owner)` before minting anything — this is SC4 rows 1 and 2.
2. **Numbered steps in the doc comment matching numbered steps in the body** (`:31-37` ↔ `:51-73`).
   `ExpireDue` copies this for its OQ3 ordering: *1. drop blobs → 2. stamp the row* (never the
   reverse — see the sweep entry).
3. **Best-effort steps `slog.Warn` and continue; mandatory steps return.** `:58-61` is the model.

---

### `internal/runner/runner_delete.go` (modified — D-15 step 4.5)

**The analog is the file itself.** The insertion point is between `:70` (step 4) and `:73` (step 5).
Two things must be edited **in the same commit** (the doc list is load-bearing here, not decorative):

The doc list at `:31-37`:
```go
//  1. cancel active work   — fire the in-flight turn's ctx-cancel for (identity, session)
//  2. expire pending pauses — askuser auto-resolve (best-effort; the cascade delete backstops it)
//  3. evict session tools   — SessionEvictor.Evict + the gateway approval ledger
//  4. terminate bg jobs     — the plan-03 owner-scoped kill of this session's live shells
//  5. delete persistence    — the owner-scoped DeleteConversationForIdentity (+ sidecar purge)
```

…and the body at `:72-73`:
```go
	// 5. Delete persistence, owner-scoped (rows-affected drives the surface's 403/404/204).
	return r.Conv.DeleteForIdentity(ctx, convID, owner)
```

**Follow step 2's pattern, not step 5's** — share revoke is the *same* class as pause-expiry: the
FK `ON DELETE CASCADE` (OQ5) backstops the row, so a transient failure is a `slog.Warn`, **not** a
blocked delete. But R-10 is the catch: the FK does **not** drop Garage bytes. So the comment must
say what `:56-57` says for pauses, inverted:

```go
// 2. Expire pending pauses (askuser auto-resolve). Best-effort: the paused_states rows
// FK-cascade with the conversation on delete, so a transient failure here is a WARN, never
// a blocked delete — the mandated lifecycle step still runs first.
```

37F's version must state: *the row FK-cascades, but the Garage bytes do NOT (R-10) — a WARN here
orphans blobs, so the sweep's prefix reconcile is the backstop.*

**Consumer-seam rule:** `runner` must **not** import `internal/share`. Declare a consumer-side
interface, exactly as `handlers` does for `agui` (see `identity_purge.go:20-27` below). The file
already models this at `:91-99` (`tools.SessionJobTerminator` via a type assertion).

---

### `internal/cron/handlers/share_expiry.go` (handler, batch) — analog `identity_purge.go` (whole file)

**The closest analog in the phase — copy the file and rename.** All 41 lines:

```go
package handlers

import (
	"context"
	"time"
)

// KindIdentityPurge is the system-seeded soft-delete grace-window purge TaskKind (Phase 36
// D-27). Like skill_ttl_sweep it is NOT model-schedulable — the composition root seeds it,
// and the dispatcher routes it here. ...
const KindIdentityPurge TaskKind = "identity_purge"

// identityPurgeMaxDuration bounds one purge sweep. The scan is a small indexed query and the
// per-identity teardown is a handful of idempotent store deletes, so a 5-minute budget is
// generous even when several identities fall due in the same tick.
const identityPurgeMaxDuration = 5 * time.Minute

// IdentityPurger is the consumer-declared seam the purge handler drives (the SnippetSweeper
// pattern): the live *agui.Deprovisioner satisfies it via PurgeExpired, so this package does
// NOT import internal/agui (D-24, and it avoids the reverse-import cycle). ...
type IdentityPurger interface {
	PurgeExpired(ctx context.Context, now time.Time) (purged int, err error)
}

// NewIdentityPurgeHandler builds the grace-window purge sweep (D-27) over purger: ...
// A nil purger yields the disabled no-op sweep (harmlessly off, not an error). The missed
// sweep never reschedules (the saga is idempotent + resumable — the next tick re-evaluates
// the same purge_after set ...).
func NewIdentityPurgeHandler(purger IdentityPurger) Handler {
	var seam sweepFn
	if purger != nil {
		seam = purger.PurgeExpired
	}
	return newCountingSweep(KindIdentityPurge, identityPurgeMaxDuration, seam,
		"identity purge: disabled (no purger)", "identity purge", "identity purge ok: purged %d expired identit(y/ies)")
}
```

**Copy line-for-line**, substituting `Share`/`share_expiry_sweep`/`ExpireDue`. The four
non-obvious bits to preserve:
- **`ShareExpirer` is declared HERE, consumer-side** — `handlers` must never import `internal/share`.
- **`var seam sweepFn; if x != nil { seam = x.Method }`** — the nil-guard idiom. `newCountingSweep`
  turns a nil seam into a disabled no-op (`sweep.go:57-59`), which is the "nil expirer ⇒ no panic"
  test in RESEARCH's map.
- **`okFmt` MUST carry exactly one `%d`** — `sweep.go:30` states this as a contract.
- **`ReschedulesOnRecovery: false`** comes free from `newCountingSweep` (`sweep.go:51`), and its
  justification ("the sweep is idempotent — the next tick re-evaluates the same due set") is
  already written at `sweep.go:48-49`. Reuse the wording.

**`sweep.go:34-45` is the "extract a helper, never duplicate" precedent to cite in the plan** —
its header says it outright:

```go
// sweep.go is the single implementation behind every system-seeded counting-sweep handler
// (identity_purge D-27, sandbox_reap D-08): ... Rather than two parallel handler types
// (identical control flow, differing only in Kind/seam/messages), there is ONE
// countingSweepHandler parameterized by those bits ... (CLAUDE.md — extract a helper, never
// copy a parallel handler).
```

A bespoke `shareExpiryHandler` struct would violate this file's stated reason for existing.

---

### `internal/agui/share_api.go` (controller, request-response) — analog `assets_api.go:13-59`

**Route registration** (`assets_api.go:13-22`) — the Go 1.22 method+path form:

```go
func (s *Server) registerAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/assets/presign", s.handleAssetPresign)
	mux.HandleFunc("GET /api/assets/{id}/download", s.handleAssetDownload)
	...
}
```

**The stream-through download handler** (`assets_api.go:35-59`) — the template for both
`GET /s/{token}/asset/{id}` and the export endpoint:

```go
func (s *Server) handleAssetDownload(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rc, asset, err := s.assets.OpenForIdentity(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	defer func() { _ = rc.Close() }()

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", contentDisposition(asset.FileName))
	h.Set("Content-Length", strconv.FormatInt(asset.SizeBytes, 10))

	_, _ = io.Copy(w, rc)
}
```

**Copy verbatim:** the nil-service 503 guard; the four headers in that order; `defer rc.Close()`;
`io.Copy` with `r.Context()`-scoped reads; `http.Error(w, sanitizeErr(err), 404)` on **any** error.

**Its doc block (`:24-34`) is the security-invariant template** — 37F's public handler needs the
same block with the tier substituted:

```go
// handleAssetDownload streams the owner's asset body from the object store as a forced,
// stored-XSS-safe attachment (WEBART-03). It inherits RequireAuth whole-origin from the parent
// mux (no per-route auth wiring, no unauthenticated surface). The security invariants:
//   - D-12 existence-hiding: OpenForIdentity's ownership gate precedes any store read, and ANY
//     error (not-found OR not-owned) collapses to 404 — never 403, never 200.
//   - D-10 stored-XSS guard: the serve Content-Type is the neutral application/octet-stream plus
//     X-Content-Type-Options: nosniff regardless of the sniffed asset.MIMEType, which is NEVER
//     trusted as a serve header.
//   - D-11 header-injection guard: the filename rides Content-Disposition via contentDisposition.
//   - D-09 DoS-safe stream-through: the read is scoped to r.Context() so a client disconnect
//     cancels it and io.Copy unblocks (no goroutine leak); the stream is never presigned/redirected.
```

⚠️ **The one line that must be INVERTED:** *"It inherits RequireAuth whole-origin from the parent
mux (no per-route auth wiring, **no unauthenticated surface**)"* is **false** for `/s/{token}/*`.
That handler is the phase's only unauthenticated surface. Its doc must say so loudly and name the
token predicate as its gate, or a future reader will assume RequireAuth covers it.

**Body cap** (`assets_api.go:80`) for `POST /api/shares`:
```go
r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)   // maxRunBodyBytes = 1<<20, server.go:27
```

**Principal extraction** (`assets_api.go:201-204`) — reuse, do not re-derive:
```go
func principalIdentityID(r *http.Request) (string, bool) {
	identityID := principalFrom(r.Context())
	return identityID, identityID != ""
}
```

**Shared helpers already in-package (never re-implement):** `writeJSON` /
`writeJSONStatus` (`conversations_api.go:149,153`), `sanitizeErr` (`server_redact.go:41`),
`contentDisposition` (`content_disposition.go:23`), `maxRunBodyBytes` (`server.go:27`).

---

### `internal/agui/share_service.go` (interface) — analog `asset_service.go` (whole file)

The consumer-declared narrow-interface pattern. Whole file, 27 lines:

```go
package agui

import (
	"context"
	"io"

	"github.com/chetto1983/aura/internal/assets"
)

// AssetService is the narrow asset API surface consumed by AG-UI handlers.
type AssetService interface {
	Presign(context.Context, assets.PresignRequest) (assets.PresignResponse, error)
	Finalize(context.Context, string, string) (assets.Asset, error)
	GetForIdentity(context.Context, string, string) (assets.Asset, error)
	// OpenForIdentity streams the owner-scoped object body for the download route (WEBART-03/D-12):
	// the ownership gate precedes any store read, and it returns a stream-through ReadCloser, never
	// a presigned store URL (D-09). The caller closes the ReadCloser.
	OpenForIdentity(context.Context, string, string) (io.ReadCloser, assets.Asset, error)
	ListForThread(context.Context, string, string) ([]assets.Asset, error)
	...
}
```

**Copy:** the "narrow surface consumed by AG-UI handlers" framing; per-method doc **only** where a
contract is non-obvious (who closes, what the gate is); domain types from the owning package
(`share.Snapshot`, `share.Link`) in the signature — agui does **not** re-declare them.

**Wire it into `server.go` at two sites** — `:104-124`:
```go
type Server struct {
	run              Runner
	conv             ConversationStore
	approvals        ApprovalStore
	assets           AssetService      // ← add: share ShareService
	...
}
```
…and `:262`:
```go
	s.registerAssetRoutes(mux)          // ← add: s.registerShareRoutes(mux)
```
`:263-268` shows the required comment shape for a new registration (what the routes are, where the
parent-mux mount + gate live):
```go
	// WEBVOICE-01/02/03 web-voice routes (37C-03): POST /api/tts (audio/mpeg + soft
	// char cap), POST /api/stt (transcribe-and-discard, D-08 — no asset/DB row), GET
	// /api/voice/capabilities (SELF-scoped presence probe, never 503). Colocated with
	// their handlers (voice_api.go); the parent-mux mounts (the two POSTs behind
	// agentRunCapability, capabilities RequireAuth-only) live in serve_webui_voice.go.
	s.registerVoiceRoutes(mux)
```

---

### `internal/agui/audit_store.go` (modified — the 4th union leg)

**The analog is the file's own 2nd leg.** `:53-69`:

```go
const auditActivityQuery = `
SELECT source, action, target, detail, created_at FROM (
    SELECT 'mcp'   AS source, action           AS action, server_name  AS target, COALESCE(reason, '')     AS detail, created_at AS created_at
      FROM aura.mcp_audit
      WHERE actor_identity_id = ANY($1::text[])
    UNION ALL
    SELECT 'skill' AS source, action, skill_name, actor_id, created_at
      FROM aura.skill_audit
      WHERE identity_id = ANY($1::text[])
    UNION ALL
    SELECT 'tool'  AS source, ti.event_kind, ti.tool_name, COALESCE(ti.status, ''), ti.ts
      FROM aura.tool_invocations ti
      JOIN aura.conversations c ON c.id = ti.conversation_id
      WHERE c.identity_id = $2::uuid
) feed
ORDER BY created_at DESC
LIMIT $3 OFFSET $4`
```

**Copy the `'skill'` leg exactly** — it is the `identity_id text = ANY($1::text[])` shape
`share_audit` shares. Two contracts the comment at `:48-52` states and the new leg must honor:
- *"Column names come from the first SELECT (UNION ALL matches by **position**)"* — the share leg
  must project **5 columns in the same order**, aliases optional.
- `$1::text[]` comes from `auditIdentityKeys` (`:99-104`), which appends the literal `'local'` for
  the seeded operator. `share_audit.identity_id text` unions for free; **a `uuid` column would not**
  (it could not hold `'local'`). This is why OQ5 specifies `text` + no FK.

Also update the file header (`:3-9`), which currently enumerates **three** ledgers:
```go
// audit_store.go is the raw-pgx read store behind the D-28 admin per-user audit API
// (audit_api.go). It unions the three identity-keyed audit ledgers — aura.mcp_audit
// (keyed on actor_identity_id), aura.skill_audit (keyed on identity_id), and
// aura.tool_invocations (conversation-keyed, joined to aura.conversations.identity_id) —
```
…and `AuditEvent.Source`'s doc (`audit_api.go:33`):
```go
	Source    string    `json:"source"` // "mcp" | "skill" | "tool"     → add | "share"
```
(CLAUDE.md: *"comments-updated in the SAME commit."*)

---

### `internal/objectstore/types.go` (modified — the `Share*Key` siblings)

**The analog is 3 lines below the insertion point.** `:60-62`:

```go
func AssetKey(identityID, assetID string) string {
	return "identity/" + identityID + "/asset/" + assetID + "/original"
}
```

**Copy:** package-level func, plain concatenation, no `path.Join` (which would normalize `..`
and silently permit traversal), no error return. **Deviate on one axis only** — OQ1 takes
`uuid.UUID`, not `string`, so `"../identity/<victim>/asset/x"` is unrepresentable in the type.
Note the deviation explicitly in the plan so it does not read as accidental inconsistency.

**The test analog is `objectstore_test.go:12-22`** — the negative-substring invariant, which the
D-12 namespace-disjointness test extends:

```go
func TestAssetKeyContainsNoFilename(t *testing.T) {
	key := AssetKey("identity-1", "asset-2")
	if key != "identity/identity-1/asset/asset-2/original" {
		t.Fatalf("AssetKey() = %q, want identity/identity-1/asset/asset-2/original", key)
	}
	for _, forbidden := range []string{".pdf", ".jpg", ".png", "invoice", "\\"} {
		if strings.Contains(key, forbidden) {
			t.Fatalf("AssetKey() contains filename-like fragment %q in %q", forbidden, key)
		}
	}
}
```

**Copy:** exact-equality assert **then** a forbidden-substring loop. 37F's forbidden list is
`{"identity/", "..", "\\"}` for `ShareSnapshotKey`, plus the reverse (`AssetKey` never has
`"share/"`).

**`FakeStore` (`fake.go:17-21`) needs no work** — it is keyed on `ObjectRef`, so `share/` keys
round-trip today. It is what keeps 37F inside the 2-tag coverage gate (R-07).

---

### `cmd/aura/serve_webui_share.go` **(new)** — analog `serve_webui_musr.go` (whole file)

**R-01's mitigation. `serve_webui_musr.go` is the exact template** — it is the sibling that mounts
routes with a mix of gated and ungated, which is precisely 37F's shape:

```go
package main

// serve_webui_musr.go carries the Phase-36 (MUSR-01) admin/user-distinction parent-mux
// mounts, kept OUT of serve_webui.go so that file stays under the 600-LOC ceiling. It
// mounts the D-03/D-26/D-28 surface registered on the agui Server.Mux (audit_api.go):
//
//   - GET /api/me — SELF-scoped ... It inherits the whole-origin RequireAuth from the
//     parent-mux wrap; NO RequireCapability (self-read is not privileged).
//   - GET /api/admin/identities, ... — the admin surface, each interposed with
//     RequireCapability(governance.write). The SPA hide is cosmetic; THIS server-side gate
//     is the trust boundary (T-36-10-E). ...

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const (
	meRoute                    = "GET /api/me"
	adminIdentitiesRoute       = "GET /api/admin/identities"
	...
)

// registerMUSRRoutes mounts the admin/user-distinction routes on the parent mux. Each
// delegates to the AG-UI handler (routes live on Server.Mux). Method+path-specific so each
// wins Go 1.22 longest-pattern precedence over the bare "/api/" carve-out and the "/" embed
// catch-all; the "/api/" fallback exclusion already returns them as backend routes.
func registerMUSRRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(meRoute, aguiHandler)
	mux.Handle(adminIdentitiesRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	...
}
```

**Copy the whole shape:** file-header naming the phase + the "kept OUT … 600-LOC ceiling" reason +
a per-route bullet stating **which gate and why**; a `const (…)` route block; one
`register*Routes(mux, aguiHandler, auth)` func; `mux.Handle(route, aguiHandler)` for ungated,
`mux.Handle(route, agui.RequireCapability(aguiHandler, auth, cap))` for gated.

**37F's mount table** (mirroring the musr bullets):

| Route | Gate | Precedent for that choice |
|---|---|---|
| `POST /api/shares` (internal tier) | `aguiHandler` bare — RequireAuth only | `meRoute` (`_musr.go:40`) / `composerSkillsRoute` — D-02 "internal links need NO capability" |
| `POST /api/shares` (public tier) | **in-handler** `share.public` + kill-switch | **R-08** — see below |
| `GET/PATCH/DELETE /api/shares/*` | `aguiHandler` bare | owner-scoped, `*ForIdentity`-gated |
| `GET /s/{token}/data`, `GET /s/{token}/asset/{id}` | `aguiHandler` + `PublicRoute` | `isPublicBootstrapRoute` (`serve_webui.go:542`) |

⚠️ **The one place 37F must NOT copy `_musr.go`.** Its pattern is
`RequireCapability` **at the mount**. `auth.go:281-283`:

```go
func RequireCapability(next http.Handler, deps AuthDeps, capability string) http.Handler {
	if !deps.SecretConfigured {
		return next // loopback dev - auth disabled, pass-through with RequireAuth
	}
```

On loopback (`!SecretConfigured`) the gate **vanishes** (R-08). That is fine for
`governance.write` (loopback = the operator's own box) and **not** fine for a link designed to
leave the box. The plan must mount `share.public` **and** re-check the org kill-switch **inside**
the handler. Two gates; one survives loopback.

**The capability const** goes in this new file, following `serve_webui.go:99-118`'s doc style
(each const explains what it gates and why it is not the neighbouring one):

```go
// governanceWriteCapability gates every Phase-29 governance WRITE surface (MCP config
// mutation + skill install). It is strictly stronger than governance.read: a write can
// install a new MCP server or a RISKY supply-chain skill, so it requires its own grant.
// The seeded `local` identity holds the `*` wildcard so it passes regardless of the exact
// name (the name becomes load-bearing once real grants arrive). ...
const governanceWriteCapability = "governance.write"
```

`identityCreateCapability` (`serve_webui.go:278`) is the closer semantic sibling for
`sharePublicCapability` (per-user, off-by-default) — OQ2's argument. Cite `:271-277` in the const doc.

---

### `cmd/aura/serve_webui.go` (modified — ~4 LOC, 593 → ~597)

**Both edits have an in-file analog.**

**Edit 1 — one `register*Routes` call + its comment.** `:507-511` is the template:
```go
	// The 37D WEBSKILL-01 composer skill-picker mount (GET /api/composer/skills) lives in
	// serve_webui_composer.go to keep this file under the 600-LOC ceiling: bare aguiHandler,
	// RequireAuth-only (like voiceCapabilitiesRoute/meRoute) — deliberately NOT
	// governance.read-gated so an ordinary identity gets the global picker list (D-03).
	registerComposerRoutes(mux, aguiHandler, auth)
```
⚠️ **Budget honestly:** the precedent comments are 3-4 lines each. Header comment + call ≈ 5 LOC →
**598/600, a 2-LOC margin.** RESEARCH's R-01 assumed ~4. **The plan should treat the
`serve_webui.go` delta as a hard 1-line call + a ≤2-line comment, or split something out.**

**Edit 2 — the `PublicRoute` chain.** `:523-534`, designed for exactly this:
```go
	previousPublicRoute := auth.PublicRoute
	auth.PublicRoute = func(r *http.Request) bool {
		if r.Method == http.MethodGet && r.URL.Path == authConfigRoute {
			return true
		}
		if isPublicPasswordResetRoute(r) {
			return true
		}
		if isPublicBootstrapRoute(r) {
			return true
		}
		return previousPublicRoute != nil && previousPublicRoute(r)
	}
```
**Add one `if isPublicShareRoute(r) { return true }` (2 LOC)** and put the predicate itself in
`serve_webui_share.go`. `isPublicPasswordResetRoute` (`:546-559`) is the predicate template —
**method-checked first, then an exact-path switch, default false** (fail-closed):
```go
func isPublicPasswordResetRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/auth/password-reset/start", ...:
		return true
	default:
		return false
	}
}
```
`isPublicShareRoute` needs a **prefix** match (`/s/{token}`), not a switch — so it must be
`r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/s/")`, fail-closed on every other
method. This is the pure-unit-testable predicate in RESEARCH's daemon-free table.

**Edit 3 — `fallbackExcludedPrefixes()` (`:85-97`): DO NOT TOUCH IT.**
```go
func fallbackExcludedPrefixes() []string {
	return []string{
		"/healthz", "/readyz", "/debug/vars", "/metrics",
		"/agent/",   // whole AG-UI agent namespace (mux registers the exact /agent/run)
		"/threads/", // AG-UI threads subtree
		integrationsRoutePrefix,
		"/api/",            // forward-compat carve-out; exclusion-only, never a mux registration
		authBasePath + "/", // Authula credential subtree — a backend route, never the SPA shell
	}
}
```
Two consequences, both load-bearing and neither in RESEARCH:
- **`/api/shares` is already covered** by the `"/api/"` carve-out → an unknown share API path 404s
  as a backend route, correctly.
- **`/s/` must stay OUT of this list.** D-03 requires `/s/{token}` to fall through
  `mux.Handle("/", static)` (`:517`) to the SPA shell. Adding `/s/` here would make it 404 and
  break the public page. The plan should say this explicitly — "add the new prefix to the exclusion
  list" is the reflex this phase must not follow.

---

### `internal/db/migrations/0040_shared_links.{up,down}.sql` — analogs `0035_*` + `0034_*`

**`0035_assets_source_kind_agent.up.sql` is the header template** — Source line, the failure the
migration fixes, and the grant note:

```sql
-- Source: Phase 37A (Web Artifact Delivery Lane) / WEBART-01 / D-06.
-- send_file under an authenticated identity ingests the produced file into Garage as an owned
-- aura.assets row (assets.Service.IngestAgentFile). That row's source_kind must be 'agent' so an
-- agent-produced deliverable is first-class and distinguishable from human 'web'/'telegram'/'cli'
-- uploads (audit + future retention/filtering), but the 0020 source_kind CHECK admits only the
-- original three, so the ingest INSERT fails with 23514 until this widen lands.
--
-- Mirror 0034's widen verbatim: the 0020 constraint is an inline column CHECK, so Postgres
-- auto-named it `assets_source_kind_check`; drop + re-add it with the extra 'agent' member. 0020
-- already GRANTed aura_app DML on aura.assets (aura_migrate owns DDL), so no grant change is
-- needed here.
```

**Copy:** `-- Source: Phase <n> / <REQ-ID> / <D-nn>.`; the concrete failure mode **with the SQLSTATE**
(`fails with 23514`); the explicit grant/ownership note (`aura_migrate owns DDL`).

**`0034_*.up.sql:19-21` is the scheduler-kind widen — copy it verbatim** for `share_expiry_sweep`:
```sql
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge', 'sandbox_reap'));
```
⚠️ **Re-read the CURRENT list at execute time** — this is the exact drift R-04 is about. The kind
list has been widened three times (0010, 0033, 0034); another in-flight phase may have added a
fourth. Copy the list **from disk**, then append `'share_expiry_sweep'`.

**`0034_*.down.sql` is the down template — this is the one most likely to be gotten wrong:**
```sql
-- Reverse 0034: restore the 0033 scheduler_tasks.kind CHECK (drop the 'sandbox_reap'
-- widening), leaving 'identity_purge' in place (the 0033 list).
--
-- A down that narrows the kind CHECK must first remove the rows the widening admitted:
-- a live sandbox_reap sweep task (seeded at boot ...) violates the restored 0033 CHECK and
-- aborts the whole down mid-chain (dirty database). agent_job_runs FKs scheduler_tasks(id)
-- ON DELETE CASCADE, so delete its reap-task run rows first (explicit here for parity ...).
DELETE FROM aura.agent_job_runs
    WHERE task_id IN (SELECT id FROM aura.scheduler_tasks WHERE kind = 'sandbox_reap');
DELETE FROM aura.scheduler_tasks WHERE kind = 'sandbox_reap';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge'));
```
**Copy the invariant:** *delete the rows the widening admitted BEFORE narrowing the CHECK*, and
delete FK-children first. 37F's down must `DELETE FROM aura.agent_job_runs WHERE task_id IN (… kind
= 'share_expiry_sweep')`, then the task rows, then narrow — **then** `DROP TABLE aura.share_audit;
DROP TABLE aura.shared_links;` (children before parents). `0035_*.down.sql` shows the same rule for
data (`DELETE FROM aura.assets WHERE source_kind = 'agent';`) with the reason stated inline.

**Grants** (OQ5) follow `0035`'s note: `aura_migrate` owns DDL; `aura_app` gets DML. The
append-only `share_audit` gets `SELECT, INSERT` only — the asymmetry is the audit-integrity
statement and deserves an inline comment.

---

### `web/src/shell/ShareShell.tsx` **(new)** — analog `ArtifactsShell.tsx:23-45` (**the exact template**)

**37B reserved this spot in code.** `ArtifactsShell.tsx:20-22`:
```tsx
// D-01: the header-style doc toggle (the reference's top-right icon; the adjacent share-arrow is
// 37F, not built). It floats over the chat workspace so it reads as a header control without
// editing ShellHeader (out of this plan's scope) or shifting the chat layout.
```
**Update this comment in the same commit** — once `ShareToggle` ships, "not built" is stale.

**`ArtifactsToggle` (`:23-45`) — mirror it exactly:**
```tsx
export function ArtifactsToggle({
  active,
  onToggle,
}: {
  readonly active: boolean;
  readonly onToggle: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={t('artifacts.toggleAria')}
      aria-pressed={active}
      data-active={active}
      onClick={onToggle}
      className="pointer-events-auto rounded-full bg-surface/70 text-text-muted backdrop-blur hover:bg-surface-2 hover:text-text data-[active=true]:bg-surface-2 data-[active=true]:text-accent-text"
    >
      <FileText aria-hidden="true" focusable="false" />
    </Button>
  );
}
```

**Copy verbatim:** `readonly` props inline-typed; `useTranslation()`; `variant="ghost" size="icon"`;
`className` **character-for-character** (`pointer-events-auto` is mandatory — the parent cluster is
`pointer-events-none`); `aria-hidden="true" focusable="false"` on the lucide icon.

**Three deliberate deviations — each with a reason the plan must record:**
1. **`aria-haspopup="dialog"`, NOT `aria-pressed`.** `ArtifactsToggle` toggles a *panel*;
   `ShareToggle` opens a *modal*. `aria-pressed` on a dialog-opener is an a11y bug.
2. **Icon: lucide `Share2`**, not `FileText`.
3. **`data-shared={sharedCount > 0}`** alongside `data-active`, styled with the same
   `data-[…=true]:` idiom the analog already uses at `:40`:
   `data-[active=true]:bg-surface-2 data-[active=true]:text-accent-text`
   → `data-[shared=true]:text-accent-text` (+ `text-warning` for a live public link — the token
   exists at `LocalArtifactDisplay.tsx:81`). This is the "beat open-webui" win; it costs ~4 LOC
   **because the pattern already exists**.

**`ArtifactsShell.tsx:87-89`'s Drawer note is the focus-trap reuse mandate** — do not build a dialog:
```tsx
// D-04: below `lg` the panel collapses into the shared right Drawer, routed through
// useSurfaceRestore's overlay slot so it obeys "one heavy overlay at a time" against the nav
// drawer. Identical portal/focus-trap/Esc UX to the left nav drawer.
```

---

### `web/src/shell/useSharePanel.ts` **(new)** — analog `useArtifactsPanel.ts`

**R-02's mitigation. Copy the header's stated reason for existing** (`:4-7`):
```ts
// 37B plan 07 — the Artefatti panel state seam, extracted from AppShell.tsx so the shell stays
// under the 600-LOC cap (refactor-on-touch). This owns the desktop-vs-mobile decision, the
// persisted open/closed intent, and the dynamic panelIds (no layout-key bump — RESEARCH
// Pattern 1). The presentational pieces live in ./ArtifactsShell.
```
…and the exported-state-interface shape (`:47-61`), with a doc line per member:
```ts
export interface ArtifactsPanelState {
  readonly isDesktop: boolean;
  /** Whether the panel is currently visible on the live surface (desktop panel or mobile drawer). */
  readonly artifactsActive: boolean;
  ...
  /** Flip the panel on the live surface (the header toggle). */
  readonly toggleArtifacts: () => void;
}
```
**`useCallback` on every returned function** (`:81-101`) — the toggle is passed to a memo-able child.

⚠️ **Do NOT copy the localStorage-persist block (`:73-79`) or `useIsArtifactsDesktop` (`:27-45`).**
A share modal is **not** a persisted panel and has **no** desktop/mobile split — it is
`useState<ShareModalState>` + open/close + the mutation wiring. Copying the persistence would
resurrect a stale "share modal open" across reloads. `useSharePanel` should be **~30 LOC**, not 117.
If it ends up mirroring `useArtifactsPanel` closely, that is a smell that the wrong analog was
followed.

---

### `web/src/chat/share/ShareModal.tsx` **(new)** — analog `DocumentUploadDialog.tsx` (whole file)

The Dialog-with-state-and-async-action shape, at almost exactly 37F's complexity (101 LOC):

```tsx
import { Button } from '@/components/ui/button';
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

interface DocumentUploadDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly onUploaded: () => void;
}

export function DocumentUploadDialog({ open, onOpenChange, onUploaded }: DocumentUploadDialogProps) {
  const { t } = useTranslation();
  const [file, setFile] = useState<File | undefined>(undefined);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');

  async function upload() {
    if (file === undefined || uploading) return;
    setUploading(true);
    setError('');
    try {
      await uploadLibraryDocument(file, setProgress);
      onOpenChange(false);
      ...
    } catch (err) {
      setError(err instanceof Error ? err.message : 'upload failed');
    } finally {
      setUploading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('documents.upload.title')}</DialogTitle>
          <DialogDescription>{t('documents.upload.body')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="document-upload-file">{t('documents.upload.file')}</Label>
          <Input id="document-upload-file" ... />
          {uploading ? (<div role="status" className="text-[13px] text-text-muted">…</div>) : null}
          {error.length > 0 ? (<div role="alert" className="text-[13px] text-danger">{error}</div>) : null}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => { onOpenChange(false); }}>
            {t('documents.actions.cancel')}
          </Button>
          <Button type="button" disabled={file === undefined || uploading} onClick={() => void upload()}>
            {uploading ? <Spinner /> : <Upload aria-hidden="true" />}
            {t('documents.actions.upload')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

**Copy:** the `@/components/ui/dialog` barrel import (**do not build a modal** — the primitive is
portal+focus-trap+Esc already); `{ open, onOpenChange, on<Done> }` props, all `readonly`;
`Dialog > DialogContent > DialogHeader(DialogTitle+DialogDescription) > body > DialogFooter`;
the guard-clause + `try/catch/finally` async handler with `setError` in `catch` and the flag reset
in `finally`; `role="status"` for progress and `role="alert"` for errors; `variant="outline"`
Cancel then primary action in `DialogFooter`; `onClick={() => void action()}` (never `async onClick`);
`disabled` on the primary while in flight.

**RESEARCH's modal adds** a `<fieldset>/<legend>` radio group, the conditional warning
(`aria-describedby`-linked), the expiry chips, and the `idle→creating→shared→…` state machine. None
have an in-repo analog — RESEARCH §UI/UX 2 is the source. `ArtifactsPanel.tsx:121-122` is the motion
idiom to reuse for the warning reveal:
```tsx
className="animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards"
style={{ animationDelay: `${String(Math.min(index, 8) * 40)}ms` }}
```

**Revoke confirm → `DeleteConfirmDialog.tsx` (whole file).** It is a thin `ConfirmDialog` wrapper
and the header states the invariant to mirror:
```tsx
// DeleteConfirmDialog gates the D-07 hard delete (T-25-14): "Delete permanently"
// never fires Store.Delete without this confirm. The shared ConfirmDialog is
// portal-backed, focus-trapped, Escape-dismissable, and always centered.
```
`RevokeConfirmDialog` = the same file with `conversations.delete.*` → `share.revoke.*`. **~41 LOC,
copy it.**

---

### `web/src/routes/SharePage.tsx` **(new)** — analog `main.tsx` route table + `LoginPage.tsx`

**`main.tsx:13-19,30-46` — the lazy-route registration:**
```tsx
const LoginPage = lazy(() => import('./routes/LoginPage').then((mod) => ({ default: mod.LoginPage })));
const NotFoundView = lazy(() => import('./routes/NotFoundView').then((mod) => ({ default: mod.NotFoundView })));
...
// The router provides client navigation + a client-side 404 (NotFoundView) only.
// It is NOT an auth boundary — the server's RequireAuth (Plan 24-03) is the real
// gate; we never hide a route purely client-side (T-24-21).
<Routes>
  <Route path="/login" element={<LoginPage />} />
  <Route path="/" element={<AppShell />} />
  {/* Deep link to a conversation at a search match (D-08); AppShell
      reads :id into the active thread. */}
  <Route path="/c/:id" element={<AppShell />} />
  <Route path="*" element={<NotFoundView />} />
</Routes>
```
**Copy:** the `lazy(() => import(...).then((mod) => ({ default: mod.X })))` named-export shape; a
`<Route path="/s/:token" element={<SharePage />} />` line; the inline `{/* comment */}` naming the
decision (as `/c/:id` does). **The 2-LOC delta is the whole `main.tsx` change.**

**`LoginPage` is the structural precedent for a route that renders without a session** — the same
class as `SharePage`. **Reinforce the header comment's rule:** the router is *not* an auth
boundary. `SharePage` renders whatever `GET /s/{token}/data` returns; a 404 from that fetch is the
gate, and the page must render the *same* body for unknown/expired/revoked (RESEARCH §4: no oracle).

---

### `web/src/chat/artifacts/renderers/assetSourceContext.ts` **(new — R-05)** — analog `voiceModeContext.ts` (whole file)

**R-05's "context with a default" mitigation already exists as a pattern.** Whole file, 35 LOC:

```ts
import { createContext, useContext } from 'react';
import type { VoiceCapabilities } from './useVoiceCapabilities';

// voiceModeContext — the context object + consumer hook for the ephemeral voice
// mode (D-06). Kept in a NON-component module (mirroring sourceExplorerControls.ts)
// so VoiceModeProvider.tsx stays component-only (react-refresh/only-export-components).
// useVoiceMode returns a DISABLED default when no provider is mounted, so the speaker
// control degrades to "absent" outside the provider (e.g. the isolated chat tests)
// rather than throwing.

export interface VoiceModeState { readonly caps: VoiceCapabilities; ... }

const DISABLED: VoiceModeState = { caps: { tts: false, stt: false }, voiceMode: false, ... };

export const VoiceModeContext = createContext<VoiceModeState>(DISABLED);

/** Read the voice-mode state; a disabled default (caps false) when no provider is mounted. */
export function useVoiceMode(): VoiceModeState {
  return useContext(VoiceModeContext);
}
```

**Copy exactly, this is the whole R-05 answer:** context + hook in a **non-component `.ts` module**
(the `react-refresh/only-export-components` lint rule — a `.tsx` with both would fail
`make quality`'s web gate); a **safe default constant** so no consumer throws without a provider;
a one-line hook doc naming the default behavior.

37F's default is the **current** URL, so every existing call site and test stays byte-identical:
```ts
const IDENTITY_SCOPED: AssetSource = {
  assetUrl: (assetId) => `/api/assets/${encodeURIComponent(assetId)}/download`,
  credentials: 'same-origin',
};
export const AssetSourceContext = createContext<AssetSource>(IDENTITY_SCOPED);
```

**The three call sites to re-point** (`useAssetContent.ts:33-36`, `PreviewModal.tsx:73,101`):
```ts
      const res = await fetch(`/api/assets/${encodeURIComponent(assetId)}/download`, {
        credentials: 'same-origin',
        signal: controller.signal,
      });
```
Its header already states the extract-a-helper rationale — the same argument now extends to the URL:
```ts
// useAssetContent — the shared same-origin fetch primitive for the text/html/docx/xlsx
// renderers ... Extracted so the four non-object-URL renderers don't duplicate the
// fetch+abort block (jscpd threshold 0).
```
⚠️ **Drop `credentials: 'same-origin'` on the public path** (R-05) — hence `credentials` on the
context value, not hardcoded.

**`HtmlPreview.tsx` (whole file) is reused VERBATIM — do not fork it for the public page.** Its
comment is the D-03 policy statement and the reason a Go template renderer is forbidden:
```tsx
// text/html (D-07 / WEBART-07 / T-37B-08): untrusted agent HTML renders in a NULL-ORIGIN
// iframe. sandbox="allow-scripts" WITHOUT the same-origin token makes the frame's origin null —
// scripts run, but document.cookie is empty, window.parent access throws (cross-origin), and
// fetch('/api/…') carries no ambient session, so the content cannot read our cookies/DOM or
// reach Garage. The bytes are fed via srcDoc (fetched text), NEVER src=downloadURL ...
// Granting the same-origin token here is forbidden — it would let the sandboxed script drop
// its own sandbox.
export default function HtmlPreview({ assetId, fileName }: RendererProps) {
  const { data, error } = useAssetContent(assetId, 'text');
  ...
  return <iframe srcDoc={data} sandbox="allow-scripts" title={fileName} className="h-full w-full border-0 bg-white" />;
}
```
Once `useAssetContent` reads the context, `HtmlPreview` works on the public page **with zero edits**.
That is the payoff of the R-05 seam and the argument against prop-threading.

---

### `web/src/i18n/resources.share.ts` **(new)** — analog `resources.compaction.ts` (whole file)

**R-03's mitigation.** The per-domain module shape (64 LOC — the closest size match to 37F's ~70):

```ts
export const compactionEn = {
  compaction: {
    heading: 'Compaction history',
    description: 'Working context changes; the canonical conversation remains intact …',
    kinds: {
      compaction: 'Semantic compaction',
      l1_offload: 'L1 payload offload',
    },
    metadata: '{{trigger}} · {{reason}} · {{delta}} tokens',
    previewCheckpoint: 'Preview checkpoint {{id}}',
  },
};
export const compactionIt = {
  compaction: {
    heading: 'Cronologia compattazione',
    ...
  },
};
```

**Copy:** two exports `<domain>En` / `<domain>It`; **one top-level namespace key** (`share: {…}`)
so `t('share.modal.title')` resolves; **identical key trees in both** (the parity test asserts it);
`{{interpolation}}` for counts/dates; nested sub-objects for enumerations (`kinds:` → `tier:`,
`expiry:`); proper Italian typography (`’` U+2019, `…` U+2026 — see `:44`).

**Wiring into `resources.ts` — exactly 4 LOC, and the alphabetical-ish import block at `:1-17`:**
```ts
import { compactionEn, compactionIt } from './resources.compaction';
```
plus one spread in each language block (`:160` and `:437`):
```ts
      ...compactionEn,
      ...compactionIt,
```
576 → 580. Do **not** inline share keys into `resources.ts` — that is the R-03 breach.

---

### `web/src/AppShell.tsx` (modified — the cluster mount, 591 → ~595)

**The analog is the file's own cluster.** `:514-517`:
```tsx
                <div className="pointer-events-none absolute right-3 top-2.5 z-20 flex items-center gap-1">
                  <VoiceModeToggle />
                  <ArtifactsToggle active={artifactsActive} onToggle={toggleArtifacts} />
                </div>
```
**Insert `<ShareToggle …/>` between them** — order `[VoiceModeToggle] [ShareToggle] [ArtifactsToggle]`
(RESEARCH §1: artifacts stays rightmost, it opens the right panel; share is an action).

**The delta budget must hold at ≈4 LOC:** 1 import + 1 hook call + 1 element + 1 modal mount. Every
line beyond that goes in `useSharePanel.ts` / `ShareShell.tsx`. `:522-524` shows the conditional-mount
idiom if the modal needs one:
```tsx
            {artifactsPanelMounted ? (
              <ArtifactsResizablePanel threadId={activeThreadId} onClose={closeDesktopPanel} />
            ) : null}
```
⚠️ **591 + 4 = 595. Five LOC of margin.** R-02 flags AppShell for a refactor-on-touch split **in
this phase** (CLAUDE.md: "DEEP REFACTOR ON TOUCH … LOC ≤600 in the SAME commit"). The plan should
carry that split as an explicit task, not a hope — `useArtifactsPanel.ts`'s header (`:4-7`) is the
precedent for **how** AppShell has been split before, and the same move (extract a state seam to
`web/src/shell/`) is available again.

---

### `web/src/chat/artifacts/ArtifactsPanel.tsx` (modified — the "Condiviso" section)

**The analog is the file itself.** The list-with-staggered-reveal (`:111-129`):
```tsx
      <div className="min-h-0 flex-1 overflow-y-auto">
        {rows.length === 0 ? (
          isLoading ? null : (<EmptyState />)
        ) : (
          <ul className="flex flex-col gap-1.5">
            {rows.map((asset, index) => (
              <li
                key={asset.id}
                className="animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards"
                style={{ animationDelay: `${String(Math.min(index, 8) * 40)}ms` }}
              >
                <ArtifactRow asset={asset} onPreview={setActive} />
              </li>
            ))}
          </ul>
        )}
      </div>
```
…the section header (`:82-95`) with its `uppercase tracking-[0.14em] text-text-faint` type
treatment, and `EmptyState` (`:147-160`) with its glyph-plate + two-line copy.

⚠️ **The panel is 160 LOC and its props contract is `{ threadId, onClose }` — stated as a contract
at `:9-12`:**
```tsx
// ArtifactsPanel (D-13/D-17/WEBART-05/06): the self-contained "Artefatti" surface.
// Props are exactly { threadId, onClose } so the AppShell (plan 07) mounts it — as a
// ResizablePanel on desktop or a right Drawer on mobile — without re-deriving any
// contract.
```
**Do not add props.** The "Condiviso" section derives from `threadId` via its own hook
(`useThreadShares(threadId)`), mirroring `useThreadArtifacts(threadId)` at `:40`. Extract the
section into `web/src/chat/share/SharedSection.tsx` rather than growing the panel — the header's
"self-contained" claim is the argument.

**`useThreadArtifacts.ts` is the hook analog AND the server-side filter spec (R-12/D-09):**
```ts
export function useThreadArtifacts(threadId: string) {
  return useQuery({
    queryKey: ['assets', threadId],
    enabled: threadId.length > 0,
    queryFn: ({ signal }) => listThreadAssets(threadId, signal),
    select: selectAgentArtifacts,
  });
}

/** The pure projection applied to the cached asset list (exported for direct unit
 *  coverage of the filter + newest-first sort without a React render). */
export function selectAgentArtifacts(assets: readonly Asset[]): Asset[] {
  return assets
    .filter((a) => a.source_kind === 'agent' && a.status !== 'deleted' && a.status !== 'canceled')
    .slice()
    .sort((a, b) => (b.created_at ?? '').localeCompare(a.created_at ?? ''));
}
```
**Copy for `useThreadShares`:** `useQuery` + `queryKey: ['shares', threadId]` + `enabled` guard +
a pure exported `select` (**exported specifically so it unit-tests without a render** — copy that
rationale). **And copy `selectAgentArtifacts`'s predicate into Go** — `share.bundleFilter` must be
`source_kind == "agent" && status != "deleted" && status != "canceled"`, byte-for-byte the same rule
(R-12). The plan should name this as "the same rule, enforced at the trust boundary this time" and
cross-reference both sites so they cannot drift.

---

## Shared Patterns

### Auth / capability gate
**Source:** `internal/agui/auth.go:197,213,281`; mount style `cmd/aura/serve_webui_musr.go:39-46`
**Apply to:** every share route
```go
// auth.go:213 — the public allowlist hook
if deps.isPublicPath(r.URL.Path) || (deps.PublicRoute != nil && deps.PublicRoute(r)) {
	next.ServeHTTP(w, r)
	return
}
```
```go
// auth.go:281-284 — the loopback fail-open (R-08)
func RequireCapability(next http.Handler, deps AuthDeps, capability string) http.Handler {
	if !deps.SecretConfigured {
		return next // loopback dev - auth disabled, pass-through with RequireAuth
	}
```
**Rule:** RequireAuth is inherited whole-origin; `/s/*` opts out via `PublicRoute`; `share.public`
is gated at the mount **and** re-checked in-handler (R-08).

### 404-on-miss existence hiding
**Source:** `conversations/store_identity.go:25-27` (sentinel) + `agui/assets_api.go:46-49` (handler)
**Apply to:** every owner-scoped share read/mutate + every token resolve
```go
	rc, asset, err := s.assets.OpenForIdentity(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
```
**Rule:** ANY error → 404. Never 403, never a distinguishable body. Unknown/expired/revoked tokens
share one status and one body (no oracle).

### Error wrapping
**Source:** `audit_store.go:77,84,89`; `store_identity.go:31,42`; `runner_delete.go:48`
**Apply to:** all Go
```go
	return nil, fmt.Errorf("audit activity for %s: %w", identityID, err)
	return 0, fmt.Errorf("delete lifecycle: owner gate: %w", err)
```
**Rule:** `%w` always, prefix = `"<operation>: "`, sentinels via `errors.Is`. **Never `%w` a raw
token into an error** (D-13: never logged).

### Attachment download headers
**Source:** `agui/assets_api.go:52-58` + `content_disposition.go:23`
**Apply to:** export, bundled-artifact download
```go
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", contentDisposition(asset.FileName))
	h.Set("Content-Length", strconv.FormatInt(asset.SizeBytes, 10))
```
`contentDisposition` is RFC-6266 + `url.PathEscape` injection-guarded + diacritic-folded and
**already tested** — never concat a filename.

### Consumer-declared interfaces (no reverse imports)
**Source:** `cron/handlers/identity_purge.go:20-27`; `agui/asset_service.go:11`; `agui/audit_api.go:34-39`
**Apply to:** every seam crossing a package boundary
```go
// IdentityPurger is the consumer-declared seam the purge handler drives (the SnippetSweeper
// pattern): the live *agui.Deprovisioner satisfies it via PurgeExpired, so this package does
// NOT import internal/agui (D-24, and it avoids the reverse-import cycle).
```
**Rule:** `handlers` declares `ShareExpirer`; `runner` declares its revoker seam; `agui` declares
`ShareService`. `internal/share` imports none of them. Wiring happens at the composition root.

### i18n
**Source:** `resources.compaction.ts`; imports `resources.ts:1-17`; spreads `:160`/`:437`
**Apply to:** every user-visible string
**Rule:** `t('share.<area>.<key>')`, keys in **both** en+it, own module (R-03), key-parity test.
The public page falls back to `Accept-Language` — never persist the owner's language into the
snapshot (RESEARCH §6).

### Test shape (the coverage-gate-safe one)
**Source:** `internal/agui/auth_capability_integration_test.go` (whole file)
**Apply to:** `share_api_test.go`, `share_cross_identity_test.go`, `store_integration_test.go`
```go
//go:build db_integration

// Integration test for the WEB-03 capability gate (D-04) against the REAL seeded
// `local` identity over a Postgres-backed identity.Store. ...
// Requires a running Postgres with the migrations applied:
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race -p 1 ./internal/agui -run TestAgentRunCapability -count=1
//
// No-skip-as-green: the shared envOrSkip (server_integration_test.go) t.Fatals under
// $CI when the DSN is unset — a skipped integration test must never pass green.
package agui
```
**Copy:** the **single** `//go:build db_integration` tag (R-07 — any extra tag ⇒ zero coverage);
the header's env + run-command + no-skip-as-green block; `pool := migratedPool(t)`;
`withPrincipal(httptest.NewRequest(...), identityID)` to inject a principal with no cookie/Authula;
`t.Cleanup` deleting seeded rows; `t.Run` subtests per case.

The 403 subtest (`:91-116`) is the direct template for SC4 rows 6/8 — note it seeds a fresh
**non-wildcard** identity, which is exactly R-13's requirement:
```go
	t.Run("identity without the grant -> 403", func(t *testing.T) {
		// Create a fresh identity with NO capability grants; its principal must be denied.
		other, err := pool.Exec(ctx,
			`INSERT INTO aura.identities (id, name, kind) VALUES (gen_random_uuid(), $1, 'user') RETURNING id`,
			"web03-capgate-"+t.Name())
		...
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, ungrantedID)
		})
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/agent/run", nil), ungrantedID)
		RequireCapability(next, deps, "agent.run").ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("ungranted POST /agent/run = %d, want 403", rec.Code)
		}
	})
```
⚠️ The seed uses **`name = "…"+t.Name()`** for uniqueness — reuse that so parallel runs don't collide
(and see the known `local`-identity re-seed footgun in project memory).

---

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/share/markdown.go` | utility | transform | **No Markdown serializer exists anywhere in the repo.** `grep` for a `Markdown()` writer returns nothing in the export sense. Follow RESEARCH OQ4's `func (Snapshot) Markdown() []byte` signature; use `strings.Builder` with `b.Grow()` (the only in-repo precedent for building a large string efficiently is `content_disposition.go:47-48`). Per-turn `##` headings + fenced code per D-07. |
| `web/src/chat/share/` modal internals (tier `<fieldset>` radio group, conditional warning, expiry chips, `idle→creating→shared→updating→revoking→revoked` state machine) | component | request-response | The **Dialog shell** has an exact analog (`DocumentUploadDialog.tsx`); these **internals** do not — no existing dialog has a radio group, a conditional warning, or a multi-state machine. RESEARCH §UI/UX 2 is the design source. The nearest in-repo primitives are `@/components/ui/label` + `@/components/ui/checkbox` (no radio-group primitive exists — **verify whether one must be added, or use native `<input type="radio">` inside a `<fieldset>`**, which RESEARCH §6 mandates for a11y anyway). |

**Partial-analog notes (usable, but do not copy blindly):**
- `internal/share/snapshot.go` / `redact.go` — the only allowlist projection in the repo is
  TypeScript (`sseAdapter.ts:353-361`). Port the *technique*; there is no Go template.
- `internal/share/jsonfmt.go` — `writeJSON` (`conversations_api.go:149`) is an HTTP-writer, not a
  `[]byte` marshaller. OQ4's `func (Snapshot) JSON() ([]byte, error)` is ~30 LOC of
  `json.Marshal`; no analog needed.
- `web/src/routes/SharePage.tsx` — `LoginPage` shares the "renders without a session" class but
  nothing else (it is a form, not a document renderer). The *renderers* it composes all have exact
  analogs (`HtmlPreview`, `artifactMeta.previewKind`, `PreviewModal`).

---

## Metadata

**Analog search scope:** `internal/agui/`, `internal/share/` (absent), `internal/objectstore/`,
`internal/conversations/`, `internal/runner/`, `internal/cron/handlers/`, `internal/llm/`,
`internal/db/migrations/`, `cmd/aura/`, `web/src/shell/`, `web/src/chat/`, `web/src/i18n/`,
`web/src/routes/`, `web/src/components/ui/`, `web/src/documents/`, `web/src/conversations/`

**Files opened for extraction (27):**
`web/src/shell/ArtifactsShell.tsx`, `web/src/shell/useArtifactsPanel.ts`,
`web/src/chat/artifacts/ArtifactsPanel.tsx`, `web/src/chat/artifacts/useThreadArtifacts.ts`,
`web/src/chat/artifacts/renderers/useAssetContent.ts`,
`web/src/chat/artifacts/renderers/HtmlPreview.tsx`, `web/src/chat/sseAdapter.ts`,
`web/src/chat/voice/voiceModeContext.ts`, `web/src/AppShell.tsx`, `web/src/main.tsx`,
`web/src/i18n/resources.ts`, `web/src/i18n/resources.compaction.ts`,
`web/src/documents/DocumentUploadDialog.tsx`, `web/src/conversations/DeleteConfirmDialog.tsx`,
`internal/agui/assets_api.go`, `internal/agui/asset_service.go`, `internal/agui/audit_store.go`,
`internal/agui/audit_api.go`, `internal/agui/auth.go`, `internal/agui/server.go`,
`internal/agui/server_redact.go`, `internal/agui/content_disposition.go`,
`internal/agui/password_reset.go`, `internal/agui/onboarding_session.go`,
`internal/agui/auth_capability_integration_test.go`, `internal/objectstore/types.go`,
`internal/objectstore/objectstore_test.go`, `internal/conversations/store_identity.go`,
`internal/runner/runner_delete.go`, `internal/cron/handlers/sweep.go`,
`internal/cron/handlers/identity_purge.go`, `internal/llm/client.go`,
`cmd/aura/serve_webui.go`, `cmd/aura/serve_webui_musr.go`, `cmd/aura/serve_webui_composer.go`,
`internal/db/migrations/0034_*.{up,down}.sql`, `internal/db/migrations/0035_*.{up,down}.sql`

**Verified at:** HEAD `1a3252e64`
**Pattern extraction date:** 2026-07-15
**Re-verify at execute time:** the migration slot (`ls internal/db/migrations/ | tail -1`), the
`scheduler_tasks_kind_check` member list, and the three LOC margins (593/591/576) — all three
projects have phases in flight.
