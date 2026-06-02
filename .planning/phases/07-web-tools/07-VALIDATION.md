---
phase: 7
slug: web-tools
status: ready
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-02
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from `07-RESEARCH.md` §Validation Architecture. The 4 ROADMAP success criteria are the requirement set (CAP-05). SSRF defense (D-24..D-30) is the dominant risk surface — security tests must be deterministic, non-leaky, and fail-closed.
>
> **`wave_0_complete: false`** is intentional at plan time: it flips green during execution once the Wave 0 TDD scaffolds (the `*_test.go` files + `main_test.go` goleak harness) compile and the RED tests are in place. The new 07-04 Task 3 (validation-status update) owns that flip — see Per-Task Verification Map row 07-04-T3.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (table-driven + goleak + race) |
| **Config file** | none — uses existing Aura Go toolchain |
| **Quick run command** | `go test ./internal/web/... ./internal/agent/tools/` |
| **Full suite command** | `go test -race -tags 'web_integration' ./internal/web/... ./internal/agent/tools/...` |
| **Estimated runtime** | unit/quick: sub-second (table-driven SSRF + httptest in-process); full `-race` incl. `web_integration` + SearXNG container startup: ~10-30s |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/web/...` (sub-second unit tier)
- **After every plan wave:** Run the full `-race` suite
- **Before `/gsd-verify-work`:** Full suite must be green (incl. SSRF + DNS-rebinding tiers)
- **Max feedback latency:** ~1s for the unit tier (per-task signal); ~30s for the full `-race -tags web_integration` tier (per-wave signal, incl. SearXNG container startup)

---

## Per-Task Verification Map

> Derived during planning from RESEARCH §Validation Architecture. Every SSRF/contract decision (D-07..D-30, D-38..D-42) and each of the 4 ROADMAP success criteria maps to an observable signal at a test tier. One row per non-checkpoint task across all 4 plans. Commands copied verbatim from each task's `<verify><automated>`.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 07-01-T2 | 01 | 1 | CAP-05 | T-07-01 / T-07-02 | SearXNG service has NO host `ports:` (internal-only); read-only `settings.yml` enables JSON `formats` | unit (compose config + grep) | `docker compose -f compose.yaml config 2>&1 \| grep -A30 "searxng:" \| grep -qv "ports:" && echo "no host port OK"; grep -E "formats" searxng/settings.yml` | ✅ | ✅ green |
| 07-01-T3 | 01 | 1 | CAP-05 | T-07-03 | Missing `SEARXNG_URL` is NOT boot-fatal (fail-closed at call time); web config + goleak harness build | unit | `go build ./... && go test ./internal/web/... && grep -E "AURA_WEB_DNS_PIN_TTL_SEC\|AURA_WEB_RESPONSE_CAP_BYTES\|SEARXNG_URL" .env.example` | ✅ | ✅ green |
| 07-02-T1 | 02 | 2 | CAP-05 | T-07-10 / T-07-11 / T-07-14 / T-07-15 | IP classifier blocks every SSRF class (incl. unmapped IPv4-mapped IPv6 + mixed records + metadata hostnames); WebError is sanitized non-leaky | unit (table-driven + leak-scan) | `go test -race -run "TestBlocked_Classification\|TestError_NonLeaky\|TestValidateAndPin_MixedRecords\|TestHostnameBlocklist" ./internal/web/` | ✅ | ✅ green |
| 07-02-T2 | 02 | 2 | CAP-05 | T-07-12 / T-07-13 | DNS pin reuse within TTL per (conv,host) (SC#4); transport dials only the pinned IP, Control re-checks, never auto-follows redirects; goleak-clean | unit (recording dialer + injectable resolver) | `go test -race -run "TestDNSPin_TTL\|TestTransport_DialsPinnedIP\|TestTransport_ControlRecheck\|TestTransport_NoAutoRedirect" ./internal/web/` | ✅ | ✅ green |
| 07-03-T1 | 03 | 3 | CAP-05 | T-07-23 / T-07-24 | SearXNG query build (`site:` rewrite + `format=json` + category enum general/news) + domain post-filter + structured unavailable errors + one-retry policy | unit (JSON fixture + counting handler) | `go test -race -run "TestSearch_ParseAndFilter\|TestSearch_Unavailable\|TestSearch_RetryPolicy\|TestSearch_CategoryEnum" ./internal/web/` | ✅ | ✅ green |
| 07-03-T2 | 03 | 3 | CAP-05 | T-07-20 / T-07-21 / T-07-22 / T-07-25 | Fetch scheme/SSRF/per-hop-redirect/MIME/size gate (SC#2 clean markdown, `redirect_to_blocked_target` at the hop) + low_content warning + per-host concurrency cap + one-retry; cache TTL | unit (httptest + recording server) | `go test -race -run "TestFetch_Readability\|TestFetch_RedirectRevalidate\|TestFetch_ContentGate\|TestFetch_LowContent\|TestFetch_UnsupportedScheme\|TestFetch_PerHostConcurrencyCap\|TestRetry_Policy\|TestCache_TTL" ./internal/web/` | ✅ | ✅ green |
| 07-04-T1 | 04 | 4 | CAP-05 | T-07-30 / T-07-31 / T-07-32 | Both tools `Deferred:true` (manifest by name only); large fetch markdown spills via NewResult (>24000B → Truncated+sidecar); sanitized inline errors greppable-clean | unit | `go build ./... && go test -race -run "TestWebFetch_Spillover\|TestWebSearch_DeferredSpec\|TestWeb_SanitizedInlineError" ./internal/agent/tools/ && go run ./cmd/aura tools \| grep -E "web_search\|web_fetch"` | ✅ | ✅ green |
| 07-04-T2 | 04 | 4 | CAP-05 | T-07-33 / T-07-34 / T-07-SC | `aura web` CLI (doctor + tool verbs) builds + vets under `web_integration`; SSRF smoke asserts SC#3 blocks; live `web_integration` tier `t.Fatal`s under `$CI` when env unset (no-skip-as-green) | smoke + integration (structural build/vet) | `go build ./cmd/aura && go vet -tags web_integration ./internal/web/` | ✅ | ✅ green |
| 07-04-T3 | 04 | 4 | CAP-05 | — | At execution time, flips this file's `wave_0_complete: true` + per-task statuses to ✅ once scaffolds compile and the RED tier passes; updates `docs/aura-quality-snapshot.md` web row | doc-contract (no code) | `grep -E "wave_0_complete: true" .planning/phases/07-web-tools/07-VALIDATION.md` | ✅ | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

> Checkpoint tasks excluded from the map (no `<automated>` verify by design): 07-01-T1 (package legitimacy human-verify) and 07-04-T4 (Live SC#1/SC#2 acceptance + mutation gate human-verify). Their SCs are proven by the automated rows above plus the Manual-Only table.

---

## Wave 0 Requirements

> SSRF-critical scaffolds. These are the TDD RED files created first in each plan; `wave_0_complete` flips when they compile and the RED tests are in place (owned by 07-04-T3).

- [x] `internal/web/main_test.go` — `goleak.VerifyTestMain` (07-01 Task 3)
- [x] IP-classification table tests (private/loopback/link-local/ULA/IPv4-mapped-IPv6 `::ffff:0:0/96`/unspecified/multicast/CGNAT) — `internal/web/ssrf_test.go` (07-02 Task 1)
- [x] DNS-rebinding fixture (injectable Go resolver returning 1.2.3.4 then 127.0.0.1) — SC#4 — `internal/web/dnspin_test.go` (07-02 Task 2); `dnspin_integration_test.go` belt-and-suspenders (07-04 Task 2)
- [x] redirect-to-blocked-target httptest fixture — D-29 — `internal/web/fetcher_test.go` (07-03 Task 2)
- [x] SearXNG-unavailable fixture (structured `web_search_unavailable`) — D-06 — `internal/web/searxng_test.go` (07-03 Task 1)
- [x] readability+markdown golden fixtures for `web_fetch` extraction — SC#2 — `internal/web/html_test.go` / `fetcher_test.go` (07-03 Task 2)
- [x] spillover fixture (>24000B → Truncated + sidecar) — D-21 — `internal/agent/tools/web_fetch_test.go` (07-04 Task 1)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live `aura tool web_search` p95 ≤ 2s against running SearXNG | CAP-05 / SC#1 | Needs live SearXNG container + public internet | `scripts/web_search_smoke.sh` against the Compose stack (07-04 Task 4 checkpoint) |
| Live `aura tool web_fetch` clean markdown (SC#2 end-to-end) | CAP-05 / SC#2 | Needs live public page fetch | `aura tool web_fetch '{"url":"https://en.wikipedia.org/wiki/Knowledge_graph"}'` (07-04 Task 4) |
| Package legitimacy (exact module paths resolve before `go get`) | CAP-05 / D-20 | pkg.go.dev / codeberg page confirmation is human-verify | 07-01 Task 1 checkpoint (`go list -m -versions` + browser confirm) |
| Mutation ≥70% killed on `ssrf.go` | CAP-05 / Gate-3 | WSL `go-mutesting`, not CI-gated | `go-mutesting ./internal/web/ssrf.go` (07-04 Task 4); PASS=killed |
| Coverage ≥85% on `internal/web` owned surface across full tag matrix | CAP-05 / Gate-3 | Needs SearXNG container up for the integration tier | `make coverage` (or scoped gate) with stack up (07-04 Task 4) |

*Most behaviors have automated verification; the manual rows are the live-stack + mutation numbers that cannot be fabricated without the running container.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (every non-checkpoint task row carries an `<automated>` command; checkpoints are excluded by design and covered by automated rows + the Manual-Only table)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (all 8 non-checkpoint task rows have automated commands)
- [x] Wave 0 covers all MISSING references (esp. SSRF fixtures) — enumerated above, each mapped to the task that creates it
- [x] No watch-mode flags (all commands are single-shot `go test` / `go build` / `go vet`)
- [x] Feedback latency recorded (unit ~1s; full `-race -tags web_integration` ~10-30s incl. SearXNG startup)
- [x] `nyquist_compliant: true` set in frontmatter
- [x] `wave_0_complete: true` — flipped during execution (07-04 Task 3): the TDD scaffolds compile, every Per-Task Verification Map command is green (unit + build/vet tiers run live), and the `web_integration` tier compiles under its tag. The live SC#1/SC#2 + Gate-3 numbers (mutation, combined coverage, p95) are the 07-04 Task 4 human-verify checkpoint — they gate CAP-05 closure, not this Wave-0 flip.

**Approval:** Wave 0 complete — all 9 non-checkpoint task rows green; CAP-05 closes after the 07-04 Task 4 live + Gate-3 sign-off.
