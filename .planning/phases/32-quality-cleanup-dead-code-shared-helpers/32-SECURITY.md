---
phase: 32
slug: quality-cleanup-dead-code-shared-helpers
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on (high) severity
threats_open: 0
asvs_level: 1
created: 2026-06-30
---

# Phase 32 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register authored at plan time (all 10 plans carry a `<threat_model>` block); verified
> retroactively at ASVS L1 (grep-depth) against `32-VERIFICATION.md`, the 10 `*-SUMMARY.md`
> threat dispositions, and the live `web/e2e/phase32-uat.spec.ts` run. Phase 32 is a
> behaviour-preserving cleanup (dead-code removal + shared-helper extraction); no new endpoint,
> auth path, file access, or schema was introduced.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| client ↔ agui governance API | `NetworkAllowlist` field re-marshalled; serialized shape must not silently change | JSON (`[]` vs `null`) |
| cockpit ↔ daemon settings overlay | Removed `AURA_MEMORY_EMBED_*` keys were a silent no-op overlay control | operator settings keys |
| daemon ↔ agent-memory sidecar | Sidecar still reads `AURA_MEMORY_EMBED_*` from compose/.env at container start (unchanged) | config + one secret |
| browser ↔ cockpit API (`getJSON`) | Shared HTTP read helper deduped to the canonical twin; error/parse behaviour must be canonical | HTTP responses |
| user ↔ focus management (a11y) | Canonical focus trap governs keyboard nav; the inline copies were an a11y defect being fixed | keyboard focus |
| agent ↔ LLM provider | Retry classification refactor; misclassifying non-retryable as retryable would change request behaviour; stream-path deadline semantics deliberate | request retries |
| client ↔ web throttle / setup SSE | Per-host concurrency limit + setup token-invalidation race are correctness surfaces pinned by tests (no production change) | concurrency / session token |
| CI green-signal integrity | `memory_integration` leg must actually run (no skip-as-green) for the signal to be honest — verified, not changed | CI verdict |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-32-01-A1 | Tampering | DELETE of a constant a wire/DB consumer expects | medium | mitigate | D-04 confirmation set (deadcode + knip + repo-wide `rg` incl. `_test.go`) + literal-string grep before removal (`32-VERIFICATION` SC-1a/SC-1e) | closed |
| T-32-02-AL | Tampering/Info | agui `NetworkAllowlist` marshaling | medium | mitigate | non-nil `append([]string{},…)` + `TestGovernanceMCPEmptyAllowlistIsArrayNotNull` asserts `[]` not `null` | closed |
| T-32-02-DEF | Deferred sec work | MCP trust-norm / `decode*Body` in agui | n/a | accept | Out of scope — deferred to a later phase; code untouched, prohibition honored (R-2) | closed |
| T-32-03-PIN | Tampering | `go.mod` telebot pin | low | mitigate | `go mod tidy` + grep confirm the pin stays DIRECT (SC-1c) | closed |
| T-32-03-REQ | Tampering | LLM request shape (Build restructure) | medium | mitigate | branch-parity test asserts byte-identical chosen request per branch | closed |
| T-32-04-SEC | Information Disclosure | `AURA_MEMORY_EMBED_API_KEY` (Secret) removal | medium | mitigate | value not logged on removal; `secret.IsSecretEnvKey` redaction untouched | closed |
| T-32-04-CFG | Tampering | sidecar config continuity | low | mitigate | compose/.env variable names kept; ownership documented | closed |
| T-32-04-DIST | Tampering | `internal/webui/dist` freshness | low | mitigate | dist rebuilt + committed in the same commit so served bytes match source | closed |
| T-32-05-PAR | Tampering | shared-helper behaviour drift on merge | medium | mitigate | test-first parity tables (union of inputs) GREEN vs old copies before the move (SC-2a–2f) | closed |
| T-32-05-NUM | Tampering | `numeric(10,4)` scale/round | medium | mitigate | assert `pgtype.Numeric(Int,Exp)` + err-presence (not string); round-trip ≤1e-4 | closed |
| T-32-05-CYC | Tampering | import cycle / agui back-edge | low | mitigate | `neostore`/`pgnumeric`/`envutil` are leaves; no agui import | closed |
| T-32-05-HASH | Crypto note | `neostore.HashText` sha256 | low | accept | content-MERGE key, not a credential — no crypto-strength requirement (R-3) | closed |
| T-32-06-STR | Tampering | `retryableStreamOpenError` (stream retry contract) | medium | mitigate | golden table captured before refactor; strict byte-identical output; `context.*→false` guard kept FIRST | closed |
| T-32-06-TOOL | Tampering | `isTransientToolErr` widening | medium | mitigate | characterize OLD then assert NEW widened set; intentional change documented (SC-2g) | closed |
| T-32-06-CYC | Tampering | canonicaljson edge | low | mitigate | both call sites already import canonicaljson — no new edge | closed |
| T-32-07-BND | Tampering | agent ↔ agui import boundary | medium | mitigate | `go list -deps ./internal/agentrender/` asserts 0 `internal/agui` (SC-2i) | closed |
| T-32-07-FIX | Tampering | eval token counting (`json.Number`) | low | mitigate | adopt chat_render superset; parity test documents the OLD `json.Number→0` as fixed | closed |
| T-32-07-PAR | Tampering | chat_render output drift | medium | mitigate | parity table GREEN vs old copies before the move (SC-2h) | closed |
| T-32-08-A11Y | Repudiation/Usability | inline focus traps | medium | mitigate | canonical `focusTrap` adopted (fixes disabled-filter + button-only bugs); consumer tests + `phase32-uat.spec.ts` assert Tab/Shift+Tab cycle, wrap, trap, Escape (SC-2k) | closed |
| T-32-08-DIST | Tampering | `internal/webui/dist` freshness | low | mitigate | dist rebuilt + committed per web commit | closed |
| T-32-08-VIS | Tampering | skeleton visual regression | low | mitigate | `phase32-uat.spec.ts` asserts `.skeleton-block` CSS-wave + 0 `.animate-pulse` on the 3 migrated views (SC-2l) | closed |
| T-32-09-DOS | Denial of Service | `web/throttle` per-host limit | medium | mitigate | test asserts acquire blocks at `perHostLimit` and ctx-cancel frees no extra token (SC-3a) | closed |
| T-32-09-RACE | Race correctness | setup `InvalidateToken`/SSE ordering | medium | mitigate | regression test pins invalidate-before-first-write order (SC-3b) | closed |
| T-32-09-DSN | Tampering | Authula `search_path` DSN | low | mitigate | table test covers malformed/idempotent/append of the pure parser (SC-3c) | closed |
| T-32-10-SKIP | Repudiation | `memory_integration` CI leg (false-green risk) | medium | mitigate | verified `CI:"true"` + `t.Fatal`-under-`$CI` + the live run (SC-3f) | closed |
| T-32-10-UTF | Tampering | `truncateTailBytes` UTF-8 safety | low | mitigate | boundary table asserts mid-rune walk-back yields valid UTF-8 (SC-3e) | closed |
| T-32-SC | Tampering (supply chain) | npm/pip/cargo installs | n/a | accept | no package installs this phase — cleanup-only, nothing to audit (R-1) | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above `high` count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

**Open threats at or above `high`: 0.** No mitigate-disposition threat exceeds `medium`; all are closed with the evidence above.

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| R-1 | T-32-SC | Cleanup-only phase performs no package installs; there is no supply-chain surface to audit | operator (davide) | 2026-06-30 |
| R-2 | T-32-02-DEF | MCP trust-norm / `decode*Body` hardening is explicitly deferred to a later phase; the code was left untouched and flagged do-not-merge | operator (davide) | 2026-06-30 |
| R-3 | T-32-05-HASH | `neostore.HashText` sha256 is a content-merge/identity key, not a password or credential; no crypto-strength requirement applies | operator (davide) | 2026-06-30 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-30 | 27 | 27 | 0 | Claude (gsd-secure-phase, ASVS L1 short-circuit — register authored at plan time, threats_open 0; mitigations cross-checked against 32-VERIFICATION.md, the 10 SUMMARY threat dispositions, and live `web/e2e/phase32-uat.spec.ts`) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-30
