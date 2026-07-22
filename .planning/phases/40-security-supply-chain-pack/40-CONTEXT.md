# Phase 40: Security & Supply-Chain Pack - Context

**Gathered:** 2026-07-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Close the security + supply-chain audit findings and prove prompt-injection denial under production. Delivers SEC-01..06 + SEC-09 (F-019-sec / F-021 / F-022 / F-047 / F-051 / F-052 + CodeQL `go/weak-sensitive-data-hashing`):

1. **SEC-04 (F-019 sec)** — deterministic prompt-injection / tool-policy-bypass regression gate proving injected shell/file/network/MCP requests are DENIED under `server_production`, **plus** an LLM injection-resistance eval tier.
2. **SEC-09 (CodeQL)** — remediate the weak-hash finding at `internal/agui/recovery_hash.go`.
3. **SEC-01 (F-021)** — secret redaction before persistence + full-trace production fail-closed policy + encrypted trace sink.
4. **SEC-02 (F-022)** — explicit-allowlist CORS replacing the wildcard.
5. **SEC-03 (F-047)** — validation console refuses non-loopback bind without an explicit unsafe flag + auth.
6. **SEC-05 (F-051)** — CI SBOM + blocking govulncheck + SHA-pinned Actions/tools + a workflow-pin lint gate.
7. **SEC-06 (F-052)** — centralized strict JSON decoding on privileged routes.

**Phase principle (inherited from v2.0.0 §Goal):** each hardening behavior is a **no-op under `dev`/`local_trusted`** and activates under `single_user_hardened`/`server_production`. The operator's daily full-host experience is unchanged.

**Explicitly NOT in this phase:** F-019 *ops* part (load/chaos, backup/DR) → Phase 41; the broader capability-eval + honest-10/10 evidence bundle → Phase 41. This phase may expose seams Phase 41 consumes but must not absorb its acceptance scope.

**Migration expectation:** none of the seven decisions requires a schema migration (token-hash: no backfill under a ≤10-min TTL; encrypted sink: file sink + env key; redaction / CORS / JSON / console: code + config only). If one nonetheless emerges, the next slot is authoritative from `ls internal/db/migrations/ | tail -1` (current head **0049**, NOT the stale 0040 in CLAUDE.md's header) — never deduced.

</domain>

<decisions>
## Implementation Decisions

### SEC-04 — Prompt-injection / tool-policy-bypass suite
- **D-01:** Ship a **deterministic policy-denial CI gate** as the SEC-04 acceptance artifact: a table-driven Go suite driving ~15-25 hand-authored adversarial tool-invocation shapes (the four F-019 classes: shell / file / network / MCP) directly through `gateway.Decide` constructed with `New(config.ProfileServerProduction, nil)`. Assert `Deny`. It runs in normal CI, needs no LLM, no DB, no network, survives no-skip-as-green, and counts toward the 85% floor.
- **D-02:** Corpus is **hand-written, not imported.** The classifier verdicts on tool-name + `Mutating` bit + `action` field — injection *payload text is inert to the verdict*, so an imported attack corpus (garak/promptfoo, or the `system_prompts_leaks/` dirs, which are defender prompts anyway) adds zero signal to the deterministic layer. Include **Allow negative-controls** (`fs_read`, `document_search`, MCP `ReadOnlyHint`, `skill{action:list}`) to prove no over-blocking.
- **D-03:** Drive every deny-case with a **plain `context.Background()`** — a `WithResolvedApproval(ctx,{Approved:true})` context short-circuits `routeApprove` to `Allow` regardless of profile. Keep the DENY corpus to **Risky/Destructive-tier** shapes (all four F-019 classes classify Risky, so the acceptance is satisfiable DB-free); a Normal-tier-mutating tool skips `routeApprove` and falls through to `beginOperation`+`reserve`, which nil-derefs on a nil store.
- **D-04:** **Also build the LLM injection-resistance eval tier this phase** (user chose the thorough scope). Extend the existing `internal/eval` `cot_eval` harness (build tag `cot_eval`, `OPENROUTER_API_KEY`-gated, **NOT CI-blocking** — honors no-unsolicited-paid-runs). This measures whether the model *resists* the injection, the half the deterministic gate cannot prove. Source real payloads from garak `promptinject`/`latentinjection` or promptfoo redteam plugins for this tier only.
- **D-05 (honest-scope statement — MANDATORY in acceptance evidence):** the deterministic gate proves the **enforcement backstop** ("if an injection coerces a mutating call, the PEP denies it fail-closed"), NOT model resistance. State this split explicitly so SEC-04 is not over-claimed; the resistance half is the `cot_eval` tier's job.

### SEC-09 — weak-hash at `recovery_hash.go`
- **D-06:** Switch `HashLookupToken` from plain SHA-256 to **HMAC-SHA-256 with a server-side pepper**, where the pepper is an **HKDF-derived dedicated subkey** from `AURA_AUTHULA_SECRET` (info label e.g. `"aura-reset-token-pepper"`) — **never the raw secret** (that is cross-protocol key reuse; follow Authula's own `DeriveOAuthHMACKey` pattern). Deterministic → the `token_hash` PRIMARY-KEY lookup still works; a DB leak alone can no longer forge/scan tokens. No migration/backfill (≤10-min TTL; in-flight reset tokens invalidated on deploy — document the ≤10-min window). Mint (`serve_password_reset.go:149`, `recovery.go:96,104`) and lookup (`password_reset.go:318`, `identity_recovery.sql:86`) both change coherently in one binary.
- **D-07 (verify-then-fallback):** Run a **local CodeQL scan with CI's exact pinned pack** to confirm HMAC resolves `go/weak-sensitive-data-hashing`. The Go query is new (2.23.7, Dec-2025) and lists only argon2/scrypt/bcrypt/PBKDF2 as remediations — HMAC may still trip the `sha256.New` sink. **If it does NOT clear**, fall back to **documented FP-dismissal** (SEC-08 precedent, Phase 31): justify that a 256-bit CSPRNG token is not a password / has no limited input space, and that the code already matches OWASP + Authula's own `HashRefreshToken` + LibreChat's `hashToken`. Either outcome resolves SEC-09.
- **D-08 (ROADMAP/PRD amendment REQUIRED — do before code, per §Q&A revision protocol):** The success criterion "remediated with a strong salted KDF and the alert resolves" is **architecturally impossible** — a randomly-salted KDF cannot serve a lookup-by-hash PRIMARY KEY. Amend to: *"remediated with a keyed hash (HMAC-SHA-256 with a server-side pepper) where CodeQL accepts it, OR dismissed as a documented false positive (256-bit CSPRNG token, not a password / no limited input space), such that the SEC-09 alert resolves."*
- **Rejected:** fixed-salt argon2id (literal wording but a cryptographic anti-pattern — fixed salt defeats salting — AND a 64 MiB/attempt memory-DoS lever on the unauthenticated `Complete` endpoint); two-column SHA+argon2 (doesn't even clear the alert — the SHA lookup column stays flagged).

### SEC-01 — redaction before persistence + full-trace policy
- **D-09 (enforcement seam — single choke-point):** Redact tool-output at the **one** seam covering the whole remaining surface: `appendTurnWrites`/`postgresTextSafe` in `internal/conversations/store_append.go:213-249` (all three append variants + sidecar spill funnel through it). This mirrors the already-blessed ledger choke-point (`toolinvocations.toParams` → `RedactForLedger`). Do **not** scatter per-writer redaction (re-opens the divergence class).
- **D-10 (detector — layered, tension-resolving):** The turn choke-point is ALSO the LLM re-feed path, so a lossy regex there corrupts the agent's working data. Therefore: **exact-match INBOUND, pattern-based OUTBOUND.**
  - *Inbound* (persisted turn body): exact-match of **boot-harvested configured secrets** — `os.Environ` keys matching the canonical `secret.IsSecretEnvKey`/`IsSecretEnvVar`, values ≥ a length floor, into a boot-time set; `strings.ReplaceAll`. Zero false positives → a connstring the agent *discovered* survives (it's the agent's data), but Aura's OWN DSN/keys never persist verbatim.
  - *Outbound* (display / export / ledger / trace): the existing pattern-based `internal/redact.String` + `RedactForLedger` catch unknown secrets on the exfil surface.
- **D-11 (reasoningtrace bug — FIX ON TOUCH):** `internal/reasoningtrace/reasoningtrace.go` `redactString` (~:232-248) only exact-matches env values whose KEY contains KEY/TOKEN/PASSWORD/SECRET. A `postgres://user:pass@host` DSN in an error field, or `AURA_DB_URL`'s value (key lacks those 4 markers), **leaks in full-trace mode**. Fold `redact.String` into `Record` and align the exact-match markers to the canonical `secret` package. Keep exactly one redaction pass per surface (no double-redaction of `[redacted]`/`[capped]` markers).
- **D-12 (full-trace prod posture):** Add `gateReasoningTraceFull(p)` to `ValidateProfile` (`internal/config/config_validate.go:88-105`, mirroring `gateDestructiveShell`): `AURA_REASONING_TRACE=full` under `RuntimeProfile.Strict()` is **Fatal unless `AURA_TRACE_FULL_ACK=1`**. No-op under dev/local_trusted; prod fails closed unless an operator deliberately acks. Add the net-new knobs (`AURA_REASONING_TRACE` if not already, `AURA_TRACE_FULL_ACK`, `AURA_TRACE_ENCRYPT_KEY`) to the `KnobSpec` registry (`config_knobs.go`).
- **D-13 (encrypted sink — BUILD THIS PHASE, user chose the thorough scope):** Ship a real encrypted full-trace file sink, **reusing the in-tree AES-256-GCM + HKDF idiom** from `internal/objectstore/identity_store.go:5-9,62-69` (KEK HKDF-derived from a key env — do NOT add `filippo.io/age`; `golang.org/x/crypto` is already a direct dep). **Fresh random nonce per record** (GCM fails catastrophically on nonce reuse) — frame per-record, never one nonce for an appending file. Pairs with the D-12 ACK gate + Phase-39's 24h prod trace TTL to bound exposure.

### SEC-02 — CORS (F-022)
- **D-14:** Replace the wildcard `internal/agui/server_cors.go` with an **explicit allowlist** from a new `AURA_AGUI_CORS_ORIGINS` knob. Because auth is a **session cookie** (`setSessionCookie`/`r.Cookie`), a credentialed request cannot use `*`: **echo the matched origin + `Access-Control-Allow-Credentials: true`**, set **`Vary: Origin`**, and keep `Access-Control-Allow-Methods` **synced to registered routes**. Wildcard/permissive is permitted **only under the dev profile with auth disabled**. `config_validate.go:226` already makes permissive CORS Fatal under strict tiers — extend it to also refuse a non-empty allowlist paired with disabled auth outside dev.

### SEC-03 — validation console (F-047)
- **D-15:** `aura mcp console` (`cmd/aura/integrations_console.go`, default `127.0.0.1:9099`, token-injecting proxy in `integrations_proxy.go`) **refuses a non-loopback `--addr` unless BOTH** an explicit `--unsafe-non-loopback` flag **AND** a console token (`AURA_INTEGRATIONS_CONSOLE_TOKEN`) are set; logs a warning; the proxy then requires the token on every request. Loopback bind stays unauthenticated (unchanged happy path). Test: non-loopback fails by default; unsafe mode without a token also fails; unsafe + token binds and logs the warning.

### SEC-06 — strict JSON decoding (F-052)
- **D-16:** Introduce **one centralized strict-decode helper** (size cap + `Content-Type: application/json` check + `DisallowUnknownFields` + single-decode `io.EOF` check + per-route `allowEmpty`). Apply it to the **audit-named privileged acceptance set**: `/agent/run` (`server_run_request.go:decodeRunAgentRequest`), approvals-resolve (`approvals_api.go:124`), onboarding (`onboarding_api.go:279`), assets (`assets_api.go:82`), governance MCP (`governance_write_api.go:decodeMCPBody`), governance skills (`governance_write_skills_api.go:decodeSkillsBody`). Adopt elsewhere **refactor-on-touch** — do not big-bang the ~20+ agui decoders. Fold in the existing `allowEmpty`/`io.EOF` pattern already at `conversations_api.go:162`.

### Claude's Discretion
- Exact new file placement, helper names, error strings, and test-fixture shapes, provided the contracts above hold and no file exceeds 600 LOC.
- The precise length floor for the boot-harvested secret set (D-10) and the exact HKDF info labels (D-06/D-13), provided they are domain-separated and documented.
- Per-route `allowEmpty` and size-cap values for D-16 (sane defaults; e.g. reuse existing `maxRunBodyBytes`/`schedulerEditBodyCap` precedents).
- The exact pinned tool versions for D-24 and whether to move `go install` tools into go.mod `tool` directives (recommended, so Dependabot's gomod ecosystem tracks them and they don't rot like `golangci-lint@v2.12.2`/`sqlc@v1.31.1`).
- Wave ordering across the seven items (they are largely independent; SEC-09's amendment gates only SEC-09's code).

### SEC-05 — supply-chain (locked by research as minimal-industrial)
- **D-17 (SBOM):** Add a `sboms:` block to `.goreleaser.yaml` (syft, **SPDX-JSON**, one doc per release, attached to the GitHub Release + checksummed — covers the shipped binary + `web/` dir). Add a **pinned syft install step** to `release.yml` before goreleaser (`goreleaser-action` does NOT install syft; the block silently no-ops without it). Per-release, not per-commit; no image-level attestation this phase.
- **D-18 (govulncheck):** Keep **reachable-any = block** (the `vulncheck` job already runs `govulncheck $(scripts/go_packages.sh)`, non-zero on any reachable finding — strictly stronger than the audit's "high-severity" wording). Add a job comment stating the intent. Pin the govulncheck tool to an exact version. Do NOT add osv-scanner (no reachability; alert fatigue).
- **D-19 (workflow-pin lint gate):** Ship a **custom grep gate** `scripts/workflow_pin_gate.sh` + a blocking CI job — dodges the "pin the linter itself" bootstrap paradox and matches the existing `scripts/quality_snapshot_gate.sh`/`go_packages.sh` script-gate precedent. Two independent checks: (a) every `uses:` is `@<40-hex-sha> # vX.Y.Z`, accepting local `./…` and `docker://…@sha256:…`; (b) no `go install …@latest` on `run:` lines. Optionally layer zizmor later as pinned, advisory defense-in-depth.
- **D-20 (pin scope):** **Pin everything to SHA** (incl. `actions/*`/`github/*`) with a trailing `# vX.Y.Z` comment — the only option meeting "all Actions SHA-pinned" + OpenSSF Scorecard / GitHub secure-use / SLSA 2026 guidance. The comment form is **mandatory** (Dependabot only updates SHA pins that carry it — a bare SHA rots blind). `.github/dependabot.yml`'s `actions` group already keeps them current. 68 `uses:` lines / 13 distinct refs across ci/codeql/release/skills; prioritize genuine third-party first (`crazy-max/ghaction-github-runtime`, `dorny/paths-filter`, `goreleaser/goreleaser-action`), then Docker-org, then GitHub first-party. Pin the 3 `@latest` tool installs (deadcode ci.yml:80, govulncheck ci.yml:160, go-mutesting skills.yml:133) to exact versions; pin `init`+`analyze` codeql-action to the **same** SHA.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §SEC — SEC-01..06 + SEC-09 (the locked WHAT; the SEC lines read as spec).
- `.planning/ROADMAP.md` §"Phase 40: Security & Supply-Chain Pack" (line ~723) — goal + 5 success criteria. **SEC-09's "salted KDF" criterion needs the D-08 amendment.**
- `prd.md` — truth-source (PRD-first); runtime profiles, secret handling, CORS loopback default (amendment #35).
- `docs/audit/bug-report.md` — F-019 (:347), F-021 (:386), F-022 (:403), F-047 (:874), F-051 (:956), F-052 (:975): evidence, impact, recommended fix, suggested test coverage per finding.

### SEC-04 — injection suite (gateway seam)
- `internal/gateway/decide.go`, `internal/gateway/gateway.go` (constructor `New(profile, store)` ~:128 — profile is a plain arg, no env plumbing), `internal/gateway/approve.go` (`routeApprove` deny branch ~:91-131 + the `resolvedApproval(ctx).Approved` fast-path escape), `internal/gateway/classify.go` (name + Mutating + action → verdict).
- `internal/config/config_runtimeprofile.go` (`Strict()` ~:56-57).
- `internal/eval/` — `harness_cot_eval_test.go`, `dataset_cot_eval.go`, `judge_cot_eval.go`, `doc.go` (build tag `cot_eval`, `OPENROUTER_API_KEY`-gated, not CI). The LLM tier (D-04) extends this.
- `D:/tmp/agent-infra-sandbox/evaluation/README.md` — the F-019-cited capability-eval taxonomy (scenario classes: shell/file/browser/MCP/error/workflow; XML task shape with exact/regex/free-text expectations). Borrow the **class taxonomy + pass/fail structure**, NOT its LLM runner.
- `D:/tmp/spike-librechat/packages/api/src/agents/hitl/policy.spec.ts` — deterministic table-driven approval-policy test; the external precedent for the D-01 pattern. (Codex `D:/tmp/codex` = 0 `.rs` files, no precedent; nanobot/picobot none.)

### SEC-09 — weak-hash
- `internal/agui/recovery_hash.go` — `HashLookupToken` (:67, the sink); argon2id helpers already present.
- Call sites: `cmd/aura/serve_password_reset.go:149,295-301,531`; `cmd/aura/recovery.go:96,104`; `internal/agui/password_reset.go:318,407`; `internal/db/queries/identity_recovery.sql:86`; `internal/db/migrations/0023_identity_recovery.up.sql:28` (`token_hash text PRIMARY KEY`). **No new migration.**
- Pepper source: `AURA_AUTHULA_SECRET` (`internal/config/config.go:158`, `Secret: true`).
- Precedent (cite in the FP-dismissal doc if D-07 falls back): `D:/tmp/authula/plugins/jwt/services/refresh_token_service.go:263` (`HashRefreshToken` = plain SHA-256), `D:/tmp/authula/internal/repositories/crypto_token_repository.go:38-42`, `D:/tmp/authula/internal/security/hmac_signer.go` + `plugins/oauth2/plugin.go:105` (`DeriveOAuthHMACKey` — the subkey-derivation pattern to mirror), `D:/tmp/spike-librechat/packages/data-schemas/src/crypto/index.ts:21` (`hashToken` = SHA-256).
- CodeQL query: https://codeql.github.com/codeql-query-help/go/go-weak-sensitive-data-hashing/ ; Go query added 2.23.7 (https://github.blog/changelog/2025-12-18-codeql-2-23-7-and-2-23-8-add-security-queries-for-go-and-rust/) ; OWASP Forgot-Password + Password-Storage (pepper) cheat sheets.

### SEC-01 — redaction + trace
- Choke-point: `internal/conversations/store_append.go:213-249` (`appendTurnWrites`/`postgresTextSafe`); callers `AppendTurn`/`AppendTurnTx`/`AppendAssistantTurnWithCacheMetric` + `maybeSpill` (:219).
- Write path: `internal/runner/runner_persist.go:122-129` (RoleTool result), :173/:266/:366.
- Existing redactors: `internal/redact/string.go` (`String`, credential-URL/userinfo/token regex), `internal/secret/envkey.go` (`IsSecretEnvKey`/`IsSecretEnvVar`/`ContainsCredentialURL` — the canonical predicate), `internal/toolinvocations/store.go:146,150` (`RedactForLedger`, the blessed ledger choke-point), `internal/reasoningtrace/reasoningtrace.go:99,106,157,232-248` (the D-11 gap).
- Full-trace gate: `internal/config/config_validate.go:88-105` (`gate*` matrix, `gateDestructiveShell` template), `internal/config/config_knobs.go` (`KnobSpec` registry for the net-new knobs).
- Encrypted-sink idiom to reuse: `internal/objectstore/identity_store.go:5-9,62-69` (AES-256-GCM + HKDF-from-secret); authula alt `crypto_token_repository.go:11,47` (XChaCha20-Poly1305). `golang.org/x/crypto` already a direct dep.
- Reference posture: `D:/tmp/spike-librechat/packages/data-schemas/src/config/parsers.ts:41-74,350-368` (log-layer-only redaction, store faithful — validates outbound-only); `D:/tmp/aura-pim-mcp/docs/telemetry.md:349-398` (privacy-first always-redacted set on the export boundary); OTel GenAI semconv 2026 (content capture opt-in + redacted). Phase-39 `39-CONTEXT.md` D-11/D-15 (content-capture opt-in; full-trace 24h prod / 7d dev TTL).

### SEC-02 / SEC-03 / SEC-06
- CORS: `internal/agui/server_cors.go` (`withCORS`), `internal/agui/auth.go:121,141,152,256` (session cookie → credentialed CORS), `internal/config/config.go:118-122,438` + `config_knobs.go:77` (`AGUICORSPermissive`), `config_validate.go:226` (permissive Fatal under strict — extend).
- Console: `cmd/aura/integrations_console.go:39-51` (`--addr`, `newConsoleHandler`), `cmd/aura/integrations_proxy.go` (server-side token injection; `pimAdminTokenDefault`).
- Strict JSON: `internal/agui/server_run_request.go:26`, `approvals_api.go:124`, `onboarding_api.go:279`, `assets_api.go:82`, `governance_write_api.go:193`, `governance_write_skills_api.go:218`; fold-in precedent `conversations_api.go:162` (`io.EOF`/allowEmpty), `governance_write_scheduler.go:131` + `connect_api.go:95` (`io.LimitReader`).

### SEC-05 — supply-chain
- `.github/workflows/{ci.yml,codeql.yml,release.yml,skills.yml}` (68 `uses:`, zero SHA pins; tool installs ci.yml:80/160, skills.yml:133; `vulncheck` job ci.yml:145-161; pinned precedents golangci-lint@v2.12.2 ci.yml:85, sqlc@v1.31.1 ci.yml:465).
- `.goreleaser.yaml` (add `sboms:`), `.github/dependabot.yml` (already supports SHA+comment pins — no change), new `scripts/workflow_pin_gate.sh`.
- Refs: OpenSSF post-tj-actions maintainers' guide; GitHub Actions secure-use reference; zizmor `unpinned-uses`; CycloneDX/cyclonedx-gomod; goreleaser sboms+syft (IMTI/Sbomify).

### Cross-phase contracts (read, do not redefine)
- `.planning/phases/35-toolgateway-policy-engine/35-CONTEXT.md` — the `gateway.Decide` reservation/classifier contract SEC-04 drives.
- `.planning/phases/33-*/33-CONTEXT.md` (Runtime profiles) + `.planning/phases/38-mcp-governance-hardening/38-CONTEXT.md` — the no-op-under-dev / harden-under-prod profile-gating pattern (`AURA_MCP_SSRF_ENFORCE` template) every SEC knob follows.
- `.planning/codebase/STACK.md`, `ARCHITECTURE.md`, `CONCERNS.md`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `gateway.Decide` (Phase 35) is deterministic + DB-free with `nil` store — the SEC-04 gate needs no fixtures beyond the profile arg.
- `internal/redact` + `internal/secret` + `toolinvocations.RedactForLedger` — the outbound redactors + canonical secret predicate; reuse, do not invent a fourth list.
- `internal/objectstore/identity_store.go` AES-GCM+HKDF — the encrypted-sink primitive already in-tree.
- `config_validate.go` `gate*` matrix + `gateDestructiveShell` — the exact template for D-12's `gateReasoningTraceFull`.
- `conversations_api.go:162` `io.EOF`/allowEmpty + `governance_write_scheduler.go:131` `io.LimitReader` — partial strict-decode patterns to unify into the D-16 helper.
- `scripts/quality_snapshot_gate.sh` / `scripts/go_packages.sh` — the script-gate precedent for D-19.
- `.golangci.yml`, `Makefile` `vuln`/`quality`/`coverage` targets, `scripts/coverage_gate.sh` / `scripts/coverage_docker.sh` — gate wiring.

### Established Patterns
- No-op-under-dev / harden-under-prod, env-knobbed (Phase-33/38). Every SEC-01/02/03 knob is default-safe and activates under strict tiers.
- Single canonical choke-point per persistence surface (ledger already does this; SEC-01 extends it to turns).
- Append-only audit inside `db.WithTx`; identity-scoped ownership (Phase-36) preserved on every path touched.
- Coverage: **daemon-free unit tests are mandatory for new logic** — the SEC-04 gate, the strict-decode helper, the CORS allowlist matcher, the console bind-guard, the secret-harvest set, and the HMAC/pepper hasher are all pure and MUST carry unit tests (owned-surface floor 85%, verify the **db_integration-only Skills-job number** at phase close). No `docker_integration`-only coverage credit.

### Integration Points
- SEC-04 gate → new `_test.go` under `internal/gateway/` (or a dedicated `internal/gateway/injectionsuite` if it grows); LLM tier → `internal/eval` `cot_eval` tag.
- SEC-09 → `recovery_hash.go` + its 4 call sites, coherent in one binary; local CodeQL run before commit; ROADMAP/PRD amendment commit first.
- SEC-01 → `store_append.go` choke-point + `reasoningtrace.Record` + `config_validate.go`/`config_knobs.go` + a small encrypted-sink pkg.
- SEC-02 → `server_cors.go` + config + `config_validate.go`.
- SEC-03 → `cmd/aura/integrations_console.go` + proxy.
- SEC-06 → new strict-decode helper in `internal/agui` + the 6 acceptance-set call sites.
- SEC-05 → the 4 workflow files + `.goreleaser.yaml` + new lint script + a new blocking CI job.

</code_context>

<specifics>
## Specific Ideas

- User explicitly chose the **thorough** option on every researched fork (encrypted sink built now, LLM eval tier built now, HMAC real-fix over dismissal) — consistent with the honest-10/10 bar and "user finishes what they start". Do not down-scope to the minimal variant.
- User asked to **mine D:/tmp reference repos + best 2026 industrial patterns** for every area — the canonical_refs above cite the specific files/URLs found; the planner/researcher should read them rather than re-deriving.
- SEC-04's honest-scope caveat (D-05) must appear in the acceptance evidence — the enforcement gate is not an injection-resistance proof.
- Pin posture puts Aura **ahead** of its peer AI-runtime set (librechat/authula/agent-memory all use floating tags) — this is a raise-the-bar decision, not parity.

</specifics>

<deferred>
## Deferred Ideas

- **Image-level SBOM attestation** (scan the pushed appliance image with syft; goreleaser `dockers_v2` bypasses `build-push-action sbom:true`) — deferred; D-17 ships binary/archive SBOM only.
- **zizmor as a blocking gate** — ship the custom grep gate now; zizmor can layer later as pinned, advisory defense-in-depth.
- **osv-scanner / container + lockfile vuln scanning** — beyond the Go call-graph; not this phase.
- **F-019 ops half** (load/chaos harness, backup/DR restore drill) + the honest-10/10 evidence bundle → **Phase 41** (OPS/REL).
- **All-routes / every-decoder strict-JSON sweep** — D-16 does the acceptance set + refactor-on-touch; a full sweep is a follow-on if desired.

</deferred>

---

*Phase: 40-security-supply-chain-pack*
*Context gathered: 2026-07-22*
