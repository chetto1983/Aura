---
phase: 40
slug: security-supply-chain-pack
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: validated
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-22
---

# Phase 40 — Validation Strategy

> Per-phase validation *contract* for feedback sampling during execution — what WILL be measured, not a
> results report. Transcribed from 40-RESEARCH.md's "Validation Architecture" and keyed to the real task
> IDs + `<verify><automated>` commands of the nine plans (40-01..40-09). No coverage numbers are asserted
> here; the owned-surface ≥85% floor is enforced by `scripts/coverage_gate.sh` at phase close.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go stdlib `testing` + `go.uber.org/goleak`, already a direct dep, `go.mod:38`); plus the `cot_eval` build-tag LLM tier (manual, paid) and the shell CI gate scripts (`scripts/workflow_pin_gate.sh`, `scripts/coverage_gate.sh`) |
| **Config file** | none — `go test` with build tags (`db_integration`, `neo4j_integration`, `cot_eval`), per the project-wide CLAUDE.md convention |
| **Quick run command** | `go test ./internal/gateway/ ./internal/agui/ ./internal/conversations/ ./internal/reasoningtrace/ ./internal/secret/ ./internal/config/ ./internal/tracesink/ ./cmd/aura/` (untagged unit tier) |
| **Full suite command** | `bash scripts/coverage_gate.sh` (owned-surface ≥85% floor across the tag matrix, run in WSL per CLAUDE.md's standing environment rule) |
| **Estimated runtime** | quick unit tier <30s per package; full `coverage_gate.sh` matrix ~several minutes (WSL, stack up) |

---

## Sampling Rate

- **After every task commit:** Run the untagged unit tier for the touched package(s) — e.g. `go test ./internal/gateway/ -run TestInjectionSuite -count=1` (seconds).
- **After every plan wave:** waves touching persistence (Wave 2 SEC-01, Wave 3 SEC-01/SEC-09) run the full tagged matrix `go test -race -tags 'db_integration neo4j_integration' -count=1 -p 1 ./internal/...` in WSL; Wave 1 (no persistence) the untagged package tier suffices.
- **Before `/gsd-verify-work`:** `bash scripts/coverage_gate.sh` full suite green — verify the **`db_integration`-only Skills-job** number specifically (the stricter of the two coverage gates, per CLAUDE.md).
- **Max feedback latency:** 30 seconds (the per-task quick unit tier; every plan's acceptance criteria target <30s with no skip).

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 40-01-01 | 01 | 1 | SEC-09 | T-40-01-STALE / T-40-01-OVERCLAIM | REQUIREMENTS/ROADMAP/PRD SEC-09 amended to keyed-hash-or-documented-FP before any code (D-08) | docs/grep | `grep -q "keyed hash" .planning/REQUIREMENTS.md` | ✅ | ✅ passed |
| 40-01-02 | 01 | 1 | SEC-01 | T-40-01-OVERCLAIM | SEC-01 scoped to configured-secrets-at-rest, unknown secrets outbound-only (D-10b) | docs/grep | `grep -qi "configured secrets" .planning/REQUIREMENTS.md` | ✅ | ✅ passed |
| 40-01-03 | 01 | 1 | SEC-02 | T-40-01-STALE | SEC-02 amended to same-origin-only, PRD CORS text superseded (D-14b) | docs/grep | `grep -qi "same-origin only" .planning/REQUIREMENTS.md` | ✅ | ✅ passed |
| 40-02-01 | 02 | 1 | SEC-04 | T-40-02-EOP / T-40-02-OVERBLOCK | Injected shell/file/mutating-MCP calls denied under `server_production`; Allow negative-controls pass (D-01/D-03) | unit | `go test ./internal/gateway/ -run TestInjectionSuite -count=1` | ❌ W0 | ⬜ pending |
| 40-02-02 | 02 | 1 | SEC-04 | T-40-02-FALSECLAIM | LLM injection-resistance eval tier builds under `cot_eval`, never CI-wired (D-04) | build + manual | `go build -tags cot_eval ./internal/eval/ && go vet -tags cot_eval ./internal/eval/` | ❌ W0 | ⬜ pending |
| 40-03-01 | 03 | 1 | SEC-06 | T-40-03-SMUGGLE / T-40-03-UNKNOWN | `strictDecodeJSON` rejects trailing-JSON / unknown-field / oversize / wrong content-type; per-route `allowEmpty` (D-16/D-16b) | unit | `go test ./internal/agui/ -run TestStrictDecodeJSON -count=1` | ✅ | ✅ passed |
| 40-03-02 | 03 | 1 | SEC-06 | T-40-03-SMUGGLE | Privileged routes, including scheduler edit, reject trailing JSON and unknown fields | unit (httptest) | `go test ./internal/agui/ -run 'Test(PrivilegedRouteDecoders|GovernanceRouteDecoders)' -count=1` | ✅ | ✅ passed |
| 40-04-01 | 04 | 1 | SEC-03 | T-40-04-EXPOSE | Console refuses non-loopback bind unless `--unsafe-non-loopback` AND `AURA_INTEGRATIONS_CONSOLE_TOKEN` (D-15) | unit | `go test ./cmd/aura/ -run TestConsoleBindGuard -count=1` | ✅ | ✅ passed |
| 40-04-02 | 04 | 1 | SEC-03 | T-40-04-NOAUTH / T-40-04-SILENT | Unsafe mode requires token per request (401 without); loopback stays unauthenticated + warns (D-15) | unit (httptest) | `go test ./cmd/aura/ -run TestConsole -count=1` | ✅ | ✅ passed |
| 40-05-01 | 05 | 1 | SEC-05 | T-40-05-RETAG / T-40-05-CODEQLSPLIT | Every `uses:` SHA-pinned + `# vX.Y.Z`; no `@latest`; codeql init/analyze same SHA (D-20) | ci/grep | `! rg -n "uses:\s*\S+@v[0-9]" .github/workflows/` | ✅ | ⬜ pending |
| 40-05-02 | 05 | 1 | SEC-05 | T-40-05-SBOMGAP | goreleaser `sboms:` archive + source; pinned `download-syft` before goreleaser (D-17) | config/grep | `rg -n "sboms:" .goreleaser.yaml && rg -n "artifacts: source" .goreleaser.yaml` | ✅ | ⬜ pending |
| 40-05-03 | 05 | 1 | SEC-05 | T-40-05-RETAG / T-40-05-SC | `workflow_pin_gate.sh` blocks unpinned/@latest (multi-segment + `run:|` aware, no goreleaser `version:` false-flag); self-tested (D-19) | shell self-test | `bash scripts/workflow_pin_gate_test.sh && bash scripts/workflow_pin_gate.sh` | ❌ W0 | ⬜ pending |
| 40-06-01 | 06 | 2 | SEC-01 | T-40-06-EXFIL / T-40-06-CORRUPT | Inbound exact-match redactor redacts configured secrets; agent-discovered survive; length floor (D-10) | unit | `go test ./internal/secret/ -run TestRedactExact -count=1` | ❌ W0 | ⬜ pending |
| 40-06-02 | 06 | 2 | SEC-01 | T-40-06-SIDECAR / T-40-06-SPILLBYPASS / T-40-06-CAPDRIFT | No verbatim configured secret in turn body, spill, or `.result` sidecar; `:123` guard mirrored (D-09/D-11) | unit + db_integration | `go test ./internal/agent/tools/ -run TestNewResult -count=1 && go test -tags db_integration -race -p 1 ./internal/conversations/ -run TestAppendTurnRedaction` | ❌ W0 | ⬜ pending |
| 40-07-01 | 07 | 2 | SEC-02 | T-40-07-CORS / T-40-07-CSRF | `withCORS` deleted; no `Access-Control-*` header; OPTIONS not 204; SameSite=Strict preserved (D-14) | unit (httptest) | `test ! -f internal/agui/server_cors.go && go test ./internal/agui/ -run TestNoCORSHeaders -count=1` | ✅ | ✅ passed |
| 40-07-02 | 07 | 2 | SEC-02 | T-40-07-DEADCODE | `AGUICORSPermissive` / `gateCORS` / `AURA_AGUI_CORS_PERMISSIVE` fully removed (no residue) (D-14) | unit + grep | `rg -n "AGUICORSPermissive\|gateCORS\|AURA_AGUI_CORS_PERMISSIVE" internal/ cmd/ ; go test ./internal/config/ -count=1` | ✅ | ✅ passed |
| 40-08-01 | 08 | 3 | SEC-09 | T-40-08-FORGE / T-40-08-REUSE | `HashLookupToken` = peppered HMAC-SHA-256; `DeriveResetTokenPepper` HKDF, fails closed on non-64-hex (D-06) | unit | `go test ./internal/agui/ -run 'TestHashLookupToken\|TestDeriveResetTokenPepper' -count=1` | ✅ | ✅ passed |
| 40-08-02 | 08 | 3 | SEC-09 | T-40-08-KEYABSENCE | Real Postgres mint→lookup round-trip resolves the same identity post-pepper | db_integration | `go test -tags db_integration -race -p 1 ./cmd/aura/ -run TestMintBreakGlassTokenRoundTrip` | ✅ | ✅ passed |
| 40-08-03 | 08 | 3 | SEC-09 | T-40-08-DOA / T-40-08-FIXEDSALT | Break-glass pepper threaded + presence guard; CodeQL security-and-quality clears `recovery_hash.go` (D-06b/D-07/D-08) | unit + CodeQL | `go test ./cmd/aura/ -run 'TestBreakGlass\|TestIdentityRecover' -count=1` | ✅ | ✅ passed |
| 40-09-01 | 09 | 3 | SEC-01 | T-40-09-CLEARTEXT / T-40-09-NONCE / T-40-09-KEYREUSE | Encrypted sink: fresh nonce/record, HKDF key, fail-closed on missing key, no plaintext path (D-13) | unit | `go test ./internal/tracesink/ -run TestSink -count=1 && go test -race ./internal/tracesink/ -count=1` | ❌ W0 | ⬜ pending |
| 40-09-02 | 09 | 3 | SEC-01 | T-40-09-LEAK | reasoningtrace uses canonical `secret` predicate + folds `redact.String`; `AURA_DB_URL` value + literal DSN redacted (D-11) | unit | `go test ./internal/reasoningtrace/ -run TestRecordRedaction -count=1 && go test -race ./internal/reasoningtrace/ -count=1` | ✅ | ⬜ pending |
| 40-09-03 | 09 | 3 | SEC-01 | T-40-09-PRODON | `gateReasoningTraceFull` Fatal unless `AURA_TRACE_FULL_ACK=1` under `Strict()`; 3 knobs registered (D-12) | unit | `go test ./internal/config/ -run TestGateReasoningTraceFull -count=1` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky.  File Exists: ✅ = test file present (edit/extend) · ❌ W0 = Wave-0 scaffold below.*

---

## Wave 0 Requirements

New test/support files that must be scaffolded before their task's automated verify can run. Execution creates
them — this is why `wave_0_complete: false` (honest: the files are not yet written):

- [ ] `internal/gateway/injection_suite_test.go` — SEC-04 deterministic policy-denial gate (40-02-01)
- [ ] `internal/eval/injection_cot_eval.go` + `_test.go` — SEC-04 D-04 LLM-resistance tier, manual `cot_eval` tag (40-02-02)
- [ ] `internal/agui/strict_decode.go` + `strict_decode_test.go` — SEC-06 D-16/D-16b centralized helper (40-03-01)
- [ ] `internal/agui/strict_decode_routes_test.go` — SEC-06 per-route trailing-JSON regression (40-03-02)
- [ ] `internal/secret/redact_exact.go` + `_test.go` — SEC-01 D-10 inbound exact-match detector (40-06-01)
- [ ] `internal/conversations/store_append_redaction_test.go` — SEC-01 `db_integration` round-trip: turn + spill + `.result` (40-06-02)
- [ ] `internal/tracesink/sink.go` + `_test.go` — SEC-01 D-13 encrypted sink, new package (40-09-01)
- [ ] `scripts/workflow_pin_gate.sh` + `workflow_pin_gate_test.sh` — SEC-05 D-19 shell gate + fixture self-test (40-05-03)

**Framework install:** none — `go.uber.org/goleak` and `testing` are already wired project-wide.

**Extend-existing** (test file present, net-new sub-tests added — NOT a scaffold gap; confirmed on disk at plan time):
- `cmd/aura/integrations_console_test.go` (SEC-03, 40-04-01/02) — preserve the existing `TestConsoleHandlerServesPageAndMountsProxy`.
- `internal/agui/recovery_hash_test.go` (40-08-01), the existing password-reset `db_integration` test (40-08-02), `cmd/aura/recovery_test.go` (40-08-03) — SEC-09.
- `internal/agui/server_test.go` (SEC-02, 40-07-01), `internal/config/config_validate_test.go` (40-07-02 / 40-09-03).
- `internal/agent/tools/result_test.go` (SEC-01, 40-06-02); `internal/reasoningtrace/reasoningtrace_test.go` (SEC-01, 40-09-02) — preserve `TestRecord_DefaultOmitsVerbatimHistoryAndUser` / `TestRecord_FullModeAllowsVerbatimHistory`.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| LLM injection-resistance (does the model RESIST the injection) | SEC-04 (D-04/D-05) | Paid OpenRouter call; honors no-unsolicited-paid-runs. The deterministic gate proves only the enforcement backstop, not model resistance | `OPENROUTER_API_KEY=… go test -tags cot_eval -run TestInjectionCoTEval -timeout 600s -v ./internal/eval/` — run by a human on request, never in CI |
| CodeQL `go/weak-sensitive-data-hashing` clears for the peppered HMAC | SEC-09 (D-07/D-08) | Resolved locally: CodeQL CLI 2.26.2 + `codeql/go-queries` 1.6.7 security-and-quality suite reported zero results on `internal/agui/recovery_hash.go` | Re-run the same suite in `codeql.yml`; the local SARIF is intentionally kept outside the repository |
| goreleaser `sboms:` config is syntactically valid | SEC-05 (D-17) | `goreleaser`/`syft` may not be on the dev box; otherwise CI-validated in `release.yml` | `goreleaser check && goreleaser build --snapshot --clean` (or note CI-validated in 40-05-SUMMARY) |
| `vulncheck` is a required branch-protection status check ("blocks merges") | SEC-05 (D-18, OQ1) | Branch-protection config lives in repo settings, not the tree — cannot be asserted by reading files | `gh api repos/<owner>/aura/branches/master/protection --jq '.required_status_checks.contexts'`; confirm `vulncheck` appears. Do NOT claim "blocks merges" without it |
| `github/codeql-action` `init` + `analyze` pinned to the IDENTICAL SHA | SEC-05 (D-19) | grep cannot enforce cross-line SHA equality | Manual review-checklist item recorded in 40-05-SUMMARY |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies — every one of the 22 tasks carries an `<automated>` command; the 8 Wave-0 files are flagged ❌ W0
- [x] Sampling continuity: no 3 consecutive tasks without automated verify — all 22 tasks have automated verify
- [x] Wave 0 covers all MISSING references — the 8 scaffold files above cover every ❌ W0 row
- [x] No watch-mode flags — all commands are one-shot `go test -count=1` / `bash …` (no `-watch`)
- [x] Feedback latency < 30s — per-task quick unit tier is seconds; <30s target in every plan's acceptance criteria
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-22
