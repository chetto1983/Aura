---
phase: 8
slug: sandbox-2b-session-bound
status: draft
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-03
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Go 1.26) + Python stdlib unittest (sidecar) |
| **Config file** | none — build-tag `sandbox_integration` tier; `synctest` for TTL reaper |
| **Quick run command** | `go test ./internal/sandbox/ ./internal/scoring/` |
| **Full suite command** | `go test -race -tags 'sandbox_integration' ./internal/sandbox/ ./internal/scoring/ ./internal/conversations/` |
| **Estimated runtime** | ~60 seconds (unit); integration adds container spin-up |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/<package>/`
- **After every plan wave:** Run the full suite command
- **Before `/gsd-verify-work`:** Full suite (incl. `sandbox_integration` tier) must be green
- **Max feedback latency:** 60 seconds (unit); integration on wave boundary

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| crit 1a | 08-09 | 5 | CAP-02 | T-08-07-INFO-XSESSION | session python namespace persists `x=42` across calls; fresh conv isolated | integration (sandbox+db) | `go test -tags 'sandbox_integration db_integration' -run TestSessions_PythonStatePersists ./internal/sandbox/` | ✅ sessions_live_integration_test.go | 🔵 live-pending (Task 3) |
| crit 1b | 08-09 | 5 | CAP-02 | T-08-05-INFO-XCONV | workspace file written call 1 visible call 2 | integration (sandbox+db) | `go test -tags 'sandbox_integration db_integration' -run TestSessions_WorkspacePersists ./internal/sandbox/` | ✅ sessions_live_integration_test.go | 🔵 live-pending (Task 3) |
| crit 1c | 08-09 | 5 | CAP-02 | T-08-05-DOS-CAP / D-07 | two goroutines same conv serialize via per-session lock; one container | integration (sandbox+db, -race) | `go test -race -tags 'sandbox_integration db_integration' -run TestSessions_ConcurrentSerialized_Live ./internal/sandbox/` | ✅ sessions_live_integration_test.go | 🔵 live-pending (Task 3) |
| crit 2 | 08-09 | 5 | CAP-02 | T-08-05-EOP-SYMLINK / T-08-08-EOP-SYMLINK | container-planted `ln -s /etc /workspace/escape` + host cascade → host /etc intact, symlink removed | integration (sandbox+db) | `go test -tags 'sandbox_integration db_integration' -run TestWorkspace_SymlinkEscapeCascade_Live ./internal/sandbox/` | ✅ workspace_integration_test.go | 🔵 live-pending (Task 3) |
| crit 3 (live) | 08-09 | 5 | CAP-02 | T-08-05-DOS-CAP | idle TTL → live container docker-rm + registry terminated + liveCount 0 | integration (sandbox+db) | `go test -tags 'sandbox_integration db_integration' -run TestReaper_LiveContainerRemoved ./internal/sandbox/` | ✅ sessions_live_integration_test.go | 🔵 live-pending (Task 3) |
| crit 3 (synctest) | 08-05 | 3 | CAP-02 | T-08-05-DOS-CAP | deterministic virtual-clock TTL eviction | unit (synctest) | `go test -run TestReaper_EvictsAfterTTL ./internal/sandbox/` | ✅ sessions_test.go | ✅ green (unit) |
| crit 4a + landmine-3 spike | 08-09 | 5 | CAP-02 | T-08-08-INFO-NET / T-08-06-INFO-EXFIL | `pip install` to pypi succeeds through host proxy at the bridge gateway (reachability spike) | integration (live egress) | `go test -tags 'sandbox_integration db_integration' -run TestNetwork_PyPIAllowed ./internal/sandbox/` | ✅ network_integration_test.go | 🔵 live-pending (Task 3) |
| crit 4b | 08-09 | 5 | CAP-02 | T-08-06-INFO-EXFIL / T-08-06-INFO-REBIND | same posture: non-allowlisted host (example.com / 1.1.1.1) refused (deny-wins) | integration (live egress) | `go test -tags 'sandbox_integration db_integration' -run TestNetwork_NonAllowlistRefused ./internal/sandbox/` | ✅ network_integration_test.go | 🔵 live-pending (Task 3) |
| boot recovery | 08-09 | 5 | CAP-02 | D-06 | prior active rows → terminated; lazy recreate on next Acquire | integration (sandbox+db) | `go test -tags 'sandbox_integration db_integration' -run TestSessions_BootRecovery ./internal/sandbox/` | ✅ sessions_integration_test.go + sessions_live_integration_test.go | 🔵 live-pending (Task 3) |
| 0008 round-trip | 08-09 | 5 | CAP-02 | T-08-02-V5-FK / T-08-02-V14-ROLE | 0008 up: table+index+uuid-FK+CHECK; ON DELETE CASCADE removes session row | integration (db) | `go test -tags db_integration -run TestMigration0008_SchemaRoundTrip ./internal/sandbox/` | ✅ sessions_integration_test.go | 🔵 live-pending (Task 3) |
| proxy unit | 08-06 | 2 | CAP-02 | T-08-06-INFO-EXFIL | deny-wins glob, *.x not parent, global-* reject, resolve-then-pin SSRF, malformed CONNECT | unit | `go test -run TestProxy_AllowlistGlobAndSSRF ./internal/sandbox/` | ✅ network_test.go | ✅ green (unit) |
| sandbox tier | 08-03 | 1 | CAP-02 | T-08-03-INFO-TIER | empty=Safe, pypi-only=Safe, arbitrary=Risky; monotone modifiers | unit (rapid) | `go test ./internal/scoring/` | ✅ scoring_test.go | ✅ green (unit) |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · 🔵 live-pending (authored + compiles under tags; live green is the Task-3 human Gate-3 sign-off)*

---

## Wave 0 Requirements

- [x] Confirm `migrations/0008_sandbox_sessions.{up,down}.sql` (NOT 0010 — repo is at 0007) with `conversation_id uuid REFERENCES conversations(id)` (NOT text — landmine #1/#2 from RESEARCH.md) — shipped 0008 (08-02); `TestMigration0008_SchemaRoundTrip` asserts table+index+uuid-FK+CHECK+CASCADE
- [x] `internal/web` SSRF export-or-extract decision (landmine #5) before egress-proxy plan can reuse `classify`/`guard`/`dnsPin` — RESOLVED 08-DECISIONS-WAVE0 (OQ2/A4: export minimal surface, `web.ClassifyIP`+`web.NewDialGuard`); wired in `network.go` (08-04/08-06)
- [x] Live egress-bridge reachability spike (landmine #3, MEDIUM confidence) — session-container seccomp/network posture vs host proxy — RESOLVED 08-DECISIONS-WAVE0 (OQ1/A2: connect-allowing session seccomp + host proxy at bridge gateway + empty-allowlist-egressless); the live spike is `TestNetwork_PyPIAllowed` (authored 08-09, run live at Task-3 Gate-3)

*Full Wave 0 gap list lives in 08-RESEARCH.md `## Validation Architecture`.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| The 4 ROADMAP criteria live | CAP-02 | The live tier needs a running Docker daemon (gVisor/runc), a migrated Postgres, and (criterion 4) a host forward proxy reachable at the egress-bridge gateway + public pypi.org egress — none available in the authoring env. | WSL with the stack up (`make db-up && make sandbox-up && aura db migrate`), export the composed DSNs + `AURA_SANDBOX_URL` + `AURA_SANDBOX_SESSION_IMAGE` + `AURA_RUN_DIR` + the egress wiring (`AURA_SANDBOX_NETWORK_ALLOW_HOSTS`, `AURA_SANDBOX_EGRESS_NETWORK`, `AURA_SANDBOX_PROXY_ENV`, `AURA_SANDBOX_SESSION_SECCOMP`) + `CI=true`, then `go test -race -tags 'sandbox_integration db_integration' ./internal/sandbox/`. See 08-09-PLAN Task 3 `<how-to-verify>` for the 7-step operator runbook. |

*The 4 ROADMAP success criteria (session persistence, symlink-escape refusal, TTL reap, network allowlist) map to named test commands in 08-RESEARCH.md `## Validation Architecture`.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s (unit); integration on the live Gate-3 boundary (Task 3)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** Wave-0 complete; per-task map populated (08-09). Live-tier cells are 🔵 live-pending — they flip to ✅ at the Task-3 human Gate-3 sign-off (live stack required). The authored tier compiles green under `-tags 'sandbox_integration db_integration'` (vet + build + test-compile exit 0, 2026-06-03).
