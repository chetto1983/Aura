---
phase: 9
slug: swarm-minimal
status: secured
threats_open: 0
asvs_level: 1
created: 2026-06-04
---

# SECURITY.md — Phase 9: Swarm (Minimal)

**Phase:** 09-swarm-minimal — CAP-03 swarm coordinator + first production MCP mount (mail/WhatsApp)
**Audited:** 2026-06-04
**ASVS Level:** 1 (default)
**Block-on:** critical (default)
**Auditor:** gsd-security-auditor
**Verdict:** SECURED — 18/18 threats resolved (16 mitigations verified in code, 2 accepted risks documented)

The threat register was authored across the six 09-0X PLAN files at plan time. Each
threat below was verified against the implemented code (grep + file:line + live test
run), not against documentation or intent. Implementation files were not modified.

---

## Threat Verification Summary

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-09-01 | Tampering (PRD drift) | mitigate | CLOSED | PRD-first gate held: amendment commit `2c05fbdf` (11:50) precedes first code commit `26e980c4` (12:11) |
| T-09-02 | DoS (fan-out budget) | mitigate | CLOSED | `internal/swarm/swarm.go` preflight (budget 90-94, goals-cap 84-88, waves 58-64, timeout 118-119) + `Budget.Child` shared steps ptr (`budget.go:287`); TestSwarmBudgetPreflight/TestSwarmBudgetInheritance PASS |
| T-09-03 | DoS (goroutine leak) | mitigate | CLOSED | `swarm.go` errgroup.WithContext:105, defer cancel:107, #61611 guard:115-117, per-child WithTimeout+defer:118-119; goleak TestMain (`main_test.go:13`) + per-test VerifyNone; TestSwarmProperties (rapid 1..8) PASS |
| T-09-04 | Tampering (path traversal) | mitigate | CLOSED | Flat SessionID `swarm.go:142` (no slash); transcript path `report.go:50,63` filepath.Join under RunDir; gosec G304 nolint justified; TestSwarmTranscript PASS |
| T-09-05 | DoS (sibling cancel) | mitigate | CLOSED | Child error → report slot + goroutine returns NIL (`swarm.go:121`), egCtx never cancels siblings; TestSwarmFailureIsolation PASS |
| T-09-06 | Tampering/EoP (destructive MCP tools) | mitigate | CLOSED | Allowlist drop-before-adapt `bridge.go:85-100`; resolver `main.go:150-159` scopes mail/whatsapp to safe tools (delete_mailbox/move_message/create_mailbox absent); TestMountAllowlistDeferred PASS |
| T-09-07 | DoS (dead MCP aborts boot) | mitigate | CLOSED | Fail-soft `main.go:139-140` slog.Warn+continue, no abort; MCPServersErr fatal path preserved `main.go:116-117`; TestBuildRegistryFailSoft PASS |
| T-09-08 | Spoofing (tool name shadows built-in) | mitigate | CLOSED | 64-byte cap + collision-hash + refuse-clobber `bridge.go:158-176` (`maxToolNameLen`, `hashSuffix`, `reg.Get` clobber refusal) — inherited from 8.1, unchanged by the Deferred flip |
| T-09-09 | Info disclosure (manifest bloat) | mitigate | CLOSED | `Deferred: true` flip `bridge.go:118`; tool_search discovers on demand; TestMountAllowlistDeferred asserts every bridged Spec Deferred |
| T-09-10 | Tampering (malformed proxied id) | mitigate | CLOSED (alternative control) | See note below — declared parseUUID control was deliberately removed (CR-01/migration 0008); residual tampering surface structurally closed by parameterized binding + opaque metadata column |
| T-09-11 | Info disclosure (proxied id in prompt) | accept | CLOSED | ask_user Description forbids secrets `ask_user.go:113-115`; proxied id is opaque dispatch metadata, model-discretionary, not user-facing — accepted risk logged below |
| T-09-12 | EoP (worker self-spawn) | mitigate | CLOSED | Worker registry = `Without(parentRegistry, "swarm_spawn")` per child (`swarm.go:139`); depth guard backstop (`swarm_depth.go:33`); TestRunnerAdapterWorkerRegistryExcludesSwarmSpawn PASS |
| T-09-13 | DoS (over-spawn) | mitigate | CLOSED | D-13 cap in tool (`swarm_spawn.go:91-95`) + engine preflight (`swarm.go:84-88`); D-24 anti-over-spawn literal (`swarm_spawn.go:28-35`, 4 phrases); TestSwarmSpawnGoalsCap/DescriptionLiteral PASS |
| T-09-14 | Tampering (import cycle) | mitigate | CLOSED | `swarm_spawn.go` imports only context/encoding/json/fmt (no agent/swarm); ctx seam `swarm_context.go` private key + `llm_agent.go:297` injection; `go build ./...` exits 0 |
| T-09-15 | Spoofing (non-self send) | mitigate | CLOSED | E2E scenario sends only to selfMail/selfPhone (`dataset_cot_eval.go:209-237`); D-20 allowlist restricts to send_email/send_message |
| T-09-16 | Info disclosure (creds leak) | mitigate | CLOSED | Release-blocking `dimSecretRedaction` dimension `scoring_cot_eval.go:256-258` (t.Fatalf on any leak); creds in managed-config Env only |
| T-09-17 | Tampering (report prompt injection) | accept | CLOSED | Workers are headless executors, parent reads report as data (worker-overlay `brief.go:10-13`); v1 single-user, sanitization deferred to v2 — accepted risk logged below |
| T-09-SC | Tampering (supply chain) | mitigate+accept | CLOSED | Zero go.mod/go.sum changes across phase 9 (`git diff 2c05fbdf~1..HEAD -- go.mod go.sum` empty); mail-mcp + whatsapp fork are out-of-process recipes, fork is user's own (chetto1983) |

---

## Note on T-09-10 — declared control removed, residual surface verified closed

The plan-time mitigation declared the proxied child id is validated by the `parseUUID`
boundary helper ("invalid uuid rejected at the domain boundary, never reaching the
sqlc layer as a raw string").

During the 09 review cycle (CR-01, commit `2e7a702d`, migration
`0008_proxied_child_id_text`) the column was deliberately retyped uuid→text because
the swarm report's child id is the FLAT worker id (`w1`..`wN`), which is not a uuid —
the original uuid column made the documented happy path fail `parseUUID` and abort the
whole pause write. The value is now stored verbatim as `pgtype.Text`
(`internal/askuser/store.go:118-120`) with **no parseUUID validation**.

The declared control is therefore ABSENT by design. The underlying tampering concern
was independently verified to be structurally closed:

- `proxied_from_child_id` is only ever bound as a parameterized value (`$9`) in
  `InsertPausedState` and read back as a SELECT column (`internal/db/queries/paused_states.sql:1-12`)
  — no SQL injection (pgx parameterized binding).
- The value is never used in path construction, a WHERE predicate, or any other unsafe
  sink (full-repo grep of the 15 referencing files confirms insert + read-back only).
- `proxied_tool_call_id` was always plain text (the register itself noted "no traversal
  risk — it is not a path").

Verdict: CLOSED via alternative control. The plan-time control was superseded by a
documented review fix; the residual attack surface (Tampering: malformed value reaching
the DB) is moot under parameterized binding + opaque-metadata semantics. No action
required.

---

## Accepted Risks Log

### AR-09-11 (T-09-11) — proxied tool_call id surfaced in a relayed pause prompt
**Disposition:** accept
**Rationale:** When the parent relays a child's `needs_user_input` report via its own
`ask_user`, it may carry `proxied_tool_call_id` — an opaque dispatch id, not a secret.
The ask_user Description (`internal/agent/tools/ask_user.go:113-115`) explicitly forbids
secrets in the prompt ("NEVER use ask_user to collect passwords, API keys, tokens, or
payment credentials"). The proxied ids are model-discretionary metadata, not user-facing
content. Single-user v1 target. Accepted.

### AR-09-17 (T-09-17) — child report prompt injection into parent aggregation
**Disposition:** accept
**Rationale:** v1 workers are headless executors producing text the parent reads as data
(worker-overlay literal `internal/swarm/brief.go:10-13`). Full report-sanitization (no
surveyed v1 system sanitizes sibling reports) is a v2 concern. The parent's own
guardrails apply. Documented low-value single-user target. Accepted.

---

## Unregistered Flags

None. No `## Threat Flags` section was present in any of the six 09-0X SUMMARY.md files;
the executor declared no new attack surface during implementation. No unmapped flags to
log.

---

## Build & Test Evidence

- `go build ./...` — exits 0 (no import cycle; T-09-14)
- `go test ./internal/swarm/ ./internal/agent/tools/` (security subset) — PASS
- `go test ./internal/agent/mcptools/ ./cmd/aura/` (allowlist + fail-soft + Validate) — PASS
- `git diff 2c05fbdf~1..HEAD -- go.mod go.sum` — empty (T-09-SC: no new dependency)
- PRD-first gate: amendment `2c05fbdf` precedes first code commit `26e980c4` (T-09-01)

The SC#5 live cot_eval E2E (operator-run, OPENROUTER-gated) is out of scope for this
static audit; it is the one legitimate non-CI tier and was separately attested PASS in
09-VERIFICATION.md (live run 2026-06-04, commit `93f261d5`). Its security-relevant
controls (T-09-15 self-only send, T-09-16 secret_redaction) are verified statically above.

---

## Security Audit 2026-06-04

| Metric | Count |
|--------|-------|
| Threats found | 18 |
| Closed | 18 |
| Open | 0 |

Mode: State B (run from PLAN artifacts, register authored at plan time). Auditor: gsd-security-auditor (opus). One control substitution surfaced (T-09-10: parseUUID → parameterized binding, see note above).
