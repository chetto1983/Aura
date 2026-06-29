# Quality Audit — Slice C: Transport, Web/Auth, MCP, Channels, Setup, Skills, Multimodal

**Auditor:** Independent read-only quality audit (consolidated from parallel sub-audits of `agui`/`web`/`webauth`, `mcp`/`channels`/`setup`/`onboarding`, and `multimodal`/`skills`/`eval`/`settings`/`profile`/`assets`).
**Date:** 2026-06-29
**Scope:** `internal/agui`, `internal/web`, `internal/webauth`, `internal/mcp` (+`manager`), `internal/channels` (+`telegram`), `internal/setup`, `internal/onboarding`, `internal/multimodal`, `internal/rerank`, `internal/skills`, `internal/skilladapters`, `internal/settings`, `internal/profile`, `internal/assets`, `internal/eval`

---

## A. Slice Summary

**Overall health: GOOD.** Largest, most-surface-area slice; architecture is clean. **The PRD-mandated `agent ⇸ agui` import boundary holds** (zero `internal/agent → internal/agui` imports; `agui → agent` is the correct direction). **No production file exceeds the 600-LOC cap** (closest: `telegram/bot_dispatch.go` 532, `agui/server.go` 588). All routes/handlers/MCP probes/skill adapters are wired into the composition root. The debt is concentrated in **repeated small helpers copy-pasted across packages** and a few **dead exports / deferred placeholders**.

**Finding counts by severity:**

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 0 |
| Medium | 8 |
| Low | 9 |
| **Total** | **17** |

---

## B. Findings Table

### QA-C-01 — Duplication — Medium
**Confidence:** High. **Evidence:** `agui/governance_write_api.go:189` `decodeMCPBody`, `agui/governance_write_skills_api.go:218` `decodeSkillsBody`, `agui/bootstrap_api.go:69` `decodeBootstrapRequest`, `agui/password_reset_api.go:76` `decodePasswordResetRequest` — four structurally identical `MaxBytesReader + json.Decode → bool` helpers. **Why:** drift risk + ~24 LOC dup; also relates to F-052 (strict JSON decode) — a single hardened helper would close the quality + the security finding together. **Action:** extract `decodeBodyInto[T any](w,r,cap,dst)` with `DisallowUnknownFields` + single-EOF. **Effort:** S. **Regression risk:** Low.

### QA-C-02 — Duplication — Medium
**Confidence:** High. **Evidence:** env-helper copies — `config/config_env.go` (canonical) vs `channels/registry.go:162` `envChannelEnabled` vs `channels/telegram/config.go:56` `envIntDefault`. Three copies, self-documented as "local copy to avoid internal/config import". **Why:** behavior drift across copies. **Action:** extract `internal/envutil` (no deps) consumed by all three. **Effort:** S. **Regression risk:** Low. *(Cross-slice: Slice A flags the same env-read pattern in agent tools.)*

### QA-C-03 — Duplication — Medium
**Confidence:** High. **Evidence:** trust-normalization logic identical in `mcp/managed_config.go:220` `NormalizedTrust` and `mcp/manager/runtime.go:166` `normalizedTrustForServer`. **Why:** the v2.0.0 MCP-hardening phase (MCPH-01 canonical transport classifier) MUST unify this — divergence here is exactly the F-027 risk class. **Action:** single canonical classifier both delegate to. **Effort:** S–M. **Regression risk:** Medium (touches trust). **Wire to Phase 36.**

### QA-C-04 — Duplication — Medium
**Confidence:** High. **Evidence:** `eval/capture_cot_eval.go:153–229` re-implements ~80 LOC from `cmd/aura/chat_render.go:111–220` (`flushRemainder`, `isToolResultPreview`, `isTerminalToolCall`, `usageFromStateDelta`, `anyInt`, `anyFloat`) — self-labeled "duplicated by design". **Why:** if `chat_render.go` event-shaping changes, the eval scorer silently drifts → wrong scores. **Action:** extract shared `internal/agentrender`. **Effort:** M. **Regression risk:** Low (eval-only consumer + production).

### QA-C-05 — Duplication — Medium
**Confidence:** High. **Evidence:** `agui/onboarding_provision.go:219–232` (`Provision`) inlines the Telegram-link mint sequence instead of reusing the sibling `mintTelegramLink` (lines 384–406). **Why:** the provisioning saga and the profile-complete path mint links differently → drift in a cross-store saga (compounds onboarding-correctness concerns). **Action:** `Provision` delegates to `mintTelegramLink`. **Effort:** S. **Regression risk:** Low–Medium (saga path — test the compensation).

### QA-C-06 — Dead Code — Medium
**Confidence:** High. **Evidence:** `settings/settings.go:57–58` allowlists `AURA_MEMORY_EMBED_BASE_URL` / `AURA_MEMORY_EMBED_API_KEY`; no Go `os.Getenv`/config reads them (only `compose.yaml` for the sidecar). **Why:** operators can "set" cockpit settings that the Go daemon never consumes (they only reach the external agent-memory sidecar via Docker env) — silent no-op, confusing. **Action:** either annotate as sidecar-only in the allowlist metadata, or remove from the Go overlay. **Effort:** S. **Regression risk:** Low (verify the sidecar truly reads them from compose, not the overlay).

### QA-C-07 — Test Gap — Medium
**Confidence:** High. **Evidence:** `web/throttle.go` (per-host concurrency limiter, goroutine acquire/release) has no `throttle_test.go` — covered only indirectly. **Why:** concurrency primitives need direct cancellation/race tests. **Action:** add a focused unit test. **Effort:** S. **Regression risk:** None (test add).

### QA-C-08 — Test Gap — Medium
**Confidence:** High. **Evidence:** `setup/handlers.go:146` documents an `InvalidateToken`-before-`writeSSE` ordering fix (race on slow Windows CI) but has no regression test asserting the ordering; `telegram/profile_onboarding.go:362` `answersFromText` (Italian keyword fallback) is untested (only the LLM-extractor path is). **Why:** race-sensitive + fallback parsing both regress silently. **Action:** add ordering + keyword-fallback tests. **Effort:** S–M. **Regression risk:** None.

### QA-C-09 — Dead Code — Low
**Confidence:** High. **Evidence:** `assets/types.go:14,20,25` `StatusCreated`/`StatusEmbedding`/`StatusCanceled` — zero references outside declarations (first real state is `StatusPresigned`). **Action:** remove or wire the intended transitions. **Effort:** S. **Regression risk:** Low (confirm no JSON-string consumer).

### QA-C-10 — Dead Code — Low
**Confidence:** High. **Evidence:** `agui/governance_api.go:226` `indexByte` reimplements `strings.IndexByte`/`strings.Cut`; `agui/governance_api.go:199` `stringList` is a no-op defensive copy. **Action:** replace with stdlib `strings.Cut`; inline `append([]T{}, …)`. **Effort:** S. **Regression risk:** Low.

### QA-C-11 — Dead/Deferred — Low
**Confidence:** Medium. **Evidence:** `setup/qr.go:10` `qrSVG()` always returns `""` (`_ = deepLink`) — OQ4 placeholder; risk a future SVG wire-up shows an empty QR silently. **Action:** keep `// TODO(OQ4)` + add a guard/log if called in a context expecting non-empty. **Effort:** S. **Regression risk:** Low.

### QA-C-12 — Dead Code — Low
**Confidence:** Medium. **Evidence:** `channels/deps.go` blank `_ "gopkg.in/telebot.v4"` import is redundant — `telegram/bot.go` imports telebot genuinely; the anchor no longer pins anything. **Action:** remove the blank import (verify `go mod tidy` keeps the direct require via the real consumer). **Effort:** XS. **Regression risk:** Low.

### QA-C-13 — Duplication — Low
**Confidence:** High. **Evidence:** `truncateRunes` duplicated in `assets/context.go:133` and `rerank/client.go:167` (two 5-line copies in unrelated packages). **Action:** move to a shared string util (or accept at this scale). **Effort:** XS. **Regression risk:** Low.

### QA-C-14 — Antipattern — Low
**Confidence:** High. **Evidence:** `agui/assets_api.go:159` defines `principalIdentityID(r)` — a general auth helper used by 6+ handlers — buried in the asset file. **Action:** move to `auth.go`/`server_principal.go`. **Effort:** S. **Regression risk:** Low.

### QA-C-15 — Antipattern / LOC Watch — Low
**Confidence:** High. **Evidence:** `telegram/bot_dispatch.go` 532 LOC, `agui/server.go` 588 LOC — both near the 600 cap; one more handler tips them over. **Action:** pre-emptively migrate `bot_dispatch` document helpers → `documents.go`; `server.go` diag handlers → `server_diag.go`. **Effort:** S. **Regression risk:** Low.

### QA-C-16 — Test Gap — Low
**Confidence:** Medium. **Evidence:** `webauth/authula.go` `ensureAuthulaSearchPath` (DSN manipulation) tested only end-to-end, not in isolation (empty/malformed/already-has-search_path edge cases). **Why:** v2.0.0 makes Authula the DEFAULT (MUSR-06) — DSN edge cases become production-critical. **Action:** table-driven unit test. **Effort:** S. **Regression risk:** None. **Wire to Phase 34.**

### QA-C-17 — Antipattern — Low
**Confidence:** Medium. **Evidence:** `web/searxng.go:186` constructs a fresh `http.Client`+`Transport` per `searxGet` call (for goleak), and several MCP `Client` methods (`initialize`/`readResponse`/`roundtrip`) are undocumented test-only seams. **Action:** package-level client with `DisableKeepAlives`; mark test seams with a doc comment. **Effort:** S. **Regression risk:** Low.

---

## C. High-Confidence Quick Wins
1. **QA-C-10 / QA-C-12 / QA-C-13 (XS–S):** delete `indexByte`, `stringList`, redundant `telebot` blank import; fold `truncateRunes`. Pure deletions/stdlib swaps.
2. **QA-C-09 (S):** remove the 3 dead `assets.Status*` consts (after confirming no JSON consumer).
3. **QA-C-06 (S):** annotate or remove the sidecar-only `AURA_MEMORY_EMBED_*` settings keys.
4. **QA-C-01 (S):** unify the 4 `decode*Body` helpers — closes a quality dup AND advances F-052.
5. **QA-C-07 / QA-C-08 (S):** add the throttle + setup-ordering + keyword-fallback tests.

## D. Risky / Uncertain Findings
- **QA-C-03 (trust normalization):** do NOT casually merge — it is security-relevant; fold into the Phase-36 canonical-classifier work with full trust tests (F-027). What to verify: every call site's trust inference is identical before unifying.
- **QA-C-06 (dead settings keys):** verify whether the agent-memory sidecar reads these from the compose env independently of the Go overlay before removing — missing evidence: the sidecar's own env-consumption.
- **QA-C-12 (telebot blank import):** verify `go mod tidy` retains the direct require via `telegram/bot.go` before removing the anchor.

## Cross-Slice Flags
- **Env-helper duplication (QA-C-02)** mirrors Slice A's "uncatalogued `AURA_*` knobs read at call time" — a repo-wide `internal/envutil` + config catalog is the shared fix.
- **Trust normalization (QA-C-03)** is the quality face of audit finding **F-027** → Phase 36.
- **`decode*Body` (QA-C-01)** is the quality face of **F-052** → Phase 38.
- **Authula DSN test gap (QA-C-16)** → Phase 34 (Authula default cutover).
