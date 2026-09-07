<!-- refreshed: 2026-09-07 -->
# Codebase Concerns

**Analysis Date:** 2026-09-07

Every row below was re-verified by reading the file at HEAD on 2026-09-07. Rows are
ordered by real risk. Each states its **evidence** (what was measured) and its
**falsifier** (what observation would retire it).

**Deliberately omitted** because they were checked and found NOT to be problems:
zero `TODO`/`FIXME`/`HACK`/`XXX` comments exist in non-test Go
(`internal/agent/tools/search_files.go:71` is a doc-string example, not a marker);
no non-generated Go file exceeds the 600-LOC ceiling; only `internal/documents` and
`internal/ingestsupervisor` spawn goroutines without a `goleak` test in the package,
and both goroutines are bounded and joined (`internal/documents/jobs_worker.go:201`
joins on a buffered channel after `cancel()`; `internal/ingestsupervisor/process.go:55`
closes `done` after `cmd.Wait()`); the ArcadeDB graph query builder's string
interpolation is fed only by a closed allowlist; the audit register in
`docs/audit/README.md` is empty with `release_ready:true`.

---

## Security Considerations

### 1. The skills installer hands the entire process environment to third-party npm code

- **Files:** `internal/skills/installer.go:378-380` (`execCommandEnv`), called from
  `execCommandRunner` at `internal/skills/installer.go:373`.
- **Risk:** `execCommandEnv()` is literally
  `append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "DO_NOT_TRACK=1")`. The command it
  builds is `npx skills add <source>`, and `npm install` executes the fetched
  package's own lifecycle scripts. Every secret in Aura's environment —
  `POSTGRES_PASSWORD`, `AURA_ARCADEDB_TENANT_SECRET`, `OPENROUTER_API_KEY`,
  `TELEGRAM_BOT_TOKEN`, `AURA_AUTHULA_SECRET`, the composed DSNs — crosses into
  code the operator has just chosen to trust for its *content*, not its
  *lifecycle hooks*.
- **Current mitigation:** the `#nosec G204` comment argues "scripts permitted per
  D-06/D-07 (container = boundary)". That reasoning covers *code execution*, not
  *credential exfiltration*, and the boundary is weak by design (see concern 3).
- **This is a known, already-solved bug class in a sibling call site.** Measured
  2026-09-05 as audit A1 and fixed for the MCP prepare installer: the fix,
  `secret.InstallerEnv`, is documented at `internal/mcp/process_env.go:68-80` in
  exactly these words — *"`uv pip install` and `npm install` execute the package's
  OWN code (setup.py, npm lifecycle scripts), so an installer inheriting Aura's
  environment hands every secret in it to whatever the operator just chose to
  install."* The skills installer was not migrated to it.
- **Fix approach:** replace `execCommandEnv()` with `secret.InstallerEnv()`. The
  helper already exists, already withholds credential-bearing values by reading the
  VALUE (so `https://user:pw@proxy` does not cross), and already keeps the proxy
  keys a resolver needs to reach a registry. This is a one-function change plus a
  unit test asserting that a `t.Setenv`-planted secret is absent from the child env.
- **Falsifier:** show that `execCommandRunner` is never reached with a
  network-fetching subcommand, or that a filtering layer intercepts the env after
  line 373. Neither is true at HEAD — `cmd.Env` is assigned once and unconditionally.

### 2. The Python ingest sidecar also inherits the full environment

- **File:** `internal/ingestsupervisor/process.go:26` —
  `Env: os.Environ()`, launched at `internal/ingestsupervisor/process.go:45` as
  `python -m ingest.app`.
- **Risk:** same class as concern 1, materially lower severity: the payload is
  first-party Aura code, not an operator-chosen third-party package. It still means
  every Aura credential is readable by anything the ingest process imports —
  including its transitive Python dependency tree.
- **Current mitigation:** none in code. The `#nosec` note reads "fixed deployment
  command, private values stay in env", which describes the situation rather than
  constraining it.
- **Fix approach:** an explicit allowlist of the keys the sidecar actually reads,
  in the shape `internal/mcp/process_env.go` already establishes.
- **Falsifier:** an audit of `ingest/` showing it reads a bounded key set, plus a
  narrowed `Env` — then this becomes a closed row rather than an open one.

### 3. The user sandbox is a resource cage, not an isolation boundary — and its caches are shared across identities

- **Files:** `internal/sandbox/usersandbox/translate.go:41-63` (`toHostConfig`),
  `internal/sandbox/usersandbox/spec.go:22-34` (`RuntimeClass`).
- **Measured, at HEAD:**
  - `CapDrop: []string{}` — the container keeps Docker's full default capability
    set. The file says so explicitly: *"D-12: the box is not a jail"*.
  - No `SecurityOpt` (so no `no-new-privileges`, no seccomp/AppArmor tightening),
    no `ReadonlyRootfs`, and no `User` — the pip cache target is `/root/.cache/pip`,
    so the workload runs as UID 0 inside the container.
  - `Runc` is the **zero value** of `RuntimeClass` (`spec.go:23`), and
    `NewSandboxSpec` rejects `Runsc` outside `config.ProfileServerProduction`
    (`spec.go:78`). Every non-`server_production` deployment therefore runs plain
    runc with default caps as root.
  - Three cache volumes — `aura-uv-cache`, `aura-npm-cache`, `aura-pip-cache`
    (`translate.go:20-27`) — are mounted **read-write** into *every* identity's
    sandbox. Only `WorkspaceVol` is per-identity.
- **Risk:** the *cache sharing* is the part not covered by any existing design note.
  One identity's sandbox can write a poisoned wheel or tarball into
  `aura-pip-cache` / `aura-npm-cache` that a *different* identity's sandbox then
  installs. That is a cross-tenant code-execution channel that survives container
  destruction, and it sidesteps the whole per-identity story the memory layer is
  built on. The root+full-caps posture then means the poisoned code has the
  strongest possible position inside the box.
- **What is genuinely well done here, and should not be undone:** host re-exposure
  is *unrepresentable* — `SandboxSpec` has no `Privileged`, no `Binds`, no
  `Devices`, no `NetworkMode`, no docker-socket field (`spec.go:55-56`), and
  `toHostConfig` pins each dangerous moby literal to a safe constant in one place.
  The attack surface is the cache mounts and the in-container privilege level, not
  the host wiring.
- **Fix approach:** make the package caches per-identity (or mount them
  `ReadOnly: true` and populate them from a privileged warmer), and add
  `SecurityOpt: []string{"no-new-privileges"}` plus a non-root `User` unless a
  measured workload requires root.
- **Falsifier:** evidence that the cache volumes are not writable from inside the
  box, or that all deployments are `server_production`+`runsc`. Neither holds at
  HEAD; `runsc` is opt-in and gated.

### 4. `internal/approvalgrants` — the standing-approval store — has no test file at all

- **Files:** `internal/approvalgrants/store.go` (174 LOC, the **only** file in the
  package). `ls internal/approvalgrants/` returns `store.go` and nothing else.
- **Measured:** `scripts/coverage_package_policy.json` pins it at
  `covered_statements: 4, total_statements: 57` = **7.0%**, the lowest in the repo
  by a wide margin. `grep -rln approvalgrants --include='*_test.go'` returns zero
  files anywhere in the tree.
- **Why this is the highest-risk coverage row and not just a number:** this package
  is the durable authorization layer. It stores the per-identity "always approve
  this verb" grants an operator creates from an approval prompt
  (`internal/gateway/grants.go:17`, `cmd/aura/gateway_grants.go:45`,
  `internal/agui/approvals_api.go:60`, wired in `cmd/aura/chat_boot.go:405` and
  `cmd/aura/serve_agui.go:141`). A bug here does not crash — it grants an approval
  that was never given, or fails to revoke one that was.
- **Specific untested invariants:** `withIdentity`
  (`internal/approvalgrants/store.go:58`) must bind `app.current_identity` before
  every table touch, because `aura.gateway_approval_grants` carries the
  fail-closed RLS pair from migration 0087. Its own doc comment refers to "the
  fake-DBTX construction the package's unit tests use" — **those unit tests do not
  exist.** The comment describes a test suite that was never written, which is
  itself evidence this gap is accidental rather than accepted.
- **Fix approach:** the comment names the design — a pool-less `Store` over an
  injected fake `sqlc.Queries`. Write that: assert `withIdentity` refuses a blank
  identity, that every exported method routes through it, and add one
  `db_integration` test proving RLS denies a cross-identity read.
- **Falsifier:** a test file appearing in the package with the ratio moved off
  4/57.

### 5. `internal/webauth` at 51.9% — nearly half the authentication surface is unexercised

- **Files:** `internal/webauth/authula.go` (349 LOC),
  `internal/webauth/mcp_oauth_handlers.go` (247), `mcp_oauth_server.go` (191),
  `mcp_token_plugin.go` (129), `session_validate.go` (106),
  `mcp_oauth_first_party.go` (100), `identity_link.go` (88).
- **Measured:** policy pin `187/360 = 51.9%`, **173 uncovered statements** — the
  second-largest absolute gap in the repo and by far the most security-sensitive
  one. Nine `_test.go` files exist, so this is not "no tests"; it is "the tested
  half is the easy half".
- **Risk:** uncovered branches in session validation, OAuth token verification and
  identity linking are exactly where authentication bypasses live, and they are the
  branches a happy-path test never reaches (expired token, wrong audience, wrong
  issuer, replayed code, identity-link race).
- **Fix approach:** cover the *rejection* paths first, table-driven, before any
  happy-path additions. Prioritise `session_validate.go` and
  `mcp_oauth_handlers.go` by risk-per-uncovered-statement.
- **Falsifier:** a coverage run showing the uncovered 173 statements are all
  live-integration-only glue rather than decision logic. That has not been
  demonstrated.

### 6. Secrets below 12 bytes are never redacted, and redaction is name-heuristic

- **Files:** `internal/secret/redact_exact.go:17` (`configuredSecretMinBytes = 12`),
  `internal/secret/envkey.go:83` (`IsSecretEnvVar`).
- **Measured:** `NewExactRedactor` skips any env value shorter than 12 bytes
  (`redact_exact.go:39`), and otherwise admits a value only if the *key name*
  matches a marker list or the *value* looks like a credential URL
  (`envkey.go:84-97`). A 10-character API key, or a real secret stored under a
  name no marker matches and whose value is not a DSN, is never replaced in any
  at-rest boundary.
- **Current mitigation:** the length floor is a deliberate, documented trade —
  short values would corrupt unrelated working data via substring replacement — and
  longest-first ordering (`redact_exact.go:47-53`) correctly prevents a short
  secret from exposing a longer one's suffix. The design is sound; the *residual*
  is what is being recorded here.
- **Fix approach:** no code change is obviously right. What is missing is a
  **boot-time assertion**: fail loudly if a known-required secret env var
  (`AURA_ARCADEDB_TENANT_SECRET` already enforces >=32 at
  `internal/arcadedb/tenant.go:88`) carries a value under the redaction floor, so
  the gap can never be entered silently.
- **Falsifier:** a config-load check proving no configured secret can be under 12
  bytes. `internal/config/config.go:369` calls `ConfigureExactRedactor(os.Environ())`
  but asserts nothing about length.

### 7. `gosec` G115 (integer overflow on conversion) is excluded repo-wide

- **File:** `.golangci.yml:34-37` — `gosec.excludes: [G115]`, justified as
  "noisy on int32 pool fields with safe sources; quiet it for now".
- **Measured:** 317 numeric conversions in non-test `internal/` code are outside
  any overflow check. The suppression is global, so conversions whose source is
  *not* a safe pool field — a DB-returned count, a parsed request field, a
  model-supplied limit — are unexamined too.
- **Risk:** a truncating `int64 -> int32` on a value derived from user or model
  input turns a bounds check into a bypass. Low likelihood, but the suppression is
  blanket, and the word "for now" has outlived its slice.
- **Fix approach:** remove the global exclusion; re-suppress the genuinely safe
  pool-field sites with targeted `//nolint:gosec // G115` and a one-line reason.
  That converts an unbounded blanket into a bounded, reviewable list.
- **Falsifier:** an inventory showing all 317 conversions have provably bounded
  sources.

---

## Tech Debt

### 8. `depguard` was scheduled and never landed; architecture boundaries are unenforced

- **File:** `.golangci.yml:3-6` — *"architecture-boundary rules (depguard) land in
  the slice that introduces multi-layer imports (Slice 0.9 — Agent runtime)"*.
- **Measured:** `grep -n depguard .golangci.yml` matches **only that comment**.
  `depguard` is absent from the `enable:` list. Slice 0.9's agent runtime shipped
  long ago — `internal/agent` is now one of the largest packages in the tree.
- **Impact:** nothing mechanically prevents a layering violation (a low-level
  package importing the gateway, a store importing the agent). The layering
  described in `ARCHITECTURE.md` is a convention held by review alone.
- **Fix approach:** enable `depguard` with the rules the architecture already
  implies; expect to fix a handful of existing edges as the first commit.
- **Falsifier:** finding depguard configured elsewhere (a second config, a CI step).
  Nothing in `.github/workflows/` or `Makefile` references it.

### 9. The committed `internal/webui/dist` has no freshness gate

- **Files:** `internal/webui/embed.go:15` (`//go:embed all:dist`),
  `internal/webui/dist/` (committed), `web/src/` (source),
  `.github/workflows/ci.yml:1741-1747`.
- **Measured:** CI *rebuilds* the dist for the web E2E job
  (`cd web && npm ci && npm run build`, then `go build -o aura ./cmd/aura`), but it
  never compares the freshly built output against the committed
  `internal/webui/dist`. `grep -rn 'webui/dist' .github/workflows/` returns only
  path-filter entries (lines 1479, 1525, 1645) and the backstop script
  `scripts/web_filter_backstop.sh`; no `git diff --exit-code` follows any build.
- **Impact:** a commit that changes `web/src` without rerunning the manual Windows
  `npm run build` passes CI green — because CI builds its own copy — while every
  *release* binary embeds the stale committed dist. The failure is invisible until
  someone loads the shipped cockpit. As of 2026-09-07 the two are in sync (both
  last touched in the same commit), so this is a latent process gap, not an active
  defect.
- **Fix approach:** after the existing CI build step, add
  `git diff --exit-code -- internal/webui/dist` with a message naming
  `cd web && npm run build` as the fix.
- **Falsifier:** a CI step that diffs or regenerates-and-verifies the committed
  dist. None exists at HEAD.

### 10. Substantial uncommitted production code in the working tree

- **Files (untracked, production, not `_test.go`):**
  `internal/arcadedb/memory_graph_temporal.go`,
  `internal/arcadedb/memory_mentions_read.go`.
  Plus 18 modified tracked files across `internal/arcadedb/`, `cmd/arcadedb-mcp/`
  and `internal/skills/embed/memory-aura/SKILL.md`, and 12 untracked test files.
- **Impact:** the temporal-memory work is real, non-trivial code that no gate has
  run against — it is outside `git log`, outside CI, and outside the coverage
  denominator. It also means this document's HEAD-based findings and the working
  tree disagree about what `internal/arcadedb` contains.
- **Fix approach:** land it as atomic commits per CLAUDE.md §Commit discipline, or
  stash it. Either way it should not persist as ambient uncommitted state across
  sessions.
- **Falsifier:** `git status --porcelain` coming back clean.

### 11. Named coverage debt, ranked by uncovered statements

All figures are exact pins from `scripts/coverage_package_policy.json` — measured
non-regression baselines, not estimates. The repo aggregate is 86.9% against an
85% floor, so these are *local* debts under a *passing* global gate, which is
precisely why they need naming: the aggregate will never surface them.

| Package | Ratio | Uncovered | Note |
|---|---|---|---|
| `internal/approvalgrants` | 4/57 = **7.0%** | 53 | see concern 4 — no test file exists |
| `internal/webauth` | 187/360 = **51.9%** | 173 | see concern 5 — auth surface |
| `internal/objectstore` | 329/476 = 69.1% | 147 | largest non-security gap |
| `internal/assets` | 700/871 = 80.4% | 171 | most uncovered statements after webauth |
| `internal/documents` | 668/806 = 82.9% | 138 | |
| `internal/db` | 269/322 = 83.5% | 53 | |
| `internal/objectstore/garageadmin` | 85/111 = 76.6% | 26 | |
| `internal/multimodal` | 107/127 = 84.3% | 20 | within 1pt of target |
| `internal/tracesink` | 47/56 = 83.9% | 9 | within 2pts of target |
| `internal/procgroup` | 3/4 = 75.0% | 1 | one statement; trivially closable |

Two packages are **delegated** and are deliberately absent from this table because
this tier cannot execute their denominator:
`internal/sandbox/usersandbox` -> `docker_coverage`, and `internal/arcadedb` ->
`arcadedb_coverage`. Their floors are the same 85%, measured by separate
release-blocking reports. Do not average or concatenate those denominators with the
ones above.

---

## Fragile Areas

### 12. Files sitting one edit away from the 600-LOC ceiling

- **Measured** (non-test, non-generated, `wc -l`):
  `internal/askuser/store.go` **600**, `cmd/aura/serve.go` 598,
  `internal/arcadedb/memory.go` 597, `internal/agent/llm_agent.go` 596,
  `internal/config/config.go` 594, `cmd/aura/main.go` 591,
  `cmd/aura/chat_boot.go` 590, `internal/runner/runner_persist.go` 589,
  `internal/llm/config.go` 582, `internal/arcadedb/memory_recall.go` 581.
- **Why fragile:** `internal/askuser/store.go` is *exactly at* the ceiling — the
  next line added to it violates CLAUDE.md §No-god-class and trips the file-size
  gate in `make quality`. Nine more files are within 20 lines. Any of them will
  force an unplanned split mid-task.
- **Safe modification:** split *before* editing, not after the gate fires. The
  convention is already established — `<name>_<concern>.go`, as
  `internal/arcadedb/memory_graph_path.go` / `memory_mentions_link.go` demonstrate.
- **Note:** the only files genuinely over 600 are `internal/db/sqlc/*` (933, 907,
  834, 771, 727, 645), which are sqlc-generated and correctly excluded both from
  linting (`.golangci.yml` exclusion path) and from the coverage denominator.

### 13. The ArcadeDB graph query builder is string-concatenated, guarded from a distance

- **Files:** `internal/arcadedb/memory_graph_temporal.go:25-33` (**untracked** —
  see concern 10), guarded by `memoryGraphRelations` at
  `internal/arcadedb/memory_graph.go:31-45`.
- **Verified safe at HEAD:** `relations` is *not* injectable. It comes from a closed
  four-arm switch returning only the literals `"FACT"`, `"MENTIONS"` and
  `"FACT,MENTIONS"`, with a `default:` that errors. `depth` is bounds-checked to
  1..6 (`memory_graph_path.go:79`), `direction` is a three-arm switch
  (`memory_graph_path.go:86-94`), and entity names stay `$source`/`$target`
  parameters. The in-file comment — *"Only validated type names, punctuation and
  depth enter the pattern; names and dates remain parameters"* — is accurate.
- **Why it is still listed:** the safety property lives in a *different file* from
  the concatenation. `readMemoryGraphPath` accepts `relations string` with no
  local check; it is safe only because its single caller
  (`memory_graph_path.go:115`) happens to have validated it. A second caller added
  without that discipline would be an injection, and nothing in the signature
  would object.
- **Safe modification:** change the parameter from `string` to a typed enum, so the
  guarantee travels with the value instead of with the call site.
- **Falsifier:** this row retires the moment `relations` is a type rather than a
  string.

### 14. Symlink-traversal security tests skip silently on the Windows dev host

- **Files:** `internal/skills/writer_traversal_security_test.go:190`,
  `internal/conversations/store_sidecar_fence_test.go:175` — both
  `t.Skipf("symlinks unavailable on this platform: %v", err)` when
  `os.Symlink` fails.
- **Risk:** these are the tests that prove the symlink-escape guards work
  (`internal/sandbox/usersandbox/materialize_stage.go:153` checks
  `d.Type()&fs.ModeSymlink`; `materialize.go:193` and `:324` reject `..` and
  non-absolute paths). On the Windows primary host they do not run, so a developer
  gets a green local suite that proved nothing about the guard. Under Linux CI they
  do run, which is what keeps this at *fragile* rather than *unmitigated*.
- **Safe modification:** run these in WSL or CI before trusting a local green.
- **Falsifier:** confirming Linux CI executes both (it should — this is a
  platform-capability skip, not an env-gated one, and it is the correct pattern).

### 15. One env-gated skip lacks the CI fail-closed guard

- **File:** `internal/db/migrate_0094_integration_test.go:72` —
  `t.Skipf("no identity seeded in the fresh database: %v", err)`.
- **Measured:** the repo's no-skip-as-green discipline is otherwise *very* well
  applied — 119 packages use `goleak`, and the
  `if os.Getenv("CI") != "" { t.Fatal(...) }` guard appears throughout
  (`internal/assets/store_test.go:26`, `internal/breakglass/breakglass_integration_test.go:68`,
  `cmd/aura/serve_deprovision_memory_integration_test.go:34`, and ~20 more).
  This one skip is keyed on *database state* rather than env, so no guard fires: if
  the migration test's fixture stops seeding an identity, the test skips and CI
  stays green while migration 0094 is never exercised.
- **Fix approach:** `t.Fatal` on the missing identity under `$CI`, matching the
  sibling pattern; or seed the identity in the test rather than querying for one.
- **Falsifier:** a CI run showing this test executing its assertions rather than
  skipping. A sub-second runtime for it is the tell.
- **Explicitly NOT a concern:** the other unguarded skips were read and are
  legitimate documented manual gates — `internal/cron/e2e_test.go:112` (paid live
  LLM, `cot_eval` tag, header explicitly reasons about no-skip-as-green),
  `internal/channels/telegram/artifact_live_e2e_test.go:59` (messages a real
  person, `live_e2e` tag), `internal/llm/openai_compat/ollama_live_e2e_test.go:19`
  (operator-authorized). These are correctly excluded from CI by build tag, not
  falsely green in it.

---

## Known Bugs

None confirmed reproducible at HEAD. The audit register at `docs/audit/README.md`
is empty (`Current unresolved: 0`, `release_ready:true`, last re-measured
2026-08-25), and no `TODO`/`FIXME`/`HACK` marker exists in non-test Go to point at
a known-broken path.

---

## Performance Bottlenecks

None measured in this pass. Two structural notes worth carrying, neither with a
measurement behind it — they are flagged as **inferred, not measured**, and should
not be acted on until profiled:

- `internal/arcadedb/memory_graph.go:48-60` (`memoryGraphPreflight`) walks the
  whole schema's record counts before every native graph procedure, because those
  procedures load every vertex before returning a bounded result. The file itself
  documents this; the cost scales with schema size, not result size.
- `internal/documents/jobs_worker.go:216` (`maintainLease`) heartbeats at
  `leaseDuration()/3`, so lease-renewal write volume is inversely proportional to
  the configured lease. A short lease under many concurrent jobs multiplies
  Postgres writes.

---

## Scaling Limits

- **ArcadeDB: one database and one server user per identity**
  (`internal/arcadedb/tenant.go:36-53`, `:99-104`). This is the correct isolation
  design and is verified server-enforced — the file records the live
  `SecurityException` (*"User 'aura_memory' is not allowed to access database
  'tenant_probe'"*) as evidence. The scaling consequence is that identity count is
  now ArcadeDB *database* count and *server-user* count, which is a far harder
  resource than a row count.
- **No rotation path for `AURA_ARCADEDB_TENANT_SECRET`.** Passwords are derived
  `HMAC-SHA256(secret, "arcadedb-tenant\x00" || database)`
  (`internal/arcadedb/tenant.go:107-112`). `grep -rni rotate internal/arcadedb cmd/aura`
  on non-test code returns exactly one hit — a comment at `tenant.go:70` saying
  there is "no per-tenant secret to keep, rotate or leak". True, but it means
  rotating the *server-side* secret invalidates **every** tenant credential at
  once, and ArcadeDB offers no way to re-scope an existing user (`ALTER USER` only
  changes a password). A rotation is therefore a sweep of `ALTER USER` across all
  tenants with no implementation and no test. **Falsifier:** a rotation command or
  runbook. Neither exists at HEAD.

---

## Dependencies at Risk

None identified. `make vuln` runs `govulncheck ./...` as the CI `vulncheck` job,
and the ArcadeDB floor for CVE-2026-44221 is enforced in code — `VerifySecureVersion`
refuses to start below 26.4.2.

---

## Missing Critical Features

- **No admin credential degrades silently to warn-and-continue.**
  `cmd/arcadedb-mcp/main.go:94-105`: if the admin config parses but
  `arcadedb.New` fails, the code logs `Warn("admin credential unusable; databases
  must be pre-provisioned")` and proceeds with `admin = nil`. Every subsequent call
  for an un-provisioned identity then fails at request time rather than at boot.
  This is a deliberate design ("No admin credential is a supported configuration"),
  but the *unusable-credential* case is a misconfiguration, not a configuration,
  and it is indistinguishable at boot from the intended one. The adjacent
  `NewTenantCredentials` failure is handled correctly — it fails **closed** at boot
  with an explicit comment about not silently sharing one credential
  (`main.go:106-112`). The admin path should match that posture.
- **Default MCP OAuth issuer is plaintext HTTP.**
  `cmd/arcadedb-mcp/auth.go:67` defaults `MCP_OAUTH_ISSUER` to
  `http://localhost:9080` and `MCP_OAUTH_RESOURCE` to `http://localhost:8096/mcp/`,
  while `cmd/arcadedb-mcp/main.go` defaults the bind host to `0.0.0.0`. Bearer auth
  *is* unconditionally required (`protectedArcadeMCP` wraps every `/mcp` route at
  `main.go:118-121`), so this is not an open server — but a deployment that binds
  all interfaces and forgets to set the issuer will verify tokens against a
  plaintext endpoint. **Fix:** reject a non-loopback bind host when the issuer
  scheme is `http`.

---

## Test Coverage Gaps

Beyond the ranked table in concern 11:

- **Packages with no `_test.go` file at all:** `internal/approvalgrants`
  (concern 4 — the serious one), `cmd/aura-filecard`, `cmd/aura-ingest-supervisor`.
  Also `internal/db/sqlc` (generated, correctly excluded) and
  `internal/steer/steertest` (a test *helper* package, expected).
- `cmd/aura-filecard` and `cmd/aura-ingest-supervisor` are `main` packages whose
  logic presumably lives in `internal/documents/filecard` and
  `internal/ingestsupervisor` respectively — both of which *are* tested. The risk
  is confined to their argument parsing and wiring. **Priority: Low.**
- **Risk if unaddressed:** for `approvalgrants`, an approval-grant or revocation
  bug ships silently. For the two `main` packages, a wiring regression is caught
  only at runtime. **Priority: High / Low / Low** respectively.

---

*Concerns audit: 2026-09-07*
