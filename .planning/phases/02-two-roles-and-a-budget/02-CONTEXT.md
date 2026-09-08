# Phase 2: Two Roles and a Budget - Context

**Gathered:** 2026-09-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Two roles and one budget. An admin adds and removes users; a user cannot. Everything else
Aura enforces is available to every identity. What bounds a user is money, and the ceiling is
held by OpenRouter rather than by Aura's own accounting.

Requirements: RBAC-01..RBAC-11, CRED-01..CRED-09, REL-06.

Not this phase: the adversarial suite and sandbox escape battery (Phase 3), load / chaos /
observability (Phase 4), restart / rollback / restore (Phase 5). Not at all, in this milestone:
per-capability gating of MCP mounting, skill authoring, sandbox shell and destructive
approval — the original Phase 2 design added five capabilities and enforced each at its call
site, and that design is deliberately overturned here.

</domain>

<decisions>
## Implementation Decisions

### The permission model

- **D-01:** Exactly two capabilities are administrative — `identity.create` (exists) and
  `identity.delete` (new). Every other capability Aura enforces (`agent.run`,
  `governance.read`, `governance.write`, `share.public`) is granted to each identity at
  provisioning. Rationale: isolation already confines an identity to its own perimeter, so a
  permission every identity holds is not a permission. The alternative considered and
  rejected was removing the non-administrative gates outright; keeping them and granting them
  by default preserves the audit trail of a refusal and makes a future narrowing one grant
  fewer rather than new code at every mount in `cmd/aura/serve_webui.go`.
  — **Reversibility:** reversible — tightening later removes a grant from the provisioning
  step, it does not restore deleted gates.

- **D-02:** The administrative capabilities are never grantable through the API.
  `POST` and `DELETE /api/admin/identities/{id}/capabilities` refuse `identity.create` and
  `identity.delete` for every caller, so admin is bootstrap-only. This replaces a
  no-self-escalation rule with the absence of any path at all.
  — **Reversibility:** reversible.

- **D-03:** The last administrative identity cannot remove or deactivate itself. Since admin
  is bootstrap-only there is exactly one, so the refusal is unconditional rather than a count.
  Known consequence, accepted: if the bootstrap identity is lost, user management is reachable
  only from `aura identity` on the host. That CLI is the break-glass and is documented as such.
  — **Reversibility:** reversible.

- **D-04:** The `*` wildcard is retired as RBAC-01 states. It cannot survive D-01, because a
  wildcard grants `identity.delete` to the bootstrap operator before anyone grants it — the
  same fault `0099` refused to inherit and `0026` prepared for.
  — **Reversibility:** one-way — a migration rewrites existing rows.

### Removing a user

- **D-05:** Removal runs the existing reverse saga, `internal/agui/deprovision.go`. It is
  already written, journaled on `aura.provisioning_saga` with `kind='deprovision'`, idempotent,
  and symmetric with the provisioning saga across sessions, jobs, sandbox, conversations,
  ArcadeDB memory, Garage bucket, per-identity directories, the Postgres row and the Authula
  user. What does not exist is any HTTP route or cockpit control for it: today it is reachable
  only from `aura identity deactivate|purge` and a cron grace sweep.
  — **Reversibility:** reversible — the phase adds a caller, not a saga.

- **D-06:** The cockpit's removal is destructive: it runs deactivate and purge in sequence
  behind a typed confirmation, rather than handing the identity to the 7-day grace window. The
  grace-window sweep stays for the CLI path.
  — **Reversibility:** one-way per invocation — the data is gone when it completes.

### The budget

- **D-07:** A user's credit is a per-identity OpenRouter key with a provider-enforced cap,
  minted through the Provisioning API — not a balance table in Postgres. This removes the whole
  LibreChat shape that was the starting point: no credits ledger to keep aligned with the
  provider, no pre-flight estimator multiplying tokens by a rate table, no post-turn debit, no
  auto-refill machinery (`limit_reset` is a field), no per-model multiplier table.
  — **Reversibility:** costly — it makes a control the product depends on rest on a provider API.

- **D-08:** Enforcement, balance and reconciliation are three different jobs taken from three
  different places. Enforcement is OpenRouter's `limit`, which holds even when Aura's
  accounting is wrong. The displayed balance and the pre-flight refusal come from Aura's own
  in-band ledger, because the provider's counter lags (M-07). Reconciliation is
  `GET /api/v1/keys` and `POST /api/v1/analytics/query`.
  — **Reversibility:** reversible.

- **D-09:** A new identity is provisioned at a zero cap and can spend nothing until an admin
  assigns credit. Fail-closed, with the ~25s unblock latency of M-06 surfaced in the UI.
  — **Reversibility:** reversible.

- **D-10:** The admin chooses each identity's reset interval — OpenRouter's `limit_reset`
  accepts `daily`, `weekly`, `monthly` — rather than the deployment fixing one.
  — **Reversibility:** reversible.

- **D-11:** No fallback to the deployment key. An identity whose own key is missing is refused
  on the OpenRouter path rather than billed to the operator, or the cap means nothing.
  — **Reversibility:** reversible.

- **D-12:** The per-identity key is stored on the pattern `internal/mcpoauth/store.go` already
  establishes: AES-256-GCM, KEK derived from `AURA_AUTHULA_SECRET`, RLS, `ON DELETE CASCADE`.
  The management credential is deployment-scoped, not per identity, and belongs beside the
  existing OpenRouter key rather than on an identity row.
  — **Reversibility:** one-way — a migration.

- **D-13:** Credits apply to OpenRouter only. A local llama.cpp or Ollama backend bills
  nothing, has no key to cap, and the cockpit says so instead of presenting a zero balance —
  the rule `internal/llm/spend.go` already applies with `ErrSpendNotApplicable`.
  — **Reversibility:** reversible.

</decisions>

<measurements>
## Measured live against the OpenRouter account, 2026-09-08

- **M-01:** A management key authenticates `/api/v1/keys`, `/api/v1/analytics/*`,
  `/api/v1/workspaces` and `/api/v1/credits`, and the documentation states it cannot call the
  completion endpoints. A leaked management key therefore mints keys but cannot burn credit.
- **M-02:** `POST /api/v1/keys {name, limit, limit_reset}` returns 201 with the raw key
  **once** plus a `hash`, and the record carries `limit`, `limit_remaining`, `limit_reset`,
  `usage`, `usage_daily|weekly|monthly`, `byok_usage*`, `disabled`, `expires_at`,
  `external_user`, `workspace_id`.
- **M-03:** A key created at `limit: 1.0` served inference on the first call — no warm-up.
- **M-04:** A key at `limit: 0` is refused **HTTP 403** `Key limit exceeded (monthly limit)`.
  Enforcement is provider-side and real. The code is 403, not the 402 one would guess.
- **M-05:** `PATCH limit` down to 0 denies within **5s**.
- **M-06:** `PATCH limit` up to 1.0 on an exhausted key took **25s** to unblock (403 at t+18s,
  200 at t+25s). A top-up is not immediate and the cockpit must say so.
- **M-07:** `GET /api/v1/key` lags a spend by **30–40s** (`usage: 0` at t+30s, correct at
  t+40s). It is reconciliation, never the live balance.
- **M-08:** `cost` is present in `usage` on **every** response, streaming included, with and
  without `usage:{include:true}` — and the documentation confirms that parameter is deprecated
  and inert. `internal/llm/openai_compat/response.go:112` already reads it opportunistically,
  so no request change is needed.
- **M-09:** Four calls at an in-band `0.000004158` each summed to a provider-reported
  `0.000016632` — Aura's own ledger and the provider agree exactly.
- **M-10:** `DELETE /api/v1/keys/{hash}` kills inference within **5s** (401) and
  `GET /api/v1/keys/{hash}` then returns 404, so the revocation is verifiable.
- **M-11:** `POST /api/v1/analytics/query` exposes **38 metrics over 16 dimensions**, among
  them `api_key_id`, `user`, `external_user` and `session_id`, at granularities from minute to
  month. One call returned per-key spend, requests, tokens, cache hit rate and p50 latency —
  so per-identity analytics need nothing of our own.
- **M-12:** `GET /api/v1/credits` reported `total_credits: 90`, `total_usage: 74.08`. Per-key
  caps allocate from that single shared pool and their sum may exceed it; OpenRouter does not
  prevent over-allocation. The two existing keys carry `limit: null`, i.e. uncapped.
- **M-13:** `GET /api/v1/keys` returns the whole roster with each key's cap, remaining and
  lifetime/daily/weekly/monthly spend in one call.
- **M-14:** Available and deliberately not taken in this phase: workspaces with per-interval
  budgets (`PUT /workspaces/{id}/budgets/{daily|weekly|monthly|lifetime}`, limits must strictly
  decrease as the interval narrows) and guardrails assignable per key.

### What the measurements do NOT show

- Whether `limit_reset` actually rolls over at a period boundary — not observed.
- Whether `/activity`'s `api_key_hash` parameter filters or is ignored; it returned an empty
  set for a key created the same day, which the endpoint's own "last 30 completed UTC days"
  rule also explains.
- How Aura's agent loop behaves when a provider 403 arrives mid-turn.
- Deleted keys keep their consumption in OpenRouter's analytics. Identity deletion is
  therefore complete on our planes and incomplete on the provider's, and no API observed here
  changes that.

</measurements>

<constraints>
## Constraints from the codebase

- **C-01:** The LLM credential is process-global today. `llm.Load()` resolves
  `OPENROUTER_API_KEY` once at boot and `chat.cfg.LLM` reaches the agent at construction
  (`internal/agent/llm_agent_construct.go:36`); `aura.settings` (migration `0024`) is keyed by
  `key` alone with no `identity_id`. No per-identity credential resolution exists. Threading
  one into the turn is the largest piece of work in this phase and it touches the hottest path.
- **C-02:** `OPENROUTER_API_KEY` is absent from `.env` — the inference key already lives in
  `aura.settings` as an `is_secret` row, which is the precedent for storing the management
  credential the same way.
- **C-03:** The ledger already exists. `internal/agent/turn_usage.go` sums prompt, completion,
  cached and cost across every call a turn makes; `aura.cache_metrics` (migration `0007`) is an
  append-only per-turn row carrying `prompt_tokens`, `cached_tokens` and `cost_usd`, scoped per
  conversation and therefore per identity by join; `aura.conversations.total_cost_usd` holds the
  running per-conversation total.
- **C-04:** Migration numbering is assigned at landing. Run `ls internal/db/migrations/ | tail -1`
  and take the next integer — do not deduce it from this document.
- **C-05:** No non-test Go file may exceed 600 LOC. `internal/agui/onboarding_provision.go` is
  at 568 and gains a key-minting leg, so it must be split rather than grown.

</constraints>
