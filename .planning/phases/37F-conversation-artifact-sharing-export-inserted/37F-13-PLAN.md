---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 13
type: execute
wave: 7
depends_on: ["37F-11", "37F-12"]
files_modified:
  - internal/agui/share_cross_identity_test.go
  - .planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
autonomous: true
requirements: [WEBSHARE-04]

must_haves:
  truths:
    - "All ten SC4 cross-identity rows pass against a real Postgres"
    - "The SC4 test lives in internal/agui under a single db_integration tag, so it actually contributes coverage"
    - "Row 9 passes: a token scopes to ITS snapshot's artifacts only — another identity's assetID 404s"
    - "Row 10 passes: a public token grants zero access to the identity-scoped /api/assets lane"
    - "Every identity in the suite is provisioned and non-wildcard, so no capability assertion passes vacuously"
    - "No 37F test carries a build tag outside db_integration — the whole Go surface is inside the coverage gate"
    - "Aggregate coverage is >=85% AND every owned 37F package individually clears 85"
  artifacts:
    - path: "internal/agui/share_cross_identity_test.go"
      provides: "the SC4 cross-identity deny E2E — the WEBSHARE-04 coverage vehicle"
      min_lines: 200
  key_links:
    - from: "internal/agui/share_cross_identity_test.go"
      to: "objectstore.NewFake()"
      via: "in-memory store — no Garage, no garage_integration tag"
      pattern: "NewFake"
  prohibitions:
    - "MUST NOT place this test in cmd/aura — the coverage gate measures ./internal/... ONLY; cmd/aura contributes ZERO coverage at any tag"
    - "MUST NOT add any build tag beyond db_integration — the existing cross-deny E2E is 5-tag-gated and therefore compiles+skips in CI, contributing ZERO coverage (WR-01). That is the exact mistake this test exists to avoid."
    - "MUST NOT use the seeded local identity or the bootstrap identity — both hold the wildcard, so every capability assertion would pass vacuously (R-13)"
    - "MUST NOT use Garage, Authula, or Neo4j — FakeStore + withPrincipal + httptest cover all ten rows"
    - "MUST NOT assert only status codes on rows 5 and 7 — assert the BODY carries no foreign data and no snapshot bytes"
    - "MUST NOT skip silently under CI — the skip helper must t.Fatal when the DSN is unset and CI is set"
    - "MUST NOT run the coverage gate against the live aura DB — use scripts/coverage_docker.sh (disposable aura_cov)"
---

<objective>
Ship WEBSHARE-04's centrepiece: the cross-identity deny E2E, all ten rows.

**This test exists because the obvious place to put it is wrong.** The repo already has a cross-identity
E2E — `cmd/aura/two_identity_e2e_test.go` — and it cannot be 37F's coverage vehicle for two independent
reasons:
1. **Tags.** It requires `db_integration && neo4j_integration && garage_integration &&
   authula_integration && musr_e2e`. The coverage gate runs **exactly** `db_integration
   neo4j_integration`, so that file **compiles + skips** in CI and contributes **zero** coverage. This is
   the documented WR-01 failure mode — the same one that let the CAP_NET_ADMIN cap-assertion bug stay
   latent.
2. **Package.** The gate measures `./internal/...` only. **`cmd/aura` is not measured at all, at any tag.**

So SC4 lives in `internal/agui` under one tag, with `objectstore.NewFake()`. That 37F has **zero**
container/daemon-gated code is not an accident — it is a design property, and this test is where it pays
off. If any 37F test reaches for `garage_integration`, that code silently drops out of the 85% floor and
CI fails ~20 minutes after the push.

Rows **9 and 10** are the ones a naive implementation fails: 9 catches *"the token authenticates, then any
asset id is fetched"*; 10 catches *"the public session leaks into the authenticated lane."*

Purpose: prove no other identity's data reaches a recipient (WEBSHARE-03/04).
Output: `internal/agui/share_cross_identity_test.go` + a closed-out VALIDATION.md.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@internal/agui/share_api.go
@CLAUDE.md
</context>

## Artifacts this plan produces

`TestShareCrossIdentityDeny` (10 subtests) — the WEBSHARE-04 acceptance vehicle — and the completed
`37F-VALIDATION.md` Per-Task Verification Map.

<tasks>

<task type="auto">
  <name>Task 1: TestShareCrossIdentityDeny — the ten-row matrix</name>
  <read_first>
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` §"The SC4 cross-identity deny E2E — exact wiring" — **the ten rows, verbatim. This is the contract; implement every row.**
    - `cmd/aura/two_identity_e2e_test.go` — read **only** the build-constraint line, to see the 5-tag combo this test must NOT copy. Model nothing else on it.
    - `internal/agui/auth_capability_integration_test.go` — **the whole file: the shape to copy.** Single `//go:build db_integration`; the header's env + run-command + no-skip-as-green block; `migratedPool(t)`; `withPrincipal(httptest.NewRequest(...), identityID)` to inject a principal with no cookie and no Authula; `t.Cleanup` row deletion; `t.Run` subtests. Its 403 subtest (`:91-116`) is the **direct template for rows 6 and 8** and shows the fresh **non-wildcard** identity seed using `name = "..."+t.Name()` for parallel-run uniqueness.
    - `cmd/aura/two_identity_e2e_harness_test.go:38-41,95-102` — `musrEnvOrSkip` (the no-skip-as-green precedent) and the two-identity seeding pattern. Note it seeds `agent.run` via a raw INSERT into `capability_grants`; 37F seeds `share.public` the same way for row 8.
    - `internal/agui/server_integration_test.go` — the shared `envOrSkip`. Use it; do not write a new skip helper.
    - `internal/objectstore/fake.go:17` — `NewFake()`: the in-memory `Store` that keeps this test inside the two-tag gate.
    - `internal/agui/share_api.go` (plan 37F-10) — the real handler signatures and routes under test
  </read_first>
  <action>
    Create `internal/agui/share_cross_identity_test.go`, package `agui`, with **exactly one** build tag:
    `//go:build db_integration`.

    Header, following `auth_capability_integration_test.go`: the env the test reads (the **composed DSNs**
    `AURA_DB_URL` + `AURA_DB_MIGRATE_URL`, not the `POSTGRES_*` primitives), the run command, and the
    no-skip-as-green note. **Add a comment stating why this file is here and not in `cmd/aura`**: the gate
    measures `./internal/...` only and runs exactly two tags, so a `cmd/aura` `musr_e2e` variant would
    contribute zero coverage (WR-01). A future reader will otherwise "consolidate" it next to the existing
    two-identity E2E and silently delete this test's entire value.

    Seed **two provisioned non-wildcard identities** A and B per run (`name = "..."+t.Name()`), each with a
    conversation and an agent-produced artifact. `t.Cleanup` removes them. Use `objectstore.NewFake()`.
    **Never** use the seeded `local` identity or the bootstrap identity — both hold the wildcard, which
    makes every capability assertion pass vacuously (R-13, verified at plan time:
    `serve_bootstrap.go:176-180` grants the literal wildcard).

    Implement all ten rows as `t.Run` subtests:
    1. **A owns conv-A; B GETs the export of conv-A ⇒ 404** (not 403 — reads hide foreign existence).
       Also assert the body omits A's conversation title.
    2. **B POSTs to create a share for conv-A ⇒ 404** — B cannot mint a link to A's thread.
    3. **A minted an internal link; B (authenticated) resolves it ⇒ 200.** This is the one row whose
       expected answer is success: D-10 bearer-within-auth is *intended*, and the redacted snapshot is the
       protection. Comment it as such so a later reader does not "fix" it to a 404.
    4. **A minted an internal link; anonymous resolves ⇒ 401/302** — internal is NOT on the public
       allowlist; `RequireAuth` gates it.
    5. **A minted a public link; anonymous resolves ⇒ 200 + zero B data + zero paths.** Assert the BODY:
       it contains A's prose, and contains none of B's conversation text, B's artifact filename, or any
       `/abs/`, `/etc/`, or `AURA_RUN_DIR` substring. A status-only assertion here proves nothing.
    6. **B revokes A's link ⇒ 404** — B cannot revoke A's link.
    7. **A minted a public link, then revoked; anonymous resolves ⇒ 404 + no stale render.** Assert the
       body carries no snapshot bytes (it does not contain A's title).
    8. **B holds `share.public`; A does NOT; A mints public ⇒ 403.** Seed `share.public` into
       `capability_grants` for B only, via a raw INSERT (the harness pattern). This row is the reason the
       identities must be non-wildcard.
    9. **A's public snapshot; anonymous requests B's assetID under A's token ⇒ 404.** A token scopes to
       **its** snapshot's artifacts only. This is the row a naive "token authenticates, then fetch any
       asset id" implementation fails.
    10. **A's public link; anonymous requests A's assetID through the identity-scoped download lane ⇒
        401/302.** The token grants **no** access to that lane. This is the row a "the public session leaks
        into the authenticated lane" implementation fails.

    Use `withPrincipal` for the authenticated rows and a bare request for the anonymous ones.

    For rows 4 and 10 (and any other route whose behavior depends on the `RequireAuth` wrap), exercise the
    handler **through `RequireAuth`** with the same `AuthDeps` and `PublicRoute` chain the server builds —
    a bare handler call would bypass the very gate under test and pass vacuously. If wiring the real chain
    from `internal/agui` is impossible for a route whose mount lives in `cmd/aura`, assert
    `isPublicShareRoute` plus `RequireAuth`'s behavior directly, and state that decomposition in the
    SUMMARY. Do **not** silently downgrade a row to a bare handler call.
  </action>
  <verify>
    <automated>go test -tags db_integration -race -p 1 -count=1 -run TestShareCrossIdentityDeny -v ./internal/agui/</automated>
  </verify>
  <acceptance_criteria>
    - `go test -tags db_integration -race -p 1 -count=1 -run TestShareCrossIdentityDeny ./internal/agui/` passes with **all ten** subtests reporting PASS, and a **non-sub-second** runtime (sub-second means it skipped — verify execution, not just PASS).
    - `head -1 internal/agui/share_cross_identity_test.go` is exactly `//go:build db_integration`, and `grep -cE "garage_integration|authula_integration|musr_e2e|neo4j_integration|docker_integration" internal/agui/share_cross_identity_test.go` returns `0`.
    - The file is in `internal/agui`, NOT `cmd/aura`, and its header states why.
    - **No wildcard identities:** the file contains no grant of the wildcard capability, and no test references the `local` identity by name.
    - **Row 8** seeds `share.public` for B only via a raw `capability_grants` INSERT and asserts A's mint is 403.
    - **Row 5** asserts on the BODY (A's prose present; B's data, `/abs/`, `/etc/`, `AURA_RUN_DIR` all absent), not merely on the 200.
    - **Row 7** asserts the 404 body omits A's title — no stale render.
    - **Row 3** asserts 200 and carries a comment marking bearer-within-auth as intended.
    - **Rows 9 and 10 are present and pass** — the two rows a naive implementation fails.
    - **Rows 4 and 10 exercise the real `RequireAuth` chain**, or the SUMMARY records the decomposition used instead.
    - `envOrSkip` is used; the test `t.Fatal`s under `$CI` with the DSN unset.
    - `grep -c "t.Run(" internal/agui/share_cross_identity_test.go` returns ≥ 10.
  </acceptance_criteria>
  <done>All ten SC4 rows pass in `internal/agui` under a single `db_integration` tag with `FakeStore` and provisioned non-wildcard identities — including rows 9 and 10, with body-level assertions where a status code alone would prove nothing.</done>
</task>

<task type="auto">
  <name>Task 2: Phase-wide tag audit + the real coverage gate + close out VALIDATION.md</name>
  <read_first>
    - `scripts/coverage_gate.sh` — `:25` the gate runs exactly `-tags "db_integration neo4j_integration"`; `:52-53` it measures `./internal/...` only; `:64-67` the owned-surface exclusions (`internal/db/sqlc`, `internal/agent/agenttest`, `internal/llm/client.go`); `:35` it refuses `db_integration` against a DB named `aura` when run locally
    - `scripts/coverage_docker.sh` — the safe runner: it provisions a **disposable** `aura_cov` DB and drops it on exit. This closed the 2026-07-10 footgun that truncated the live deployment's auth tables (operator identity + Authula wiped, no backup). **Do not bypass it.**
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the Per-Task Verification Map (to populate) and the Validation Sign-Off list (to close)
    - `CLAUDE.md` — the COVERAGE GATE TAG SET rule (WR-01) and the `.env` `AURA_WEB_AUTH_SECRET` leak footgun that breaks `make coverage`
  </read_first>
  <action>
    Close the phase's test contract.

    **1. Tag audit.** Across every file 37F created or modified, assert no test carries a build tag other
    than `db_integration`. 37F adds **zero** container/daemon-gated code — the only external dependency is
    Garage, covered in-process by `objectstore.FakeStore` — so 100% of its Go surface is reachable under
    the gate's two tags. Any stray tag means that code contributes zero coverage and CI fails ~20 minutes
    after the push.

    **2. Run the full matrix locally, on a disposable DB:** `bash scripts/coverage_docker.sh` with the
    stack up. **Never** run the gate against the live `aura` DB. Unset `AURA_WEB_AUTH_SECRET` first — the
    `.env` exports it, it flips `SecretConfigured`, and it breaks the config tests.

    **3. Assert the floor twice:** the aggregate is ≥85% **and every owned 37F package individually clears
    85** — `internal/share`, `internal/agui`, `internal/objectstore`, `internal/runner`,
    `internal/cron/handlers`, `internal/config`. The 2026-06-13 campaign floor is per-package, not just
    aggregate. Record each number in the SUMMARY.

    **4. Update `37F-VALIDATION.md`:** populate the Per-Task Verification Map with the real task IDs,
    plans, waves, requirements, threat refs, test types, and automated commands — one row per
    Requirements-to-Test-Map entry. Check off the Validation Sign-Off list. Set `nyquist_compliant: true`
    in the front-matter once every row is green, and `status: complete`.

    If a package is under 85, **close the gap in this plan** — do not defer it. A phase does not close
    below the floor. A green local full-matrix run is worth more than a push-and-wait CI cycle.
  </action>
  <verify>
    <automated>bash scripts/coverage_docker.sh</automated>
  </verify>
  <acceptance_criteria>
    - **Tag audit passes:** `grep -rlE "go:build .*(garage_integration|authula_integration|musr_e2e|docker_integration)" internal/share/ internal/objectstore/ internal/cron/handlers/ internal/config/ internal/runner/runner_delete_share_test.go internal/agui/share_export_test.go internal/agui/share_api_test.go internal/agui/share_cross_identity_test.go internal/agui/share_audit_union_test.go` returns NOTHING.
    - `bash scripts/coverage_docker.sh` exits 0 with the aggregate ≥ 85%.
    - **Every owned 37F package individually reports ≥85.0%** — `internal/share`, `internal/agui`, `internal/objectstore`, `internal/runner`, `internal/cron/handlers`, `internal/config`. Each number is recorded in the SUMMARY.
    - The gate ran against the **disposable** `aura_cov` DB, never `aura` — confirmed by using `coverage_docker.sh` rather than a bare `coverage_gate.sh`.
    - `37F-VALIDATION.md`'s Per-Task Verification Map has one row per Requirements-to-Test-Map entry, each with a real automated command and a green status — no `{TBD-planner}` placeholder remains.
    - `37F-VALIDATION.md` front-matter has `nyquist_compliant: true` and `status: complete`.
    - Every Validation Sign-Off checkbox is ticked, including "No 37F test carries a tag outside `db_integration neo4j_integration`" and "SC4 lives in `internal/agui`, NOT `cmd/aura`".
  </acceptance_criteria>
  <done>No 37F test carries a forbidden tag; the full matrix is green on a disposable DB with the aggregate and every owned package at ≥85%; `37F-VALIDATION.md` is populated, signed off, and marked nyquist-compliant.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| identity A's data → identity B | The whole point of the suite. Every row is one crossing that must be refused (except row 3, where bearer-within-auth is the intended design). |
| a token → the identity-scoped lane | Rows 9 and 10. A token is a capability for one snapshot, not a session. |
| a test that skips → a green CI | A skipped integration test that reports PASS is a falsely-green job exercising nothing. The skip helper's `t.Fatal`-under-CI is the boundary. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-05 | Information Disclosure | cross-identity read via a token (rows 1, 2, 5, 9) | mitigate | Ten-row matrix with body-level assertions; row 5 asserts B's data and every path shape are absent from a real public response. |
| T-37F-52 | Elevation of Privilege | token authenticates then any asset id is fetched (row 9) | mitigate | Row 9 requests B's assetID under A's token and asserts 404. |
| T-37F-05b | Elevation of Privilege | public session leaking into the authenticated lane (row 10) | mitigate | Row 10 requests the identity-scoped download lane with only a public token and asserts 401/302, exercised through the real `RequireAuth` chain. |
| T-37F-06 | Elevation of Privilege | the wildcard making capability assertions vacuous (R-13) | mitigate | Provisioned non-wildcard identities only; row 8 seeds `share.public` for B alone and asserts A's mint is 403. Grep-gated against any wildcard grant. |
| T-37F-01 | Information Disclosure | stale render after revoke (row 7) | mitigate | Row 7 asserts the 404 body omits A's title — bytes, not just a status code. |
| T-37F-62 | Repudiation | a 5-tag test compiling+skipping and reporting a false green (WR-01) | mitigate | Exactly one build tag, enforced by a `head -1` assertion and a phase-wide grep audit; runtime is checked for the sub-second skip tell. |
| T-37F-63 | Repudiation | coverage silently below the floor for a single package | mitigate | The floor is asserted per-package as well as in aggregate, on a real full-matrix run. |
| T-37F-64 | Tampering | the coverage gate truncating the live deployment's tables | mitigate | `coverage_docker.sh` provisions a disposable `aura_cov` and drops it; `coverage_gate.sh:35` refuses the live `aura` DB locally. Never bypassed. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Test-only plan; no dependency added. |
</threat_model>

<verification>
- `go test -tags db_integration -race -p 1 -count=1 -run TestShareCrossIdentityDeny -v ./internal/agui/` — all ten subtests PASS, non-sub-second
- Phase-wide tag audit — no forbidden `go:build` tag in any 37F test
- `bash scripts/coverage_docker.sh` — aggregate ≥85%, every owned 37F package ≥85%
- `37F-VALIDATION.md` — map populated, sign-off ticked, `nyquist_compliant: true`
- `golangci-lint run ./internal/agui/` → 0 issues
</verification>

<success_criteria>
WEBSHARE-04 is satisfied by a test that actually counts: ten cross-identity rows in `internal/agui` under
one tag, with body-level assertions, provisioned non-wildcard identities, and rows 9 and 10 — the two a
naive implementation fails — passing. The whole 37F Go surface sits inside the two-tag coverage gate, with
the aggregate and every owned package at ≥85%.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-13-SUMMARY.md` when done.
Record the ten-row result, the SC4 runtime, and the per-package coverage numbers.
</output>
