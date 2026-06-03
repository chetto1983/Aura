---
phase: 08-sandbox-2b-session-bound
plan: 01
subsystem: infra
tags: [sandbox, session-bound, prd-amendment, doc-only, egress-proxy, scoring, os-root, privacy-mode]

# Dependency graph
requires:
  - phase: 05-sandbox-2a-stateless
    provides: 2a runner + AR-05-01 egressless posture (the egress pivot deviates from this)
  - phase: 04-conversation-persistence
    provides: conversations PK (uuid) + Conversations.Delete cascade hook
provides:
  - "prd.md §Slice 2b amended: migration 0008 (not 0010), conversation_id uuid FK CASCADE, host-side forward proxy egress (supersedes iptables), two-tier persistence (D-01), docker-lifecycle carve-out (D-05), os.Root host walkers (D-13)"
  - "prd.md §Risk-Based Governance: internal/scoring home-slice migrated Slice 6 -> Phase 8 (amendment #41, D-11) + scope-guard D-12"
  - ".planning/DECISIONS.md §7: D-01..D-14 logged with amendment IDs #38..#42"
  - ".planning/phases/08-sandbox-2b-session-bound/08-DECISIONS-WAVE0.md: 3 Wave-0 OQs resolved (SSRF export, privacy fail-fast, session network+seccomp posture)"
affects: [08-02-db-substrate, 08-03-scoring, 08-04-ssrf-export, 08-05-sessionmanager, 08-06-proxy, 08-07-sidecar, 08-08-wiring, 08-09-security]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PRD-amendment gate as a Wave-1 doc-only plan that every code wave depends_on (truth-source ordered ahead of code)"
    - "Wave-0 decisions doc as a planning-state contract distinct from PRD amendments (sequencing contract, not truth-source)"

key-files:
  created:
    - .planning/phases/08-sandbox-2b-session-bound/08-DECISIONS-WAVE0.md
    - .planning/phases/08-sandbox-2b-session-bound/08-01-SUMMARY.md
  modified:
    - prd.md
    - .planning/DECISIONS.md

key-decisions:
  - "Five PRD amendments #38..#42 landed: D-01 two-tier persistence (persistent per-session interpreter + workspace), D-05 docker-lifecycle carve-out (NEVER mounts socket), D-08 host-side forward proxy egress (supersedes iptables / CAP_NET_ADMIN incompatible with cap_drop:ALL; CAP-02 'via iptables' superseded), D-11 internal/scoring home-slice Slice 6 -> Phase 8 (+ D-12 scope guard), D-13 os.Root/openat2 supersedes O_NOFOLLOW"
  - "Two schema landmine fixes: migration 0010 -> 0008 (floor is 0007, phase-order rule); conversation_id text -> uuid (conversations PK is uuid, verified 0005)"
  - "Wave-0 OQ2/A4: SSRF reuse = export minimal surface (ClassifyIP + dial-guard) over extracting internal/netguard; re-test web tier + no copy-paste"
  - "Wave-0 OQ4/A5: AURA_PRIVACY_MODE currently unread; add PrivacyMode config field, fail-fast under local-only + non-empty allowlist"
  - "Wave-0 OQ1/A2: session containers need connect(2)-allowed seccomp variant (egress contained host-side); proxy at bridge gateway IP; empty allowlist keeps 2a egressless; extends AR-05-01; live reachability spike = Wave-5 gate"

patterns-established:
  - "Amendment numbering continues from #37 (Phase 5): Phase-8 amendments = #38..#42"
  - "Per-phase implementation decisions logged as DECISIONS.md §7 (D-NN lowercase-d prefix), distinct from the D00-D28 architectural registry"

requirements-completed: []

# Metrics
duration: ~7min
completed: 2026-06-03
---

# Phase 8 Plan 01: PRD-Amendment Gate + Wave-0 Decisions Summary

**Five Phase-8 PRD/DECISIONS amendments (persistent-interp tier, docker-lifecycle carve-out, host-proxy egress, scoring home-slice, os.Root symlink guard) + two schema landmine fixes (migration 0010->0008, conversation_id text->uuid) landed in the truth-source, plus the three Wave-0 open questions resolved on paper — doc-only, gating every Phase-8 code wave.**

## Performance

- **Duration:** ~7 min
- **Completed:** 2026-06-03
- **Tasks:** 2 (both landed this run)
- **Files:** 2 modified (prd.md, DECISIONS.md) + 1 created (08-DECISIONS-WAVE0.md)

## Accomplishments

**Task 1 — 5 amendments + 2 schema landmine fixes (commit `21060757`):**
- prd.md §Slice 2b network-policy line: egress pivoted to host-side Go forward proxy + hostname-CONNECT allowlist + resolve-then-pin (amendment #40, D-08); iptables explicitly marked SUPERSEDED (needs CAP_NET_ADMIN, incompatible with `cap_drop: ALL`/D12); CAP-02 "via iptables" wording superseded.
- prd.md §Slice 2b: added the two-tier persistence note (amendment #38, D-01 — long-lived per-session Python interpreter so `x=42` survives, PLUS the RW workspace mount; the two PRD persistence claims are the two tiers, not a contradiction), the docker-lifecycle carve-out (amendment #39, D-05 — lifecycle via docker CLI, execution HTTP-only, NEVER the socket), and the os.Root/openat2 symlink guard (amendment #42, D-13 — supersedes literal O_NOFOLLOW; cascade is a manual no-follow openat walk, NOT os.RemoveAll).
- prd.md schema landmines: migration `0010` -> `0008_sandbox_sessions` (repo floor verified 0007 via `ls internal/db/migrations/`; phase-order rule); `conversation_id text` -> `conversation_id uuid ... ON DELETE CASCADE` (conversations PK is uuid). Applied consistently across the file-target table rows + the commit template block + the §Test-discipline 2b row.
- prd.md §Risk-Based Governance: added the home-slice note that `internal/scoring/` ships in Phase 8 (amendment #41, D-11), consumed by Scheduler P10 / Skills P11, with the D-12 scope guard (module only, sandbox advisory path).
- DECISIONS.md §7: D-01..D-14 logged with amendment IDs #38..#42 and per-decision deviation flags.

**Task 2 — 3 Wave-0 open questions resolved (commit `1b18b915`):**
- Created `08-DECISIONS-WAVE0.md` with the three resolutions the code waves consume: (1) SSRF reuse = export minimal surface from `internal/web` (`ClassifyIP` + dial-guard) over extracting `internal/netguard`, with web-tier re-test mandate + no-copy-paste rule; (2) privacy-mode = add `PrivacyMode` config field, fail-fast at session-create under `local-only` + non-empty allowlist; (3) session network+seccomp posture = connect(2)-allowed session seccomp variant + bridge-gateway-IP proxy reachability + empty-allowlist-keeps-egressless + extends-AR-05-01 + live reachability spike as a Wave-5 gate item.

## Task Commits

1. **Task 1: Apply 5 PRD/DECISIONS amendments + 2 schema landmine fixes** - `21060757` (docs)
2. **Task 2: Resolve 3 Wave-0 open questions into a decisions doc** - `1b18b915` (docs)

**Plan metadata:** see final docs commit (SUMMARY + STATE + ROADMAP progress).

## Files Created/Modified

- `prd.md` — §Slice 2b (egress host-proxy, two-tier persistence, docker-lifecycle carve-out, os.Root guard, 0010->0008, text->uuid across acceptance + file-targets + commit-template + test-discipline rows) + §Risk-Based Governance (scoring home-slice note + scope guard).
- `.planning/DECISIONS.md` — new §7 logging D-01..D-14 with amendment IDs #38..#42 + the schema-landmine note.
- `.planning/phases/08-sandbox-2b-session-bound/08-DECISIONS-WAVE0.md` — created; the Wave-0 contract Waves 2-5 cite, with a consumption map.

## Decisions Made

- **ROADMAP.md not edited:** the plan's Task-1 action mentioned updating the Phase-8 Goal line ("via iptables" -> host-proxy, "O_NOFOLLOW" -> os.Root), but the planner had **already pre-applied** that wording in the Phase-8 Goal (ROADMAP.md:265 reads "host-side forward-proxy hostname allowlist ... os.Root/openat2 symlink escape guard (D-13)" with iptables/O_NOFOLLOW present only as "supersedes" notes). Editing it would be a no-op, so it was left unchanged. This is consistent with the Task-1 acceptance grep list (which checks prd.md + DECISIONS.md, not ROADMAP wording).
- **05-SECURITY.md not edited:** it appears in the plan frontmatter `files_modified` and Task-2 `read_first`, but Task 2's action explicitly defers the AR-05-01 re-statement to "08-SECURITY (authored in 08-09)". So this plan only *records the contract* in 08-DECISIONS-WAVE0; the 05/08-SECURITY edit is plan 08-09's work, not this gate's.
- **DECISIONS.md §7 vs the #36 format:** the plan said to "mirror the #36 entry format". The #36 entry lives in the D00-D28 *architectural* registry (a table-row format). The D-01..D-14 here are *per-phase implementation* decisions (lowercase-d, distinct namespace). They were logged as a dedicated §7 table mirroring the registry's table format + amendment-ID column, keeping the two namespaces separate to avoid renumbering the architectural registry.
- **Document language:** prd.md and DECISIONS.md are Italian-native; all amendment prose was written in Italian to match the surrounding sections, with English literals (e.g. `host-side`, `connect(2)`, `os.Root`) preserved verbatim where they are the canonical identifier the verify-greps and downstream code cite. 08-DECISIONS-WAVE0.md is English (matching the English 08-CONTEXT/08-RESEARCH planning docs).

## Deviations from Plan

None functional — the two tasks executed as written. The only judgment calls were the two no-op skips above (ROADMAP Goal already pre-amended by the planner; 05/08-SECURITY edit deferred to 08-09 per Task 2's own action text), neither of which changes the gate's outcome.

## Ground-Truth Verifications

- Highest existing migration confirmed `0007_cache_metrics` via `ls internal/db/migrations/` before renumbering 0010 -> 0008 (per the critical-notes instruction to verify rather than guess).
- `AURA_PRIVACY_MODE`/`PrivacyMode` confirmed **unread** in `internal/` (`grep -rn` returns nothing) — validates the OQ4 premise that the field must be *introduced*, not read.
- `internal/web` SSRF symbols confirmed unexported (`classify` ssrf.go:35, `validateAndPin` ssrf.go:85, `dnsPin` dnspin.go) — validates the OQ2 export-vs-extract framing (landmine #5).

## Known Stubs

None — doc-only plan, no code surface.

## Threat Flags

None — doc-only plan; no new network endpoints, auth paths, file access, or schema changes at trust boundaries (the schema change is a *doc* edit to the planned migration, not a live schema). T-08-01-DOC (the truth-source-drift repudiation threat) is exactly what this plan mitigates by landing the amendments before any code.

## Next Phase Readiness

- The truth-source (prd.md + DECISIONS.md) now matches the code Waves 2-5 will write: migration 0008/uuid FK, host-proxy egress, two-tier persistence, docker-lifecycle carve-out, scoring home-slice, os.Root guard. No code wave can contradict locked content without a fresh amendment.
- The three Wave-0 open questions are resolved on paper; 08-04 (SSRF export), 08-02/05/08 (privacy-mode), and 08-07/08/09 (session network+seccomp posture) have an unambiguous contract.
- Tracked obligation carried forward: the session network+seccomp deviation (connect-allowed + host proxy + bridge-gateway reachability) EXTENDS AR-05-01 and MUST be re-stated in 08-SECURITY (plan 08-09); the live session->proxy reachability is the single highest-risk assumption (A2 HIGH) and is flagged as a Wave-5 gate item.

## Self-Check: PASSED

- `.planning/phases/08-sandbox-2b-session-bound/08-01-SUMMARY.md` — present (this file)
- `.planning/phases/08-sandbox-2b-session-bound/08-DECISIONS-WAVE0.md` — present
- Commit `21060757` (Task 1) — present in git log
- Commit `1b18b915` (Task 2) — present in git log
- prd.md contains `0008_sandbox_sessions` (3x), `conversation_id uuid` (5x), `host-side` (4x); iptables present only as "superseded" in the 2b egress context
- DECISIONS.md §7 contains D-01..D-14 + amendment IDs #38..#42
- `git diff --name-only HEAD~2 HEAD` shows ZERO `.go`/`.sql`/`.py` files (doc-only gate held)

---
*Phase: 08-sandbox-2b-session-bound*
*Completed: 2026-06-03*
