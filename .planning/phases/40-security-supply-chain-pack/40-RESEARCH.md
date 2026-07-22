# Phase 40: Security & Supply-Chain Pack - Research

**Researched:** 2026-07-22
**Domain:** Go backend security hardening (in-process policy enforcement, credential hashing, persistence redaction, HTTP hardening) + GitHub Actions supply-chain pinning
**Confidence:** HIGH

## Summary

This phase is unusual: `40-CONTEXT.md` already embeds a full discuss-phase research pass **plus** a three-reviewer adversarial validation, with 45+ file:line citations backing 20 locked decisions (D-01..D-20). Per the task brief, this research does not re-litigate those decisions. Instead it (1) verifies every canonical-ref citation against HEAD, (2) resolves the five items CONTEXT.md left to "Claude's Discretion" with concrete, tool-verified answers, (3) produces the mandatory Validation Architecture section, and (4) documents the five implementation pitfalls the planner was asked to encode.

**Verification result: exceptionally high fidelity.** I independently re-read every file CONTEXT.md cites for SEC-04 (gateway PEP), SEC-09 (recovery_hash.go + 4 call sites), SEC-01 (turns choke-point + tool-result sidecar + reasoningtrace bug + redact/secret packages), SEC-02 (CORS files), SEC-03 (console), SEC-06 (all 8 decoder call sites), and SEC-05 (all four workflow files + goreleaser + dependabot). Every single line-number citation matched exactly, with **one minor mislabel**: the canonical-refs line "`internal/gateway/scoring.go` (:140 `GateRecommended`)" should read `internal/scoring/scoring.go:140` — `GateRecommended` lives in the `internal/scoring` package, not `internal/gateway` (confirmed: `internal/gateway/` has no `scoring.go` file at all). The cited line number and code content are exactly correct; only the package path in the citation is off by one directory. No other drift found.

**Discretion items resolved this session:** exact SHA pins (+ version comments) for all 14 distinct GitHub Action refs (13 existing + the new `anchore/sbom-action/download-syft` needed for D-17), exact current versions for govulncheck/deadcode/go-mutesting, a verdict on go.mod `tool` directives (recommended for deadcode+govulncheck, optional for go-mutesting), the HKDF info-label convention already established in-tree, and per-route `allowEmpty`/size-cap defaults for all six SEC-06 acceptance-set routes (all six already use the existing `maxRunBodyBytes` cap — no new cap value needed).

**One new environment gap surfaced:** the CodeQL CLI is **not installed locally** on this dev machine (only a query-pack cache directory exists). D-07's "run a local CodeQL scan with CI's exact pinned pack" needs this. Fallback: `gh extension install github/gh-codeql` (official, zero-friction since `gh` is already authenticated) — see Environment Availability.

**Primary recommendation:** proceed straight to planning using CONTEXT.md's D-01..D-20 as locked. Use this document to fill in exact versions/SHAs/signatures/file placements and to build the Validation Architecture / VALIDATION.md.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SEC-04 policy-enforcement (injection-denial gate) | API/Backend | — | `gateway.Decide` is a pure in-process function; the deny path never touches DB or browser |
| SEC-09 credential hashing (HMAC pepper) | API/Backend | Database | HMAC computed in Go; the result is looked up as a Postgres PRIMARY KEY (`token_hash`) |
| SEC-01 redaction + trace + encrypted sink | API/Backend | Database / Storage (filesystem) | choke-point lives in the conversations store (Postgres) AND the tool-result sidecar + trace file (local filesystem) |
| SEC-02 CORS removal | API/Backend | Browser/Client | the fix is an HTTP middleware deletion; its effect is observed by the browser's fetch/XHR credentialing behavior |
| SEC-03 validation console bind guard | API/Backend | — | a standalone CLI-launched loopback HTTP proxy, not the main `aura serve` daemon |
| SEC-06 strict JSON decoding | API/Backend | — | pure HTTP request-body parsing, no DB/browser dependency for the helper itself |
| SEC-05 supply-chain (SBOM/govulncheck/SHA-pins) | CI/CD Pipeline | — | does not fit the Browser/SSR/API/CDN/Database model at all — it is build/release automation. Called out explicitly since the standard 5-tier map has no natural home for it. |

**Note for the plan-checker:** SEC-05 is correctly a *pipeline*-tier concern, not an application-tier one — do not force it into "API/Backend" just to fit the table. Its "runtime" is GitHub Actions, not the `aura` binary.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### SEC-04 — Prompt-injection / tool-policy-bypass suite
- **D-01:** Ship a **deterministic policy-denial CI gate** as the SEC-04 acceptance artifact: a table-driven Go suite driving ~15-25 hand-authored adversarial tool-invocation shapes (the four F-019 classes: shell / file / network / MCP) directly through `gateway.Decide` constructed with `New(config.ProfileServerProduction, nil)`. Assert `Deny`. It runs in normal CI, needs no LLM, no DB, no network, survives no-skip-as-green, and counts toward the 85% floor.
- **D-02:** Corpus is **hand-written, not imported.** The classifier verdicts on tool-name + `Mutating` bit + `action` field — injection *payload text is inert to the verdict*, so an imported attack corpus (garak/promptfoo, or the `system_prompts_leaks/` dirs, which are defender prompts anyway) adds zero signal to the deterministic layer. Include **Allow negative-controls** (`fs_read`, `document_search`, MCP `ReadOnlyHint`, `skill{action:list}`) to prove no over-blocking.
- **D-03:** Drive every deny-case with a **plain `context.Background()`** — a `WithResolvedApproval(ctx,{Approved:true})` context short-circuits `routeApprove` to `Allow` at the very top of the function, *regardless of profile* (`approve.go:92-98`, above the deny gate at `:103`). A plain `Background()` cleanly hits the fail-closed Deny (`approve.go:103-105`) **without panicking** — verified: the only ctx reads on the deny path (`resolvedApproval`, `responderPresent`) are comma-ok/safe. Keep the DENY corpus to **Risky/Destructive-tier** shapes because that is where `scoring.GateRecommended(tier)` fires → `routeApprove` → Deny. **Rationale corrected (adversarial pass):** a Normal-tier-mutating tool (`skill{restore|archive|save_snippet}`, `task{cancel}`) does NOT nil-deref — `beginOperation`/`reserve` are nil-guarded (`reserve.go:36-37,172-176`) and return **Allow**. So the restriction stands (a Normal-mutating shape would fail a `Deny` assertion), but the reason is "Normal-mutating → Allow", not "nil-deref".
- **D-03b (network class — CORRECTED by adversarial pass):** There is **no first-class mutating "network" tool** — `web_fetch`/`web_search` are **non-mutating → classify `scoring.Safe` → ALLOWED by the gateway**, and their egress control lives in the in-tool SSRF/DNS-pin layer (Phase 31 CAP-05), NOT the PEP. So the SEC-04 "network requests DENIED" class must be modeled as **shell-egress** (`shell_exec{cmd: "curl … | sh"}` — Risky regardless of the command string) or a **mutating MCP egress tool** (`!ReadOnlyHint`). The acceptance evidence must state this explicitly: the gateway denies network egress *when it is a shell/MCP mutation*; it legitimately allows `web_fetch`/`web_search`, whose SSRF defense is a separate, already-shipped layer. A deny-case authored with `web_fetch` will get `Allow` and fail.
- **D-04:** **Also build the LLM injection-resistance eval tier this phase** (user chose the thorough scope). Extend the existing `internal/eval` `cot_eval` harness (build tag `cot_eval`, `OPENROUTER_API_KEY`-gated, **NOT CI-blocking** — honors no-unsolicited-paid-runs). This measures whether the model *resists* the injection, the half the deterministic gate cannot prove. Source real payloads from garak `promptinject`/`latentinjection` or promptfoo redteam plugins for this tier only.
- **D-05 (honest-scope statement — MANDATORY in acceptance evidence):** the deterministic gate proves the **enforcement backstop** ("if an injection coerces a mutating call, the PEP denies it fail-closed"), NOT model resistance. State this split explicitly so SEC-04 is not over-claimed; the resistance half is the `cot_eval` tier's job.

#### SEC-09 — weak-hash at `recovery_hash.go`
- **D-06:** Switch `HashLookupToken` from plain SHA-256 to **HMAC-SHA-256 with a server-side pepper**, where the pepper is an **HKDF-derived dedicated subkey** from `AURA_AUTHULA_SECRET` (info label e.g. `"aura-reset-token-pepper"`) — **never the raw secret** (that is cross-protocol key reuse; follow Authula's own `DeriveOAuthHMACKey` pattern). Deterministic → the `token_hash` PRIMARY-KEY lookup still works; a DB leak alone can no longer forge/scan tokens. No migration/backfill (≤10-min TTL; in-flight reset tokens invalidated on deploy — document the ≤10-min window). The online mint (`serve_password_reset.go:149`) and lookup (`password_reset.go:318`, `identity_recovery.sql:86`) share the serve process env, so they stay coherent once the pepper is threaded.
- **D-06b (pepper threading + break-glass guard — REQUIRED, adversarial pass):** `HashLookupToken(token)` currently takes no pepper (`recovery_hash.go:67`), and the secret is **not in scope** at any current caller. Thread the pepper through **three groups**: (1) the `recovery_hash` signature; (2) the serve adapter/service (`recoveryStoreAdapter`/`wirePasswordResetService`/`PasswordResetService` hold only a pool today); (3) **the offline break-glass CLI** — `identity.go:61` calls `identityRecover` **without `cfg`** and `mintBreakGlassToken` (`recovery.go:63`) takes no secret, and `runIdentity` uses `config.LoadDB()` with **no `ValidateProfile` gate** so `AURA_AUTHULA_SECRET` is not guaranteed set in that operator shell. **Failure if unthreaded:** a break-glass token minted under an empty/wrong pepper produces a different `token_hash` than the serve-side lookup computes → PRIMARY-KEY miss → the recovery token is **silently dead-on-arrival**. Add a **hard presence guard** that `AURA_AUTHULA_SECRET` is set on the break-glass path AND is the same value serve uses. (`config.LoadDB` DOES populate `AuthulaSecret` from env via `loadBase`, so the *plumbing* exists — the gap is that the break-glass path never receives it and never checks it.) The verify-by-candidate challenge `code_hash` stays argon2id (`HashOpaqueSecret`) — it is not the SEC-09 sink; the break-glass throwaway challenge hash (`recovery.go:96`) is never read back, so switching it is immaterial.
- **D-07 (verify-then-fallback):** Run a **local CodeQL scan with CI's exact pinned pack** to confirm HMAC resolves `go/weak-sensitive-data-hashing`. The Go query is new (2.23.7, Dec-2025) and lists only argon2/scrypt/bcrypt/PBKDF2 as remediations — HMAC may still trip the `sha256.New` sink. **If it does NOT clear**, fall back to **documented FP-dismissal** (SEC-08 precedent, Phase 31): justify that a 256-bit CSPRNG token is not a password / has no limited input space, and that the code already matches OWASP + Authula's own `HashRefreshToken` + LibreChat's `hashToken`. Either outcome resolves SEC-09.
- **D-08 (ROADMAP/PRD amendment REQUIRED — do before code, per §Q&A revision protocol):** The success criterion "remediated with a strong salted KDF and the alert resolves" is **architecturally impossible** — a randomly-salted KDF cannot serve a lookup-by-hash PRIMARY KEY. Amend to: *"remediated with a keyed hash (HMAC-SHA-256 with a server-side pepper) where CodeQL accepts it, OR dismissed as a documented false positive (256-bit CSPRNG token, not a password / no limited input space), such that the SEC-09 alert resolves."*
- **Rejected:** fixed-salt argon2id (literal wording but a cryptographic anti-pattern — fixed salt defeats salting — AND a 64 MiB/attempt memory-DoS lever on the unauthenticated `Complete` endpoint); two-column SHA+argon2 (doesn't even clear the alert — the SHA lookup column stays flagged).

#### SEC-01 — redaction before persistence + full-trace policy
- **D-09 (enforcement seams — TWO at-rest surfaces, corrected by adversarial pass):** The `conversation_turns` choke-point `appendTurnWrites`/`postgresTextSafe` (`store_append.go:213-249`) is verified as the single builder for the turns table (one sqlc caller `insertTurnAndAggregates:252`; all append variants + `ForkBranch` + the pause/flush path route through it; SSE is transport-only). The redactor **must sit at `store_append.go:218`** (computes `safeContent` before `maybeSpill`) so it covers **inline AND the conversations sidecar** — redacting only `maybeSpill`'s inline branch re-opens the spill bypass. **BUT the turns table is NOT the whole tool-output surface:** `agent/tools/result.go NewResult` writes the **FULL** tool-output bytes to `<run_dir>/…/<spill-id>.result` (mode 0o600) during tool execution; only a capped **preview** reaches the turn. The ledger's `ResultSidecarPath` (`toolinvocations/store.go:154`) just points at that unredacted file. So a large `env`/`cat .env` dump lands Aura's own secrets **verbatim at rest** in the `.result` sidecar, bypassing every seam. **This phase must redact the `.result` sidecar write too** (apply the same inbound detector at the `NewResult` write path). Do **not** scatter per-writer regex — reuse the one detector at both at-rest seams.
- **D-10 (detector — layered; exact-match at ALL at-rest surfaces + requirement amendment):** The turn choke-point is ALSO the LLM re-feed path, so a lossy regex there corrupts the agent's working data. Therefore: **exact-match INBOUND (at every at-rest surface: turn body, spill, `.result` sidecar), pattern-based OUTBOUND.**
  - *Inbound* (turn body + spill + `.result` file): exact-match of **boot-harvested configured secrets** — `os.Environ` keys matching the canonical `secret.IsSecretEnvKey`/`IsSecretEnvVar`, values ≥ a length floor, into a boot-time set; `strings.ReplaceAll`. Zero false positives → a connstring the agent *discovered* survives (it's the agent's data), and Aura's OWN DSN/keys never persist verbatim **at any at-rest surface** (this closes the D-09 `.result` leak).
  - *Outbound* (display / export / ledger / trace): the existing pattern-based `internal/redact.String` + `RedactForLedger` catch unknown secrets on the exfil surface.
- **D-10b (REQUIREMENTS/ROADMAP amendment REQUIRED — adversarial pass):** SEC-01's literal wording "redact **secret-like** values before persistence" is pattern language, but exact-match-inbound deliberately does NOT pattern-redact *unknown* secrets at rest (that would corrupt the agent's re-fed working data — the core tension). Amend the requirement to scope it: *"Aura's **configured** secrets are redacted before persistence at every at-rest surface (turn body, spill, tool-result sidecar); unknown secret-like values in tool output are the agent's working data and are pattern-redacted only on the OUTBOUND surface (display / export / ledger / trace)."* Without this amendment SEC-01 is over-claimed. (Consistent with LibreChat: store faithful, redact at the log/export layer.)
- **D-11 (reasoningtrace bug — CONFIRMED real by adversarial pass; FIX ON TOUCH):** `internal/reasoningtrace/reasoningtrace.go` `redactString` (~:232-248) only exact-matches env values whose KEY `Contains` KEY/TOKEN/PASSWORD/SECRET (`:239-243`), and `secret.IsSecretEnvVar` is **not imported/called** here. So `AURA_DB_URL`'s value (key lacks those 4 markers) and a literal `postgres://user:pass@host` DSN in any free-text/error trace field **both leak in full-trace mode** — there is no `redact.String` pattern pass in the current `Record` path. Fold `redact.String` into `Record` and align the exact-match markers to the canonical `secret` package. Keep exactly one redaction pass per surface (no double-redaction of `[redacted]`/`[capped]` markers). **Fix-on-touch coupling:** `AppendTurnTx`'s spill-guard length at `store_append.go:123` is computed independently from the `:218` recompute — when the redactor is inserted at `:218`, mirror it at `:123` or the guard/`maybeSpill` disagree at the cap boundary.
- **D-12 (full-trace prod posture):** Add `gateReasoningTraceFull(p)` to `ValidateProfile` (`internal/config/config_validate.go:88-105`, mirroring `gateDestructiveShell`): `AURA_REASONING_TRACE=full` under `RuntimeProfile.Strict()` is **Fatal unless `AURA_TRACE_FULL_ACK=1`**. No-op under dev/local_trusted; prod fails closed unless an operator deliberately acks. Add the net-new knobs (`AURA_REASONING_TRACE` if not already, `AURA_TRACE_FULL_ACK`, `AURA_TRACE_ENCRYPT_KEY`) to the `KnobSpec` registry (`config_knobs.go`).
- **D-13 (encrypted sink — BUILD THIS PHASE, user chose the thorough scope):** Ship a real encrypted full-trace file sink. The in-tree AES-256-GCM + HKDF idiom (`internal/objectstore/identity_store.go:155-176`) maps to per-record framing — `Seal(nonce, nonce, …)` with a fresh `rand` nonce per call gives "fresh nonce per record" for free — **but those `encrypt`/`decrypt` are unexported `IdentityStore` methods, so this is a ~20-line re-implementation in a small new pkg, NOT a wired call** (the D-13 integration note already says "a small encrypted-sink pkg" — plan to write, not import). Do NOT add `filippo.io/age`; `golang.org/x/crypto` is already a direct dep. **Fresh random nonce per record** (GCM fails catastrophically on nonce reuse) — never one nonce for an appending file. **Key-absence policy = FAIL-CLOSED (adversarial pass):** the sink uses a net-new `AURA_TRACE_ENCRYPT_KEY`; when full-trace is acked but the key is absent/malformed, **refuse to write the trace** (mirror `deriveObjectStoreKey`'s fail-closed `:196-197`) — a plaintext fallback would silently reintroduce the F-021 cleartext-on-disk exposure. Pairs with the D-12 ACK gate + Phase-39's 24h prod trace TTL.

#### SEC-02 — CORS (F-022) — DECISION REVERSED after adversarial pass
- **D-14 (same-origin-only — REVERSED from the credentialed-allowlist first locked):** The adversarial pass showed the credentialed-allowlist has **no production cross-origin consumer** (the cockpit dist is `go:embed`-served same-origin; `CORSPermissive` defaults false and only a dev Vite server is cross-origin), that **`SameSite=Strict` + `Secure`** (`auth_cookie.go:129-130`) already blocks the session cookie on any cross-*site* request (so a credentialed allowlist cannot even enable a separate-domain frontend), and that it would trip the **CSRF re-eval `auth.go:14-17` explicitly warns about** ("SameSite=Strict is the only CSRF control … re-evaluate if a cross-origin write surface is introduced"). "Methods synced to routes" is also not cleanly doable — routes are method-prefixed string patterns across ~15 files with no central registry, and the current hardcoded `"GET, POST, DELETE, OPTIONS"` already silently omits the registered `PATCH`/`PUT` routes (live drift proof). **Therefore: serve same-origin only.** Remove `withCORS` and the `AURA_AGUI_CORS_PERMISSIVE` knob; emit **no CORS headers** at all. This eliminates the wildcard (closing F-022), adds zero CSRF surface, and removes the methods-sync maintenance trap. Simpler and stronger than the allowlist given the evidence. `config_validate.go:226`'s permissive-CORS gate becomes dead code → remove it with the knob (no new `gateCORSAllowlist` needed).
- **D-14b (REQUIREMENTS/ROADMAP amendment REQUIRED):** SEC-02's text ("replaced by explicit origin **allowlists** … sets `Vary: Origin` … methods in sync with routes") was written assuming a cross-origin need that does not exist. Amend to: *"Wildcard/permissive CORS is removed; the cockpit is served same-origin only (no CORS headers). Introducing any cross-origin consumer later requires an explicit credentialed allowlist AND the `auth.go` SameSite/CSRF re-evaluation."* If a dev-only cross-port workflow (Vite :5173→:9080) must survive, it is a documented dev-profile-only escape, never a prod path.

#### SEC-03 — validation console (F-047)
- **D-15:** `aura mcp console` (`cmd/aura/integrations_console.go`, default `127.0.0.1:9099`, token-injecting proxy in `integrations_proxy.go`) **refuses a non-loopback `--addr` unless BOTH** an explicit `--unsafe-non-loopback` flag **AND** a console token (`AURA_INTEGRATIONS_CONSOLE_TOKEN`) are set; logs a warning; the proxy then requires the token on every request. Loopback bind stays unauthenticated (unchanged happy path). Test: non-loopback fails by default; unsafe mode without a token also fails; unsafe + token binds and logs the warning.

#### SEC-06 — strict JSON decoding (F-052)
- **D-16:** Introduce **one centralized strict-decode helper** (size cap + `Content-Type: application/json` check + `DisallowUnknownFields` + **trailing-JSON rejection** + per-route `allowEmpty`). Apply it to the **audit-named privileged acceptance set**: `/agent/run` (`server_run_request.go:decodeRunAgentRequest`), approvals-resolve (`approvals_api.go:124`), onboarding (`onboarding_api.go:279`), assets (`assets_api.go:82`), governance MCP (`governance_write_api.go:decodeMCPBody`), governance skills (`governance_write_skills_api.go:decodeSkillsBody`). Adopt elsewhere **refactor-on-touch** — do not big-bang the ~20+ agui decoders.
- **D-16b (the trailing-JSON idiom — CORRECTED by adversarial pass, WOULD FAIL THE F-052 TEST):** The `conversations_api.go:162` / `decodeMCPBody:196` / `decodeSkillsBody:221` pattern (`Decode(&body); err != nil && !errors.Is(err, io.EOF)`) **IS the F-052 vulnerability, not the fix** — `json.Decoder.Decode` reads only the FIRST value, so `{"class":"trusted_local"}{"ignored":true}` (the audit's exact repro) is accepted. That single-decode idiom is correct **only for the `allowEmpty` (empty-body → `io.EOF` tolerated) half.** The helper MUST add the **second step**: after the first `Decode`, call `dec.Decode(&struct{}{})` and require it to return `io.EOF` (the Alex-Edwards idiom) — none of the three cited references contain this, so do **not** copy them verbatim for trailing-JSON. Two distinct `io.EOF` uses: (a) first-decode-may-EOF = allowEmpty; (b) second-decode-must-EOF = no-trailing.
- **D-16c (scope note):** the acceptance set matches audit F-052 (Evidence names the 3 decoders; Suggested-coverage names the 5 surfaces = these 6 with governance split MCP+skills). The **scheduler-edit** mutation (`governance_write_scheduler.go:131`) is the same weakness class but is **consciously deferred to refactor-on-touch** — flag it in the acceptance evidence so a reviewer knows one governance-write is intentionally outside the gated set this phase.

### Claude's Discretion
- Exact new file placement, helper names, error strings, and test-fixture shapes, provided the contracts above hold and no file exceeds 600 LOC.
- The precise length floor for the boot-harvested secret set (D-10) and the exact HKDF info labels (D-06/D-13), provided they are domain-separated and documented.
- Per-route `allowEmpty` and size-cap values for D-16 (sane defaults; e.g. reuse existing `maxRunBodyBytes`/`schedulerEditBodyCap` precedents).
- The exact pinned tool versions for D-18/D-20 and whether to move `go install` tools into go.mod `tool` directives (recommended, so Dependabot's gomod ecosystem tracks them and they don't rot like `golangci-lint@v2.12.2`/`sqlc@v1.31.1`).
- Wave ordering across the seven items (they are largely independent; each amendment gates only its own requirement's code).

### Amendments required (do BEFORE/WITH the relevant code, per PRD §Q&A revision protocol)
Three REQUIREMENTS/ROADMAP amendments came out of discussion + adversarial validation — the planner must land these first:
1. **SEC-09 (D-08):** "salted KDF" → keyed-hash-or-documented-FP wording (a random-salt KDF cannot serve a lookup-by-hash PRIMARY KEY).
2. **SEC-01 (D-10b):** scope "redact secret-like values before persistence" to *Aura's configured secrets at every at-rest surface*; unknown secrets pattern-redacted outbound only.
3. **SEC-02 (D-14b):** "explicit origin allowlists + Vary/methods-sync" → *same-origin only, no CORS headers* (no production cross-origin consumer exists; SameSite=Strict already blocks cross-site cookies).

### SEC-05 — supply-chain (locked by research as minimal-industrial)
- **D-17 (SBOM — web/ coverage CORRECTED by adversarial pass):** `.goreleaser.yaml` is `version: 2` and has `archives:` (tar.gz + zip), so a `sboms:` block runs. Add a **pinned syft install step** to `release.yml` before goreleaser (`goreleaser-action` does NOT install syft; the block silently no-ops without it). **But the default `sboms:` (`artifacts: archive`) scans the Go binary's buildinfo → Go modules ONLY** — the `web/` npm graph is a build-time artifact `go:embed`-baked as opaque bytes and is **NOT captured**. To honestly cover "binary + web/", also emit a **source-artifact SBOM** (`sboms: [{artifacts: source}]`, syft over the release source tarball incl. `web/package-lock.json`). Ship BOTH docs (binary Go-modules SBOM + source SBOM covering web/), SPDX-JSON, per-release, attached to the Release + checksummed. No image-level attestation this phase.
- **D-18 (govulncheck — merge-gate note added):** Keep **reachable-any = block** (the standalone `vulncheck` job runs `govulncheck $(scripts/go_packages.sh)` with no flags → exit 3 on any reachable finding, so the *job* reds — strictly stronger than the audit's "high-severity" wording). Pin the govulncheck tool to an exact version (it is `@latest` today). **Adversarial caveat:** whether it *blocks merges* depends on **branch-protection required-checks config, which is not in the repo** — do NOT assert "blocks merges" in acceptance without confirming `vulncheck` is a required status check (add that to the acceptance checklist). Do NOT add osv-scanner (no reachability; alert fatigue).
- **D-19 (workflow-pin lint gate — regex hardened by adversarial pass):** Ship a **custom grep gate** `scripts/workflow_pin_gate.sh` + a blocking CI job — dodges the "pin the linter itself" bootstrap paradox and matches the existing `scripts/quality_snapshot_gate.sh`/`go_packages.sh` script-gate precedent. Two independent checks: (a) every `uses:` is `@<40-hex-sha> # vX.Y.Z`, allowing **multi-segment paths** (`github/codeql-action/init` — a naive `owner/repo@` regex fails these), local `./…`, and `docker://…@sha256:…`; scope the ref-pattern to `uses:` lines so it does **not** false-flag `version: "~> v2"` (a goreleaser-action *input*, `release.yml:55`); (b) no `go install …@latest` — and must **scan indented `run: |` body lines**, not just step-level `run:`, while NOT flagging the already-pinned `@v2.12.2`/`@v1.31.1`/pipx `==` installs. **Manual (grep can't enforce):** codeql `init`+`analyze` pinned to the **same** SHA — add as a review-checklist item. Optionally layer zizmor later as pinned, advisory defense-in-depth.
- **D-20 (pin scope):** **Pin everything to SHA** (incl. `actions/*`/`github/*`) with a trailing `# vX.Y.Z` comment — the only option meeting "all Actions SHA-pinned" + OpenSSF Scorecard / GitHub secure-use / SLSA 2026 guidance. The comment form is **mandatory** (Dependabot only updates SHA pins that carry it — a bare SHA rots blind). `.github/dependabot.yml`'s `actions` group already keeps them current. 68 `uses:` lines / 13 distinct refs across ci/codeql/release/skills; prioritize genuine third-party first (`crazy-max/ghaction-github-runtime`, `dorny/paths-filter`, `goreleaser/goreleaser-action`), then Docker-org, then GitHub first-party. Pin the 3 `@latest` tool installs (deadcode ci.yml:80, govulncheck ci.yml:160, go-mutesting skills.yml:133) to exact versions; pin `init`+`analyze` codeql-action to the **same** SHA.

### Deferred Ideas (OUT OF SCOPE)
- **Image-level SBOM attestation** (scan the pushed appliance image with syft; goreleaser `dockers_v2` bypasses `build-push-action sbom:true`) — deferred; D-17 ships binary/archive SBOM only.
- **zizmor as a blocking gate** — ship the custom grep gate now; zizmor can layer later as pinned, advisory defense-in-depth.
- **osv-scanner / container + lockfile vuln scanning** — beyond the Go call-graph; not this phase.
- **F-019 ops half** (load/chaos harness, backup/DR restore drill) + the honest-10/10 evidence bundle → **Phase 41** (OPS/REL).
- **All-routes / every-decoder strict-JSON sweep** — D-16 does the acceptance set + refactor-on-touch; a full sweep is a follow-on if desired.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SEC-01 | Tool output and traces redact secret-like values before persistence; full reasoning-trace mode requires explicit production warning/fail-fast + retention config + optional encrypted sink | Verified both at-rest seams (`store_append.go:218`, `agent/tools/result.go:165`) still hold at HEAD; verified `reasoningtrace.go`'s redaction bug is real (no `secret`/`redact` package import); confirmed the exact AES-256-GCM+HKDF idiom to mirror for the encrypted sink; provided a `KindEnum`-shaped `KnobSpec` recommendation for the three new env knobs |
| SEC-02 | Permissive/wildcard CORS replaced (same-origin per D-14 reversal) | Verified `server_cors.go` (23 lines, clean deletion), all 4 config touch-points, and both `withCORS` call sites in `server.go:357,364` (a detail beyond CONTEXT's citations, needed for the removal to compile) |
| SEC-03 | Validation console refuses non-loopback bind without explicit unsafe flag + auth | Verified `mcpConsole` (`integrations_console.go:38-52`) has zero bind-guard code today; confirmed `pimAdminTokenDefault` injection pattern in `integrations_proxy.go` as the token-handling precedent to extend |
| SEC-04 | Prompt-injection / tool-policy-bypass regression suite denies shell/file/network/MCP under `server_production` | Verified the entire gateway deny-path call chain byte-for-byte (`decide.go`, `approve.go`, `gateway.go`, `reserve.go`, `classify.go`, `internal/scoring/scoring.go`); verified `web_fetch`/`web_search` never set `Mutating` (D-03b); verified the `cot_eval` harness's exact invocation command and build-tag isolation |
| SEC-05 | CI publishes SBOM, govulncheck blocks, all Actions SHA-pinned | Resolved exact SHA + version-comment for all 13 existing + 1 new (`anchore/sbom-action/download-syft`) action refs via live GitHub API queries; confirmed goreleaser's `sboms: artifacts: [archive, source]` syntax via official docs; resolved exact current versions for govulncheck/deadcode/go-mutesting; researched go.mod `tool` directive mechanics and gave a scoped recommendation |
| SEC-06 | Privileged JSON routes use strict decoding | Verified all 6 acceptance-set decoders plus the 2 vulnerable trailing-JSON precedents (`governance_write_api.go:196`, `governance_write_skills_api.go:221`) plus the correctly-non-vulnerable size-cap precedents (`governance_write_scheduler.go:131`, `connect_api.go:95`); confirmed all 6 routes already share one size cap (`maxRunBodyBytes`), simplifying the helper's default |
| SEC-09 | Weak-hash CodeQL finding at `recovery_hash.go` remediated | Verified `HashLookupToken` sink, all 4+ call sites (including the previously-unlisted `code_hash` use at `recovery.go:96`), the PRIMARY KEY schema, and `AURA_AUTHULA_SECRET`'s plumbing through `config.LoadDB()`; resolved the exact local-CodeQL-replication command (query suite = `security-and-quality`, from `.github/codeql/codeql-config.yml`) and flagged that the CLI itself is not installed locally |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

These directives apply with the same authority as CONTEXT.md's locked decisions — the plan must not contradict them:

- **PRD-first:** no code without the PRD amendments named in D-08/D-10b/D-14b landing first (own commits, per the Q&A revision protocol).
- **NEVER SUPPOSE / READ BEFORE EDIT:** every file this phase touches has now been read at HEAD (see Canonical Refs Verification below) — re-read again immediately before editing if more than ~5 messages have elapsed.
- **3-STRIKE RULE:** applies to D-07's CodeQL verify-then-fallback (at most 3 local-scan attempts before falling back to the documented-FP path) and to D-19's regex hardening.
- **NO GOD CLASS (≤600 LOC):** every touched file has headroom — `recovery_hash.go` is 121 lines, `server_cors.go` is 23 lines (deleted whole), `reasoningtrace.go` is 249 lines, `config_validate.go` is well under the cap even after adding `gateReasoningTraceFull`.
- **DEEP REFACTOR ON TOUCH:** D-16c explicitly flags `governance_write_scheduler.go:131` as consciously deferred — do not silently sweep it in; do not silently leave it undocumented either.
- **NO COMMENTS UNLESS WHY IS NON-OBVIOUS:** match the existing file's comment density (every file read this session is comment-heavy explaining *why*, not *what* — follow that house style, it is intentional per this same CLAUDE.md rule).
- **COVERAGE FLOOR 85%** (combined unit+integration+smoke) **and the Skills-job `db_integration`-only number is the one to verify at phase close** — see Validation Architecture.
- **NO SKIP-AS-GREEN IN CI:** the new SEC-04 gate and SEC-06 helper tests must be real assertions runnable in <30s, not env-gated skips.
- **Migration numbering:** confirmed via `ls internal/db/migrations/ | tail -1` = `0049_cli_idempotency_identity.up.sql` at research time (CLAUDE.md's own header "40 migrations… floor 0040" is stale — CONTEXT.md's `23` note already corrected this to 0049; independently reconfirmed here). This phase needs **no migration** (all seven items are code+config only), so the number is moot unless something unexpected surfaces.
- **Deferred-tool pattern:** N/A — this phase adds no new agent-visible tools.
- **Quality snapshot update at phase close:** since this phase touches `internal/agui/**`, `internal/gateway/**` (if the SEC-04 suite lands there), `internal/conversations/**`, `internal/reasoningtrace/**`, `internal/config/**`, and `.github/workflows/**`, check `docs/aura-quality-snapshot.md` for rows whose CI-gate-path glob matches — the phase-close push will be gated by `scripts/quality_snapshot_gate.sh` per CLAUDE.md's standing rule.
- **WSL is the primary dev/test environment** for `-race`, `db_integration`, `neo4j_integration`, and mutation testing — the Validation Architecture below assumes WSL execution per the established project convention.

## Canonical Refs Verification Report

Every file:line citation in `40-CONTEXT.md`'s `<canonical_refs>` section was independently re-read at HEAD this session. Result: **all confirmed exact** except one path mislabel.

| Ref (as cited in CONTEXT.md) | Verification | Note |
|---|---|---|
| `decide.go` :31, :35-40, :49, :51 | ✅ exact | Strict-gate, read-only branch, GateRecommended routing, early-return all match line-for-line |
| `gateway.go` `New(profile, store)` :128 | ✅ exact | `func New(profile config.RuntimeProfile, store reservationStore) *Gateway` |
| `approve.go` :92-98, :103-105, :218-219 | ✅ exact | `WithResolvedApproval` top escape, profile deny, `recordDegradedDeny` nil-guard all match |
| `reserve.go` :36-37, :172-176 | ✅ exact | `beginOperation`/`reserve` nil-guards confirmed — a Normal-mutating call returns `Allow`, never panics |
| `classify.go` :37-45, :52-59, :87-91 | ✅ exact | `classify`, `skillFixedTiers`, `taskFixedTiers` all match line-for-line |
| `internal/gateway/scoring.go` :140 `GateRecommended` | ⚠️ **path mislabel** | `GateRecommended` is defined at `internal/scoring/scoring.go:140` — confirmed `internal/gateway/` has no `scoring.go` file (package listing: `approvals.go, approve.go, classify.go, decide.go, gateway.go, guard.go, reconcile.go, reserve.go` — no `scoring.go`). Line number and code are exactly right; only the package directory in the citation is off. |
| `web_fetch.go:49`, `web_search.go:58` non-mutating | ✅ exact | Neither file sets `Mutating` anywhere (grep: 0 matches) — Go zero-value `false` applies, confirming `scoring.Safe` classification |
| `mcptools/bridge.go:72,242` `Mutating = !ReadOnlyHint` | ✅ exact | Both lines match verbatim |
| `shell_exec.go:104` `Mutating:true` | ✅ exact | Confirmed, with the D-43 comment explaining the conservative choice |
| `config_runtimeprofile.go` `Strict()` ~:56-57 | ✅ exact | `func (p RuntimeProfile) Strict() bool { return p == ProfileSingleUserHardened \|\| p == ProfileServerProduction }` |
| `internal/eval/` harness files | ✅ exact | `harness_cot_eval_test.go`, `dataset_cot_eval.go`, `judge_cot_eval.go`, `doc.go` all present; `doc.go` confirms `cot_eval` tag, `OPENROUTER_API_KEY`-gated, never in CI/Makefile |
| `recovery_hash.go:67` `HashLookupToken` sink | ✅ exact | `sha256.Sum256([]byte(token))` at line 68, function starts line 67 |
| `serve_password_reset.go:149,295-301,531` | ✅ exact | Mint call, `getPasswordResetTokenForUpdate` SELECT-FOR-UPDATE, and `newPasswordResetToken` (32-byte / 256-bit `crypto/rand`) all confirmed |
| `recovery.go:96,104` | ✅ exact | Both `agui.HashLookupToken(...)` calls confirmed; **note** — line 96's hash actually feeds the throwaway `code_hash` column (via `breakGlassChallengeParams`), not `token_hash`; CONTEXT.md's D-06b text already correctly describes this as "never read back" |
| `identity.go:61` calls `identityRecover` without `cfg` | ✅ exact | `identityRecover(ctx, store, pool, args[1:])` — confirmed no `cfg` argument; sibling `recover-operator` case at :63 DOES pass `cfg`, confirming the omission is specific to the plain `recover` path |
| `password_reset.go:318`, `identity_recovery.sql:86`, migration `0023:28` | ✅ exact | `tokenHash := HashLookupToken(...)`, `WHERE token_hash = $1`, `token_hash text PRIMARY KEY` all confirmed |
| `config.go:158`, `Secret: true` | ✅ exact (split across 2 files) | Field declared at `config.go:158`; the `Secret: true` KnobSpec tag is at `config_knobs.go:72`, not literally on config.go:158 — both are correct facts, the citation just doesn't separate them |
| `store_append.go:213-249`, `:218`, `:219`, `:123`, `:252` | ✅ exact | `appendTurnWrites` body, `safeContent := postgresTextSafe(...)`, `maybeSpill(...)`, the independent spill-guard length check, and `insertTurnAndAggregates`'s sole `InsertConversationTurn` call all confirmed line-for-line |
| `store_append.go` `AppendTurn:58,86`, `AppendAssistantTurnWithCacheMetric:141,174` | ✅ exact | All four `s.appendTurnWrites(p)` call sites confirmed |
| `store_branch.go:219 ForkBranch` | ✅ exact | Fifth (sixth counting all) call site to `appendTurnWrites` confirmed — **all six non-test callers funnel through the one function**, verified via repo-wide grep |
| `agent/tools/result.go NewResult :165,236-244` | ✅ exact | `writeSidecar(path, content)` call at :165; `writeSidecar` body (`os.WriteFile(...,0o600)` + `os.Chmod(...,0o600)`) at :236-244 |
| `toolinvocations/store.go:154 ResultSidecarPath` | ✅ exact | Confirmed — this column stores only the *path*, never the file's redacted content |
| `redact/string.go`, `secret/envkey.go` | ✅ exact | `String()`, `IsSecretEnvKey`, `IsSecretEnvVar`, `ContainsCredentialURL` all confirmed present with the exact behavior described |
| `toolinvocations/store.go:146,150 RedactForLedger` | ✅ exact | Both call sites confirmed (`ArgsRaw`, `ResultPreview`) |
| `reasoningtrace.go:99,106,157,232-248` | ✅ exact | `redactValueForKey` call in `Record`, `redactString(string(line))` call, `redactValue` function, and the buggy `redactString` (4-marker check, no `secret` package import) all confirmed |
| `config_validate.go:88-105` gate matrix, `gateDestructiveShell` | ✅ exact | `ValidateProfile`'s 15-gate aggregation confirmed line-for-line; `gateDestructiveShell` template confirmed at :238-247 |
| `config_validate.go:226 gateCORS` | ✅ exact | `if c.AGUICORSPermissive {` is precisely line 226 |
| `config.go:118-122,438`, `config_knobs.go:77` `AGUICORSPermissive` | ✅ exact | Field decl, default-wiring, and KnobSpec entry all confirmed |
| `server_cors.go` `withCORS` | ✅ exact | Whole 23-line file read; confirmed the hardcoded `"GET, POST, DELETE, OPTIONS"` (already missing PATCH/PUT, proving the drift claim) |
| `auth.go:14-17` CSRF re-eval note | ✅ accurate paraphrase | Actual text: "Re-evaluate if Phase 28/29 introduces a cross-origin write surface" — CONTEXT.md's paraphrase ("re-evaluate if a cross-origin write surface is introduced") preserves the meaning exactly |
| `auth_cookie.go:129-130` `SameSite=Strict`+`Secure` | ✅ exact | Both cookie attributes confirmed at those exact lines |
| `integrations_console.go:38-52` | ✅ exact | Whole `mcpConsole` function — raw `--addr` parse with zero validation, `srv.ListenAndServe()` with no auth wrapper |
| `integrations_proxy.go:37,48-63` | ✅ exact | `pimAdminTokenDefault` constant and the `X-Admin-Token` injection in `builtinIntegrations()` confirmed |
| `server_run_request.go:26`, `approvals_api.go:124`, `onboarding_api.go:279`, `assets_api.go:82` | ✅ exact | All four confirmed as plain `Decode` calls with no `DisallowUnknownFields`/trailing-check |
| `governance_write_api.go:193,196`, `governance_write_skills_api.go:218,221` | ✅ exact | Both `decodeMCPBody`/`decodeSkillsBody` functions AND their vulnerable `!errors.Is(err, io.EOF)` single-decode idiom confirmed line-for-line |
| `conversations_api.go:162` | ✅ exact | Same vulnerable idiom, inlined (no shared helper) |
| `governance_write_scheduler.go:131`, `connect_api.go:95` `io.LimitReader` precedents | ✅ exact | Confirmed as size-cap-only precedents; scheduler's decode has no EOF-tolerance at all (implicitly `allowEmpty=false`); connect_api's read is of an *upstream sidecar response*, not an inbound privileged route — correctly cited only as an `io.LimitReader` pattern example, not an acceptance-set member |
| `.github/workflows/{ci,codeql,release,skills}.yml` — 68 `uses:`, 13 distinct refs, zero SHA pins | ✅ exact | `grep -c uses:` totals 55+4+7+2=68 exactly; enumerated exactly 13 distinct action refs; confirmed zero `@<sha>` pins exist anywhere (all `@vN` floating tags) |
| `ci.yml:80,85,145-161,160,465`, `skills.yml:133` | ✅ exact | deadcode/govulncheck `@latest`, golangci-lint/sqlc already-pinned precedents, and the `vulncheck:` job boundaries all confirmed line-for-line |
| `.goreleaser.yaml` `version: 2`, `archives:` | ✅ exact | Confirmed; also confirmed **no `sboms:` block exists yet** (D-17 is additive) and `dockers_v2:` exists as a separate goreleaser-native docker path (confirms the "Deferred" image-SBOM note is accurate — `sboms:` cannot reach that artifact type) |
| `.github/dependabot.yml:20-32` `github-actions` group | ✅ exact | Confirmed present and already configured — no dependabot.yml change needed for D-20 |

**Overall verdict:** CONTEXT.md's canonical refs are current and trustworthy. The planner can cite them directly without re-verification, with the one path-label correction noted above.

## Standard Stack

This phase adds **zero new Go module dependencies**. Every primitive needed is already stdlib or already a direct dependency:

### Core (already in go.mod / stdlib — no `go get` needed)
| Package | Version | Purpose | Why no new dep |
|---------|---------|---------|-----------------|
| `crypto/hkdf` | stdlib (Go 1.24+) | HKDF-Expand subkey derivation for D-06's pepper and D-13's sink key | `go.mod` already declares `go 1.26.5`; `internal/objectstore/identity_store.go` already imports and uses this exact stdlib package `[VERIFIED: identity_store.go:7]` |
| `crypto/hmac` | stdlib | HMAC-SHA-256 for D-06's peppered token hash | stdlib, zero cost |
| `crypto/aes` + `crypto/cipher` | stdlib | AES-256-GCM AEAD for D-13's encrypted trace sink | `identity_store.go` already uses this exact construction `[VERIFIED: identity_store.go:62-69]` |
| `golang.org/x/crypto` | v0.54.0 (already required, `go.mod:39`) | Optional XChaCha20-Poly1305 alternative for D-13 (`golang.org/x/crypto/chacha20poly1305`) | Already a direct dependency; adding this sub-package costs nothing new |
| `encoding/json` | stdlib | `DisallowUnknownFields`, two-stage `Decoder.Decode` for D-16's strict-decode helper | stdlib |

### Supporting — CI/build tools (not Go module deps; installed via `go install` or GitHub Actions)
| Tool | Current version (resolved 2026-07-22) | Purpose | Provenance |
|------|------|---------|------------|
| `golang.org/x/vuln/cmd/govulncheck` | **v1.6.0** | SEC-05 vuln-scan gate (D-18) | `[VERIFIED: go list -m golang.org/x/vuln@latest via GOPROXY, 2026-07-22]` |
| `golang.org/x/tools/cmd/deadcode` | **v0.48.0** | Already-shipped dead-code CI gate; D-20 asks only to pin it | `[VERIFIED: go.mod:46 already requires golang.org/x/tools v0.48.0 — pinning deadcode to this exact version costs zero go.sum churn]` |
| `github.com/avito-tech/go-mutesting/cmd/go-mutesting` | **v0.0.0-20251226130216-48d0401f00fb** (pseudo-version — no tagged releases) | Mutation-testing CI job (skills.yml) | `[VERIFIED: go list -m github.com/avito-tech/go-mutesting@latest, 2026-07-22]`. **Caveat:** re-resolve at execution time — this fork has no semver tags, so "pinning" means pinning an exact commit-derived pseudo-version, which the planner/executor should re-fetch fresh rather than hardcode from this document, since a newer commit may exist by execution time. |
| `anchore/syft` (binary, not a Go module) | **v1.49.0** (GitHub release) | D-17's SBOM generator, invoked by goreleaser's `sboms:` block | `[VERIFIED: gh api repos/anchore/syft/releases/latest, 2026-07-22]` |
| `anchore/sbom-action/download-syft` (GitHub Action) | **v0.24.0** / SHA `e22c389904149dbc22b58101806040fa8d37a610` | The **official, purpose-built** way to install syft onto PATH in CI before goreleaser runs (goreleaser-action does not install syft itself) | `[VERIFIED: gh api repos/anchore/sbom-action/{releases,commits}, 2026-07-22]` — 252 stars, org-owned (`Anchore`), not archived, 24+ tagged releases; confirmed the `download-syft` subdirectory action exists (`action.yml` present) |

### Alternatives Considered
| Instead of | Could use | Tradeoff |
|------------|-----------|----------|
| stdlib `crypto/hkdf.Key(...)` for D-06's pepper | Authula's `DeriveOAuthHMACKey` pattern (`hmac.New(sha256.New, secret).Write(label).Sum(nil)`) | **Important clarification:** Authula's own `DeriveOAuthHMACKey` is **not actually HKDF** — it is a simple HMAC-as-PRF construction (`[VERIFIED: D:/tmp/authula/plugins/oauth2/services/crypto.go:15-19]`). D-06's text says "HKDF-derived" and cites Authula "as the pattern to mirror" — these are in tension if taken literally. Recommendation: mirror Authula's **design principle** (never reuse the raw secret; derive one dedicated, domain-separated subkey), but **implement via Aura's own already-in-tree stdlib `hkdf.Key()` idiom** (matches D-06's literal wording, reuses a tested pattern, avoids introducing a third distinct derivation style into the codebase). |
| AES-256-GCM (mirror `identity_store.go`) for D-13's sink | XChaCha20-Poly1305 (mirror Authula's `crypto_token_repository.go:47`, `golang.org/x/crypto/chacha20poly1305`) | Both are zero-new-dependency options. AES-GCM matches Aura's own established in-tree pattern (recommended — least novelty). XChaCha20's 192-bit random nonce space is theoretically safer against nonce-reuse at very high record volumes, but a reasoning-trace sink will never approach the ~2^32-record threshold where AES-GCM's 96-bit space becomes a real concern — treat this as a documented, equally-valid alternative, not a required change. |
| `go install X@version` (status quo) for deadcode/govulncheck | `go.mod` `tool` directive (Go 1.24+ `go get -tool`) | See detailed discretion resolution below — recommended for deadcode + govulncheck, optional for go-mutesting. |

**Installation:** none required — this phase is implementation-only against existing dependencies plus CI-tool version/pin bumps in workflow YAML (no `go get`/`npm install`).

**Version verification performed:** `go list -m <module>@latest` against the live GOPROXY for all Go-ecosystem tools; `gh api repos/<owner>/<repo>/releases/latest` and `gh api repos/<owner>/<repo>/tags` for all GitHub-Action and non-Go-module tools. All confirmed current as of 2026-07-22.

## Package Legitimacy Audit

**Scope note:** the standard slopcheck/npm/pip/cargo protocol does not apply — this phase introduces **zero new entries into go.mod's `require` block** (everything needed is stdlib or an existing direct dependency; see Standard Stack). The actual external supply-chain surface this phase touches is (a) SHA-pinning GitHub Actions that are **already in use** today, and (b) one **new** GitHub Action (`anchore/sbom-action/download-syft`) needed for D-17. I ran an equivalent legitimacy check for (b) via the GitHub API (age via tag history, star count, archived status, owning-org type) since `slopcheck`/registry-CLI tooling targets package registries, not Actions.

| Package/Action | Registry | Age signal | Stars | Owner | Disposition |
|---------|----------|-----|-----------|-------------|-------------|
| `anchore/sbom-action` (parent repo of `download-syft`) | GitHub Actions | 24+ tagged releases (v0.21.0 → v0.24.0 visible) | 252 | Organization (`Anchore`, a known container-security vendor) | `[VERIFIED: gh api, 2026-07-22]` **Approved** — active, non-archived, mature, matches its stated purpose exactly ("GitHub Action for creating software bill of materials using Syft") |
| `crazy-max/ghaction-github-runtime` | GitHub Actions | tags back to v3 | 99 | Individual maintainer (`crazy-max`, a well-known, prolific GH-Actions author with dozens of widely-used actions) | `[VERIFIED: gh api, 2026-07-22]` Approved — already in use today, D-20 only adds a SHA pin, no new trust decision |
| `dorny/paths-filter` | GitHub Actions | tags back to v3.12.0+ | 3260 | Individual maintainer | `[VERIFIED: gh api, 2026-07-22]` Approved — already in use, high star count, mature |
| The other 11 existing action refs (`actions/*`, `docker/*`, `github/codeql-action`, `goreleaser/goreleaser-action`) | GitHub Actions | N/A | N/A | GitHub-first-party / Docker Inc. / GoReleaser org | Not re-audited for trust — these are already in production use in this repo; D-20's work is exclusively a SHA-pin (supply-chain integrity), not a new-trust decision |

**Packages/Actions removed due to a legitimacy concern:** none.
**Packages/Actions flagged as suspicious:** none.

**Note on go.mod tool-directive additions:** if the planner adopts the `tool` directive for `golang.org/x/vuln` (see discretion resolution below), that module enters go.mod's `require` block for the first time. It is a `golang.org/x` namespace module maintained directly by the Go team — the highest legitimacy tier available in the Go ecosystem, equivalent to stdlib in provenance even though it ships as a separate module.

## Architecture Patterns

### System Architecture Diagram

This phase touches six independent hardening seams. None of them share a request path with each other; the diagram shows each seam's own data flow.

```
SEC-04  Tool-call dispatch  ──▶  gateway.Decide(profile, spec, args, key)
                                        │
                          profile.Strict()? ──No──▶ Allow (dev/local_trusted no-op)
                                        │Yes
                                classify(spec, args) ──▶ RiskTier
                                        │
                      GateRecommended(tier)? ──No──▶ beginOperation ──▶ reserve ──▶ Allow
                                        │Yes
                                 routeApprove(ctx)
                                        │
                   plain context.Background() (no resolved approval,
                   no responder) ──▶ recordDegradedDeny ──▶ DENY  ◀── SEC-04 gate asserts THIS

SEC-09  Reset-token flow (mint)                    Reset-token flow (lookup)
        newPasswordResetToken()                     tokenHash := HashLookupToken(candidate, pepper)
              │ 256-bit CSPRNG                             │
        pepper := HKDF(AURA_AUTHULA_SECRET, "aura-reset-token-pepper-v1")
              │
        HashLookupToken(token, pepper)  ──▶  INSERT token_hash (PK)   SELECT … WHERE token_hash = $1

SEC-01  Tool executes ──▶ agent/tools.NewResult(ctx, content)
                                 │                        │
                    preview (capped, in turn)     FULL bytes ──▶ <run_dir>/…/<id>.result (0o600)
                                 │                        │
                      [BOTH must pass the SAME inbound exact-match detector — configured-secret set]
                                 │
                    conversations.appendTurnWrites ──▶ safeContent := redact(postgresTextSafe(content))
                                 │                        │
                    INSERT conversation_turns      maybeSpill (sidecar, same redacted content)

        reasoningtrace.Record(stage, fields) ──▶ redactValueForKey (structured fields)
                                 │                        │
                    json.Marshal(row)              redact.String(line) [NEW — currently missing]
                                 │                        │
                    tracesink.Write(encrypted, AEAD, fresh nonce/record)  [NEW pkg, fail-closed on missing key]

SEC-02  Browser request ──▶ agui.Server mux ──▶ (withCORS deleted — no header injected) ──▶ handler
                            same-origin only; no preflight branch remains

SEC-03  `aura mcp console [--addr X] [--unsafe-non-loopback] [token env]`
                                 │
                    isLoopback(addr)? ──No──▶ unsafe flag set? ──No──▶ FAIL (refuse to bind)
                                 │Yes                              │Yes
                            bind unauthenticated            token set? ──No──▶ FAIL
                            (unchanged happy path)                 │Yes
                                                              bind + WARN log + require token per-request

SEC-06  HTTP request ──▶ strictDecode(w, r, &dst, opts{cap, allowEmpty})
                                 │
                    Content-Type check ──▶ size-cap MaxBytesReader ──▶ dec.DisallowUnknownFields()
                                 │
                    dec.Decode(&dst)  [err==io.EOF && allowEmpty ⇒ OK; err!=nil ⇒ 400]
                                 │
                    dec.Decode(&struct{}{})  [MUST be io.EOF, else 400 "trailing data"]
                                 │
                            handler receives dst

SEC-05  git push (tag v*) ──▶ release.yml
                                 │
                    anchore/sbom-action/download-syft@<SHA>  (puts syft on PATH)
                                 │
                    goreleaser/goreleaser-action@<SHA> release
                                 │
                    sboms: [{artifacts: archive}, {artifacts: source}]  ──▶ 2× SPDX-JSON attached to GH Release

        every `.github/workflows/*.yml` push/PR ──▶ scripts/workflow_pin_gate.sh
                                 │
                    every `uses:` is @<40-hex> # vX.Y.Z?  &&  no `go install …@latest` (incl. run: | bodies)?
                                 │No──▶ CI job FAILS (blocking)
```

### Recommended File Placement (resolving "Claude's Discretion" item 1)

CONTEXT.md leaves exact file placement to discretion. Recommendations, grounded in the existing package conventions observed this session:

```
internal/gateway/
  injection_suite_test.go     # SEC-04 deterministic gate — in-package (not a new
                               # sub-package) so it inherits main_test.go's
                               # goleak.VerifyTestMain(m) for free [VERIFIED:
                               # internal/gateway/main_test.go]. Matches the existing
                               # decide_test.go/approve_test.go/classify_test.go
                               # naming convention.

internal/eval/
  injection_cot_eval.go       # SEC-04 D-04 LLM-resistance dataset, mirrors
  injection_cot_eval_test.go  # dataset_cot_eval.go / skills_cot_eval_test.go naming;
                               # tag cot_eval, same doc.go isolation

internal/agui/
  recovery_hash.go            # SEC-09 — edit in place (121 LOC, ample headroom);
                               # add DeriveResetTokenPepper(secretHex) ([]byte, error)
                               # as a sibling function in the SAME file (small,
                               # single-purpose, matches identity_store.go's pattern
                               # of co-locating the derive-fn beside its consumer)
  strict_decode.go            # SEC-06 — new centralized helper, mirrors the existing
  strict_decode_test.go       # one-concern-per-file convention (server_cors.go,
                               # connect_api.go, etc. are all similarly scoped)
  server_cors.go              # SEC-02 — DELETE whole file (23 LOC)

internal/tracesink/           # SEC-01 D-13 — new small package (~20-40 LOC + test),
  sink.go                     # per CONTEXT's own estimate; keeps the encrypted-file
  sink_test.go                # concern out of internal/reasoningtrace (which stays
                               # focused on the JSONL/redaction concern)

internal/secret/
  redact_exact.go             # SEC-01 D-10 — the shared inbound exact-match detector,
  redact_exact_test.go        # placed in internal/secret (not internal/redact, which
                               # is outbound-pattern-only) since it directly builds on
                               # IsSecretEnvKey/IsSecretEnvVar already there; consumed
                               # by BOTH internal/conversations and internal/agent/tools
                               # without a new cross-package coupling

scripts/
  workflow_pin_gate.sh        # SEC-05 D-19 — exact name CONTEXT.md specifies
  workflow_pin_gate_test.sh   # mirrors the quality_snapshot_gate_test.sh self-test
                               # pattern [VERIFIED: scripts/quality_snapshot_gate_test.sh
                               # uses a tmpdir + fixture files + rc assertions]
```

### Pattern 1: HKDF Subkey Derivation (for SEC-09's pepper and SEC-01's sink key)
**What:** derive a purpose-bound, domain-separated subkey from a shared high-entropy secret via HKDF-Expand, never using the raw secret directly for a second purpose.
**When to use:** any time a new cryptographic use needs a key derived from `AURA_AUTHULA_SECRET` (or a similarly-scoped operator secret) without reusing it verbatim.
**Example (the exact in-tree idiom to mirror):**
```go
// Source: internal/objectstore/identity_store.go:192-208 (VERIFIED at HEAD, 2026-07-22)
const keyDerivationInfo = "aura-objectstore-identity-key-v1" // existing convention: "aura-<domain>-<purpose>-v1"

func deriveObjectStoreKey(authulaSecretHex string) ([]byte, error) {
	secret := strings.TrimSpace(authulaSecretHex)
	if len(secret) != 64 { // fail CLOSED on malformed input — never derive from a short/empty secret
		return nil, fmt.Errorf("objectstore identity: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: AURA_AUTHULA_SECRET must be valid hex: %w", err)
	}
	// crypto/hkdf is Go 1.24+ STDLIB (RFC 5869) — no golang.org/x/crypto/hkdf import needed.
	key, err := hkdf.Key(sha256.New, raw, nil, keyDerivationInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: derive key: %w", err)
	}
	return key, nil
}
```
**Recommended labels for this phase (domain-separated per the established `-v1` suffix convention):**
- SEC-09 pepper: `"aura-reset-token-pepper-v1"`
- SEC-01 sink key: `"aura-reasoning-trace-sink-v1"`

### Pattern 2: AES-256-GCM Encrypted Sink with Fresh Nonce Per Record (SEC-01 D-13)
**What:** an append-only, per-record-framed encrypted file sink; every record gets its own random nonce (never reuse a nonce with GCM).
**When to use:** the D-13 `internal/tracesink` package.
**Example (the exact in-tree idiom to mirror, currently unexported on `IdentityStore` — write a fresh ~20-line version, do not attempt to import these unexported methods):**
```go
// Source: internal/objectstore/identity_store.go:155-176 (VERIFIED at HEAD, 2026-07-22)
// encrypt seals plaintext with a fresh random nonce prepended to the ciphertext.
func encrypt(aead cipher.AEAD, plaintext string) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil { // crypto/rand — fresh nonce EVERY call
		return nil, fmt.Errorf("tracesink: nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, []byte(plaintext), nil), nil // nonce-prefixed framing
}

// decrypt opens a nonce-prefixed ciphertext produced by encrypt.
func decrypt(aead cipher.AEAD, ciphertext []byte) (string, error) {
	ns := aead.NonceSize()
	if len(ciphertext) < ns {
		return "", fmt.Errorf("tracesink: ciphertext too short (%d < %d)", len(ciphertext), ns)
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("tracesink: decrypt: %w", err)
	}
	return string(plaintext), nil
}
```
**Fail-closed key policy (mirrors `deriveObjectStoreKey`'s :196-197 length check exactly):** if `AURA_TRACE_ENCRYPT_KEY` is absent or not exactly 64 hex chars when `AURA_REASONING_TRACE=full` is acked, the sink must **refuse to write** rather than fall back to plaintext — a silent plaintext fallback reopens the exact F-021 finding this phase closes.

### Pattern 3: Strict JSON Decode with Trailing-Data Rejection (SEC-06 D-16/D-16b)
**What:** the two-decode idiom that closes the F-052 trailing-JSON vulnerability. This is the single most safety-critical code pattern in this phase — every existing in-repo precedent (`conversations_api.go:162`, `governance_write_api.go:196`, `governance_write_skills_api.go:221`) is **missing** the second step and must NOT be copied verbatim.
**When to use:** the D-16 centralized helper, applied to all six acceptance-set routes.
**Example (the Alex Edwards idiom — VERIFIED via WebSearch against the canonical technique; the "second decode must be EOF" step is the part every current in-repo precedent lacks):**
```go
// strict_decode.go — NEW, no existing in-repo precedent implements the FULL pattern.
type decodeOpts struct {
	maxBytes   int64 // default maxRunBodyBytes (1<<20) — matches all 6 acceptance-set routes' EXISTING cap
	allowEmpty bool  // per-route: false for approvals/onboarding/assets/run, true for governance MCP/skills verb-only bodies
}

func strictDecodeJSON(w http.ResponseWriter, r *http.Request, dst any, opts decodeOpts) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("strict decode: unsupported content-type %q", ct)
	}
	cap := opts.maxBytes
	if cap <= 0 {
		cap = maxRunBodyBytes // existing package constant, server.go:28 — 1 MiB
	}
	r.Body = http.MaxBytesReader(w, r.Body, cap)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // reject unknown fields — NONE of the 3 existing precedents do this

	err := dec.Decode(dst)
	switch {
	case errors.Is(err, io.EOF) && opts.allowEmpty:
		return nil // empty body is fine for this route
	case err != nil:
		return fmt.Errorf("strict decode: %w", err)
	}

	// THE MISSING STEP in every current in-repo precedent (D-16b): a second Decode
	// into a throwaway value MUST hit io.EOF, or the body had trailing JSON —
	// {"class":"trusted_local"}{"ignored":true} is exactly the audit's repro.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("strict decode: unexpected trailing data after JSON body")
	}
	return nil
}
```
**Per-route defaults (resolving "Claude's Discretion" item 3 — all six routes already cap at `maxRunBodyBytes`, confirmed via grep at HEAD):**

| Route | Current cap | Current `allowEmpty` behavior | Recommended `decodeOpts` |
|---|---|---|---|
| `/agent/run` (`server_run_request.go:26`) | none visible in the decoder itself (capped upstream in `server.go`) | implicitly false (a run always needs a body) | `{maxBytes: maxRunBodyBytes, allowEmpty: false}` |
| approvals-resolve (`approvals_api.go:124`) | `maxRunBodyBytes` (`:122`) | false today (plain `Decode`, no EOF tolerance) | `{maxBytes: maxRunBodyBytes, allowEmpty: false}` |
| onboarding (`onboarding_api.go:279`) | `maxRunBodyBytes` (`:301` on a sibling handler; confirm per-call-site) | false today | `{maxBytes: maxRunBodyBytes, allowEmpty: false}` |
| assets (`assets_api.go:82`) | `maxRunBodyBytes` (`:80`) | false today | `{maxBytes: maxRunBodyBytes, allowEmpty: false}` |
| governance MCP (`governance_write_api.go:193`) | `maxRunBodyBytes` (`:194`) | **true today** (doc comment: "verb-only enable/disable use no body") | `{maxBytes: maxRunBodyBytes, allowEmpty: true}` |
| governance skills (`governance_write_skills_api.go:218`) | `maxRunBodyBytes` (`:219`) | **true today** (doc comment: "delete uses no body") | `{maxBytes: maxRunBodyBytes, allowEmpty: true}` |

No route in the acceptance set needs a cap value different from the existing `maxRunBodyBytes` (1 MiB) — the helper can default to it and only the two governance routes need `allowEmpty: true`.

### Anti-Patterns to Avoid
- **Copying the single-decode `!errors.Is(err, io.EOF)` idiom as "the fix":** it is the bug (D-16b). Every existing occurrence in this repo needs the *second* decode-must-be-EOF step added, not just reuse.
- **Deriving the SEC-09 pepper or SEC-01 sink key from the raw `AURA_AUTHULA_SECRET` bytes directly (no HKDF):** cross-protocol key reuse — a compromise of one derived use should never help forge another.
- **A plaintext fallback when `AURA_TRACE_ENCRYPT_KEY` is missing/malformed:** must fail closed (refuse to write), not silently degrade to plaintext — see D-13.
- **Pattern-redacting (regex) the INBOUND turn/spill/sidecar content:** corrupts the agent's own re-fed working data. Inbound must stay exact-match against a boot-harvested configured-secret set; only the OUTBOUND (export/ledger/trace) surface gets pattern-based `redact.String`.
- **Trying to sync CORS `Access-Control-Allow-Methods` to the live route table:** explicitly rejected by D-14 — the routes are scattered across ~15 files with no central registry, and the fix (same-origin, no CORS headers at all) makes the sync problem disappear rather than solving it.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Trailing-JSON / unknown-field HTTP body validation | A custom byte-scanning JSON validator | `encoding/json`'s `Decoder.DisallowUnknownFields()` + the two-decode-EOF idiom (Pattern 3 above) | stdlib-only, zero new deps, and the two-decode trick is a well-known, narrowly-scoped technique — no need for a JSON-schema library |
| Authenticated-encryption framing | A hand-rolled XOR/counter-mode cipher | `crypto/cipher.NewGCM` (or `golang.org/x/crypto/chacha20poly1305`) | AEAD constructions are exactly this hard to get right (nonce reuse, no authentication tag) — Aura already has a correct in-tree example to mirror, don't re-derive from first principles |
| Key derivation from a shared secret | String-concatenation-then-hash (`sha256(secret + "label")`) | `crypto/hkdf.Key(...)` (RFC 5869 HKDF-Expand, stdlib since Go 1.24) | naive concatenation lacks HKDF's formal security proof and domain-separation guarantees; the stdlib package costs nothing |
| SBOM generation | A custom `go list -m all` → JSON script | `syft` via goreleaser's native `sboms:` block | syft correctly resolves license/CPE/PURL metadata and produces a spec-compliant SPDX document; a hand-rolled script would need to reinvent SPDX's schema |
| Go vulnerability scanning | Grep-ing `go.sum` against a hand-maintained CVE list | `govulncheck` (official Go team tool, already in use) | govulncheck's call-graph reachability analysis (only flags vulnerabilities in code paths actually *called*) is precisely what avoids the alert-fatigue CONTEXT.md's D-18 explicitly rejected osv-scanner for |
| GitHub Actions pin verification | A one-off python/node script | The existing `scripts/quality_snapshot_gate.sh`/`go_packages.sh` bash-gate convention, extended with `scripts/workflow_pin_gate.sh` | matches the established project convention exactly (D-19); no new scripting language enters the toolchain |

**Key insight:** every "don't hand-roll" item in this phase has a **zero-new-dependency** answer already present in Aura's own toolchain or stdlib — this is a hardening phase, not a feature phase, and the research consistently found that reuse-in-place beats introducing anything new.

## Common Pitfalls

### Pitfall 1: The D-16b Trailing-JSON Idiom Looks Like a Fix But Isn't
**What goes wrong:** an implementer sees `if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) { ... }` already in three places in the codebase (`conversations_api.go:162`, `governance_write_api.go:196`, `governance_write_skills_api.go:221`) and assumes it is the *pattern to copy* for the new strict-decode helper.
**Why it happens:** the idiom genuinely does one useful thing (tolerates an empty body via `io.EOF`), so it reads as "the EOF-handling pattern" at a glance — but `json.Decoder.Decode` only ever consumes the *first* JSON value from the stream, silently ignoring anything after it. `{"class":"trusted_local"}{"ignored":true}` decodes successfully into `{"class":"trusted_local"}` with this idiom, exactly reproducing the audit's F-052 repro.
**How to avoid:** always pair the first `Decode` with a **second** `Decode(&struct{}{})` call and require it returns `io.EOF` — that is the only way to prove nothing followed the first JSON value. See Pattern 3's code example.
**Warning signs:** any decoder whose only body-shape check is `err != nil && !errors.Is(err, io.EOF)` with no subsequent decode call is vulnerable to trailing-payload smuggling.

### Pitfall 2: SEC-04's "Network" Class Cannot Be Modeled with `web_fetch`/`web_search`
**What goes wrong:** an implementer writes a DENY test case using `web_fetch{url: "http://169.254.169.254/..."}` expecting the gateway to deny it, and the test fails because the gateway returns `Allow`.
**Why it happens:** `web_fetch` and `web_search` never set `spec.Mutating = true` anywhere in their `Spec()` methods (`[VERIFIED: zero matches for "Mutating" in web_fetch.go/web_search.go]`) — Go's zero-value `false` applies, so `classify()` returns `scoring.Safe` for both, and Safe tools are always `Allow`ed by `gateway.Decide` (they never even reach the `GateRecommended` branch). Their SSRF/DNS-pin defense is a completely separate, already-shipped layer (Phase 31 CAP-05) that has nothing to do with the gateway PEP.
**How to avoid:** model the "network" deny-class as either (a) `shell_exec{cmd: "curl ... | sh"}` (Risky regardless of command content, per the D-43 conservative-mutating comment at `shell_exec.go:102-104`) or (b) a mutating MCP tool (`!ReadOnlyHint` per `mcptools/bridge.go:72,242`). State explicitly in the acceptance evidence that `web_fetch`/`web_search` are *legitimately* allowed by the gateway and denied (when appropriate) by a different layer entirely.
**Warning signs:** any SEC-04 test case using a tool name that never appears in `multiplexedClassifiers` (`classify.go:27-31`) and has no explicit `Mutating: true` in its `Spec()` will silently classify `Safe` and fail a `Deny` assertion.

### Pitfall 3: The Break-Glass CLI Path Silently Mints Dead-on-Arrival Tokens (D-06b)
**What goes wrong:** after threading the pepper through `HashLookupToken`'s new signature and the online serve path, the offline `aura identity recover <name>` CLI command is missed — it still calls `mintBreakGlassToken` with the OLD (unpeppered) signature, or is threaded with an empty/wrong `AURA_AUTHULA_SECRET` because the operator's shell doesn't have the same `.env` sourced as the running `aura serve` process. The resulting `token_hash` never matches what the online lookup computes, so the printed one-time token is a **silent dead end** — no error at mint time, just a mysterious "invalid token" when the user tries to use it.
**Why it happens:** `identity.go:61` calls `identityRecover(ctx, store, pool, args[1:])` with **no `cfg` parameter today** (confirmed — `[VERIFIED: identity.go:61]`), and `runIdentity` (`identity.go:33`) uses `config.LoadDB()` with **no call to `Validate()`/`ValidateProfile()` anywhere in the dispatch** — so there is currently no code path that would catch a missing `AURA_AUTHULA_SECRET` before minting.
**How to avoid:** (1) thread `cfg.AuthulaSecret` (or the derived pepper) through `identityRecover` → `mintBreakGlassToken` → the `HashLookupToken` calls at `recovery.go:96,104`; (2) add an explicit presence guard immediately after `cfg := config.LoadDB()` in `runIdentity`, specific to the `"recover"` case, that exits non-zero with a clear message if `strings.TrimSpace(cfg.AuthulaSecret) == ""` — `config.LoadDB()` already populates this field from `os.Getenv("AURA_AUTHULA_SECRET")` via `loadBase()` (`[VERIFIED: config.go:332-334,471]`), so the guard is a simple, cheap addition, not a plumbing project. Note there is **no way to cryptographically verify** "is the same value serve uses" across two separate process invocations without leaking secret material — the guard is necessarily operational (fail if unset) rather than a true equality check; document this limitation rather than over-engineering a false sense of verification.
**Warning signs:** a break-glass recovery that "succeeds" (prints a token, no error) but the user's subsequent reset attempt 404s / "invalid token"s is the signature of this bug — it will not surface in any single-process test, only in a test that mints via the CLI path and redeems via the serve path in separate process invocations (or two separately-constructed peppers in a test).

### Pitfall 4: A Plaintext Fallback on Missing Trace-Encryption Key Reopens F-021 (D-13)
**What goes wrong:** the encrypted sink is wired, but when `AURA_TRACE_ENCRYPT_KEY` is unset or malformed, the code degrades to writing the trace in plaintext "so the operator isn't blocked" — silently recreating the exact cleartext-reasoning-trace-on-disk exposure (F-021) this phase exists to close.
**Why it happens:** fail-open is the natural instinct for a debug/observability feature ("don't lose the trace, just warn") — this is the opposite of the correct posture here, because `AURA_REASONING_TRACE=full` is already gated behind an explicit `AURA_TRACE_FULL_ACK=1` operator acknowledgment (D-12); by the time the sink runs, the operator has already opted into a mode that may persist PII/credentials/prompts, so a silent plaintext write is a broken promise, not a convenience.
**How to avoid:** mirror `deriveObjectStoreKey`'s exact fail-closed shape (`[VERIFIED: identity_store.go:196-197]` — a hard length/format check that returns an error, never a default key) for `AURA_TRACE_ENCRYPT_KEY`: if the key is absent or malformed when a full-trace write is attempted, **refuse to write that record** (log a warning once, drop the record) rather than fall back to plaintext.
**Warning signs:** any code path in the new `internal/tracesink` package that has a branch writing raw/unencrypted bytes to the trace file is the tell — there should be exactly one write path, and it should be unreachable without a valid key.

### Pitfall 5: The D-19 Workflow-Pin Regex Must Handle Multi-Segment Action Paths and `run: |` Bodies
**What goes wrong:** a naive regex like `^\s*uses:\s*[\w-]+/[\w-]+@` (assuming exactly one `owner/repo` segment) fails to match `github/codeql-action/init@v4` and `github/codeql-action/analyze@v4` (three path segments), so the gate silently skips checking them — exactly the two refs D-19 flags as needing to share one SHA. Separately, a check for `go install .*@latest` that only scans lines starting with `run:` (not the indented body lines under a YAML block scalar `run: |`) misses every actual tool-install line in this repo, since **all** of them are written as multi-line `run: |` blocks (`[VERIFIED: ci.yml:78-81, ci.yml:158-161, skills.yml:132-134 — every install is inside a `run: |` block, never a single-line `run:`]`).
**Why it happens:** GitHub Actions YAML allows both single-line `run: command` and block-scalar `run: |\n  line1\n  line2`; a regex written against only the first shape will compile-and-pass locally against a hand-picked example but silently no-op against this repo's real files.
**How to avoid:** (1) for the SHA-pin check, match `uses:\s*(\S+)@([0-9a-f]{40})\s*#\s*v\S+` where the ref portion before `@` is matched as one opaque non-whitespace token (correctly handles any number of `/`-separated path segments, `./local-action`, and `docker://image@sha256:...` forms need a separate allow-pattern per D-19); (2) for the `@latest`/`go install` check, do **not** anchor on `^\s*run:` — instead scan every non-blank line of the file (or every line inside a step's `run:` scalar block, tracked via YAML block-scalar indentation) for the substring `@latest` combined with `go install`. Test both checks against this repo's actual `ci.yml`/`skills.yml` content (not synthetic examples) before considering the gate done — CONTEXT.md's own three-reviewer adversarial pass caught exactly this class of mistake once already (the original regex draft would have false-flagged `version: "~> v2"` in `release.yml:55`, a goreleaser-action *input*, not a `uses:` ref).
**Warning signs:** the gate passing green against a workflow file containing an unpinned `github/codeql-action/analyze@v4` or an un-caught `go install golang.org/x/tools/cmd/deadcode@latest` inside a `run: |` block is proof the regex is under-matching — write the self-test (`workflow_pin_gate_test.sh`) against fixture files that reproduce these exact shapes before trusting the gate.

## Code Examples

### Resolving the 14 GitHub Action Refs to Exact SHA + Version Comment (SEC-05 D-20)

All 13 currently-used action refs plus the 1 new ref needed for D-17, resolved live against the GitHub API on 2026-07-22. **Re-resolve at plan/execute time** using the same command — these are correct as of research time but the tags are mutable and newer patches may exist by execution:

```bash
# The exact command to re-resolve any tag → SHA (re-run before committing the pins):
gh api repos/<owner>/<repo>/commits/<tag> --jq '.sha'
```

| # | Current ref in repo | Exact semver (resolved) | SHA (resolved 2026-07-22) | Files using it |
|---|---|---|---|---|
| 1 | `actions/checkout@v7` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` | ci.yml (×many), codeql.yml, release.yml, skills.yml |
| 2 | `actions/setup-go@v7` | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` | ci.yml, codeql.yml, release.yml, skills.yml |
| 3 | `actions/cache@v6` | v6.1.0 | `55cc8345863c7cc4c66a329aec7e433d2d1c52a9` | ci.yml |
| 4 | `docker/setup-buildx-action@v4` | v4.2.0 | `bb05f3f5519dd87d3ba754cc423b652a5edd6d2c` | ci.yml, release.yml |
| 5 | `docker/build-push-action@v7` | v7.3.0 | `53b7df96c91f9c12dcc8a07bcb9ccacbed38856a` | ci.yml |
| 6 | `dorny/paths-filter@v4` | v4.0.2 | `7b450fff21473bca461d4b92ce414b9d0420d706` | ci.yml (×5) |
| 7 | `actions/setup-node@v7` | v7.0.0 | `820762786026740c76f36085b0efc47a31fe5020` | ci.yml (×5) |
| 8 | `github/codeql-action/init@v4` | v4.37.3 | `e4fba868fa4b1b91e1fdab776edc8cfbe6e9fb81` | codeql.yml — **must share this exact SHA with #9** |
| 9 | `github/codeql-action/analyze@v4` | v4.37.3 | `e4fba868fa4b1b91e1fdab776edc8cfbe6e9fb81` | codeql.yml — same repo/tag as #8, confirmed identical SHA |
| 10 | `docker/setup-qemu-action@v4` | v4.2.0 | `96fe6ef7f33517b61c61be40b68a1882f3264fb8` | release.yml |
| 11 | `crazy-max/ghaction-github-runtime@v4` | v4.0.0 | `04d248b84655b509d8c44dc1d6f990c879747487` | release.yml |
| 12 | `docker/login-action@v4` | v4.4.0 | `af1e73f918a031802d376d3c8bbc3fe56130a9b0` | release.yml |
| 13 | `goreleaser/goreleaser-action@v7` | v7.2.3 | `f06c13b6b1a9625abc9e6e439d9c05a8f2190e94` | release.yml |
| 14 (new, D-17) | `anchore/sbom-action/download-syft` | v0.24.0 | `e22c389904149dbc22b58101806040fa8d37a610` | release.yml (new step, before goreleaser) |

Example resulting line shape (the mandatory `# vX.Y.Z` comment — Dependabot only updates SHA pins that carry one):
```yaml
- name: Checkout
  uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
```

**Pitfall discovered while resolving these:** `actions/cache`'s GitHub `/releases` API endpoint (ordered by release *publish time*, not tag semver) listed `v5.1.0` as `.[0]` even though `v6.1.0` is the actual latest tag — its release notes were apparently edited/republished more recently than v6.1.0's. **Always cross-check via `/tags` (ordered by tag creation) and via `commits/<moving-tag>` matching `commits/<exact-semver-tag>`, never trust `/releases[0]` alone** when resolving "the latest version" for a pin.

### go.mod `tool` Directive — Verdict for D-20's Discretion Item

`[CITED: go.dev/doc/go1.24 — crypto/hkdf release note; WebSearch cross-verified against multiple 2026-current sources (Alex Edwards, ITNEXT, rednafi.com) describing `go get -tool` and `tool (...)` block syntax]`

Go 1.24 (Aura is on 1.26.5) introduced `go get -tool <module>@<version>` which adds both a `require` line and a `tool` directive to go.mod; the tool then runs via `go tool <name>` (using the package's last path segment) instead of a separately-installed binary. This gives Dependabot's `gomod` ecosystem visibility into tool versions — exactly the gap D-20's discretion note names ("so they don't rot like `golangci-lint@v2.12.2`/`sqlc@v1.31.1`").

**Recommendation, scoped exactly to D-20's named tools (not a blanket migration):**
- **`golang.org/x/tools/cmd/deadcode` → migrate to `tool` directive.** Zero go.sum cost: `golang.org/x/tools v0.48.0` is **already** a direct requirement (`[VERIFIED: go.mod:46]`) at the exact latest version — adding the `tool` line costs nothing.
- **`golang.org/x/vuln/cmd/govulncheck` → migrate to `tool` directive.** Low cost: `golang.org/x/vuln` is not currently in go.mod, but it is a lean, Go-team-maintained module in the highest-trust namespace — `go get -tool golang.org/x/vuln/cmd/govulncheck@v1.6.0` adds one clean new `require` line.
- **`github.com/avito-tech/go-mutesting/cmd/go-mutesting` → optional, evaluate before deciding.** It has no tagged releases (pseudo-version only), so the ergonomic win is smaller, and its transitive dependency footprint on go.sum was not measured this session (`go mod tidy` was not run against a `tool`-directive draft). A plain `go install ...@<pinned-pseudo-version>` step (still exact-version-pinned, satisfying D-20's "no `@latest`" requirement) is an equally valid, lower-risk fix if go.sum bloat turns out to be a concern.
- **`golangci-lint`/`sqlc` → out of scope.** D-20 does not name these; they are already exact-pinned and stable. Migrating them is a reasonable *future* refactor-on-touch, not required by any locked decision this phase.

The Makefile's `tools:` target (`staticcheck`, `goimports`, `lefthook`, `dupl`, `gotestsum` — none of which are CI-blocking-gate tools) is explicitly **out of scope** for this phase; only the 3 tools named in D-20 (deadcode, govulncheck, go-mutesting) are candidates.

### Reproducing D-07's Local CodeQL Scan (resolving the exact query pack)

`[VERIFIED: .github/codeql/codeql-config.yml:16, .github/workflows/codeql.yml:53-58]`

CI's exact pinned query pack is `security-and-quality` (not `security-extended` — confirm this exact suite name when replicating locally):

```bash
# One-time: install the official CodeQL CLI via the gh extension (gh is already
# authenticated on this machine — zero additional setup).
gh extension install github/gh-codeql

# Build a database for the Go analysis (mirrors codeql-action/init's build-mode: autobuild).
gh codeql database create aura-go-db --language=go --command="go build ./..."

# Run the SAME query suite CI uses.
gh codeql database analyze aura-go-db codeql/go-queries:codeql-suites/go-security-and-quality.qls \
  --format=sarif-latex --output=results.sarif

# Inspect results.sarif for go/weak-sensitive-data-hashing on recovery_hash.go.
```

**Caveat (Assumption A1 below):** `gh-codeql`'s bundled CLI/query-pack version may not be byte-identical to `github/codeql-action@v4.37.3`'s bundled version. If the local scan and CI disagree, trust CI's actual run (push to a branch, let `codeql.yml` execute, read the Security tab / SARIF) as the authoritative signal — the local scan is a fast pre-commit sanity check, not a substitute for the real gate.

## State of the Art

| Old Approach (current Aura state) | Current/Recommended Approach | When Changed | Impact |
|--------------------------------|------------------------------|---------------|--------|
| Floating GitHub Action tags (`@v7`, `@v4`, ...) | SHA-pinned with `# vX.Y.Z` comment | OpenSSF Scorecard's "Pinned-Dependencies" check + the post-tj-actions-2025 supply-chain incident hardened this into baseline guidance industry-wide | closes the mutable-tag attack surface (a compromised maintainer account or compromised tag re-push can no longer silently swap what a workflow runs) |
| `go install pkg@latest` in CI | Exact version pin (or a `go.mod tool` directive) | Same OpenSSF/SLSA supply-chain hardening wave | reproducible builds; a compromised/broken new release of a dev tool can no longer break CI silently on the next run |
| Plain SHA-256 for a lookup-by-hash token (`HashLookupToken`) | HMAC-SHA-256 with an HKDF-derived server-side pepper | CodeQL's `go/weak-sensitive-data-hashing` query (added 2.23.7, Dec 2025) started flagging this class in Go for the first time | a database leak alone no longer lets an attacker forge or offline-enumerate valid reset tokens — forging now additionally requires the server-side secret |
| Wildcard/credential-agnostic CORS consideration | Same-origin only (no CORS headers) for a `go:embed`-served SPA with no real cross-origin consumer | This phase's own adversarial-validation pass (2026-07-22) — not an external dated trend, an internal correction | eliminates the CORS wildcard finding AND avoids introducing new CSRF surface a credentialed allowlist would have required re-evaluating |
| Single-decode `Decode(&body)` with bare `io.EOF` tolerance for "flexible" JSON bodies | Two-decode (data + trailing-must-be-EOF) strict JSON parsing | This is a long-standing (pre-2020) Go idiom (Alex Edwards popularized it), but this repo had never applied the *second* half of it | closes JSON body-smuggling / trailing-payload confusion attacks on privileged routes |
| `golang.org/x/crypto/hkdf` (external import) | `crypto/hkdf` (stdlib) | Go 1.24 (Feb 2025) | Aura is already on the stdlib path (`identity_store.go`) — this phase should continue that pattern, not introduce the older x/crypto import for the new HKDF uses |

**Deprecated/outdated:** nothing in this phase's scope is deprecated Go API — every primitive used (crypto/hkdf, crypto/aes, crypto/cipher, encoding/json) is current stdlib on Aura's Go 1.26.5 toolchain.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `gh-codeql`'s bundled CodeQL CLI/query-pack version will produce results consistent with CI's `github/codeql-action@v4.37.3` for the `go/weak-sensitive-data-hashing` query specifically | Code Examples — D-07 local CodeQL reproduction | Low-medium: if the local scan disagrees with CI (e.g., local says "clears" but CI still flags it), D-07's 3-strike budget could be spent chasing a local-tool version mismatch rather than the real finding. Mitigation already stated: trust the real CI run as authoritative if they disagree. |
| A2 | Recommended HKDF info labels (`"aura-reset-token-pepper-v1"`, `"aura-reasoning-trace-sink-v1"`) are new suggestions extending the observed `"aura-<domain>-<purpose>-v1"` convention from `identity_store.go` — not literally present anywhere else in the codebase | Architecture Patterns — Pattern 1 | Very low: any domain-separated, documented label satisfies D-06/D-13's actual requirement; these are naming suggestions, not a functional dependency. The planner/executor is free to pick different label strings as long as they're domain-separated. |
| A3 | `go-mutesting`'s transitive dependency footprint on `go.sum` if migrated to a `tool` directive was not empirically measured (no `go mod tidy` dry-run performed against a draft `tool` line) | Code Examples — go.mod tool directive verdict | Low: this is explicitly flagged as "optional, evaluate before deciding" in the same section — no decision was forced on unverified grounds; the fallback (plain pinned `go install`) is presented as equally valid. |
| A4 | The exact per-call-site `allowEmpty` value for `onboarding_api.go:279`'s decoder (inferred from a sibling call site's `maxRunBodyBytes` use at `:301`, not from the exact same line) | Architecture Patterns — Pattern 3 per-route table | Low: worth a 30-second re-check at plan time (`grep -n maxRunBodyBytes onboarding_api.go`) before finalizing the route's `decodeOpts`; the existing decode call at :279 is a plain `Decode` with no EOF tolerance, so `allowEmpty: false` is very likely correct regardless. |

**If this table is empty:** N/A — four low-risk assumptions are logged above; none affect a locked decision, all are either self-correcting (A1's CI-is-authoritative fallback) or trivially re-verifiable in under a minute at plan/execute time (A3, A4).

## Open Questions

1. **Is `vulncheck` actually configured as a required branch-protection status check?**
   - What we know: the `vulncheck` job in `ci.yml` runs `govulncheck` with no flags, exiting 3 (job-red) on any reachable finding — this is a real, functioning gate at the job level `[VERIFIED: ci.yml:145-161]`.
   - What's unclear: whether GitHub's branch-protection rules for `master` actually list `vulncheck` as a *required* status check — this setting lives in repository configuration, not in any file in the repo, so it cannot be verified by reading the tree.
   - Recommendation: the planner should add a manual verification step to the acceptance checklist (e.g., `gh api repos/chetto1983/aura/branches/master/protection --jq '.required_status_checks.contexts'` and confirm `vulncheck` appears), per D-18's own explicit adversarial caveat. Do not claim "blocks merges" in the phase's acceptance evidence without this confirmation.

2. **Does the local `gh-codeql` CLI need explicit query-pack version pinning to exactly match `github/codeql-action@v4.37.3`'s bundled CodeQL engine version?**
   - What we know: `gh codeql` self-manages CLI version via a "release"/"nightly" channel with `set-version` for pinning `[CITED: github.com/github/gh-codeql]`.
   - What's unclear: whether the DEFAULT (unpinned) `gh codeql` version at install time will match the specific CodeQL engine bundled with `codeql-action@v4.37.3` closely enough for query results to be identical for this one query.
   - Recommendation: if D-07's local scan and a subsequent CI run disagree, do not spend the 3-strike budget debugging the local tool — treat CI's SARIF output as ground truth immediately.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all seven SEC items (build/test) | ✓ | 1.26.5 (native Windows) | — |
| `gh` CLI (authenticated) | D-07 fallback, D-20 SHA resolution, D-18 branch-protection check | ✓ | 2.87.3, authenticated as `chetto1983` with `repo`+`workflow` scopes | — |
| Docker Desktop | `db_integration`/`neo4j_integration` test tiers referenced in Validation Architecture | ✓ | Client 29.6.1, daemon running (`docker-desktop`, 8 CPUs, 15.47GiB) | — |
| **CodeQL CLI** | D-07's literal "run a local CodeQL scan" step | **✗** | — (only a query-pack cache dir exists at `~/.codeql/packages/codeql`, no CLI binary found anywhere searched) | `gh extension install github/gh-codeql` (official, zero-friction since `gh` is already authenticated) — see Code Examples for the exact commands. If that also proves infeasible in the execution environment, fall back further to: push a WIP branch, let `codeql.yml` run remotely, read results from the Security tab / `gh api .../code-scanning/alerts`. |
| syft (binary) | D-17's SBOM generation | not checked locally (CI-only concern — installed fresh in `release.yml` via `anchore/sbom-action/download-syft`, never needs to run on a dev machine) | v1.49.0 (latest) | N/A — not a local dependency |
| WSL (for `-race`, `db_integration`, mutation) | Validation Architecture's coverage/mutation tiers, per CLAUDE.md's standing "WSL is the primary dev environment" rule | assumed available per project convention (not re-probed this session — CLAUDE.md documents this as already-working infrastructure) | — | — |

**Missing dependencies with no fallback:** none — the one gap found (CodeQL CLI) has a documented, low-friction fallback.

**Missing dependencies with fallback:**
- CodeQL CLI — install via `gh extension install github/gh-codeql`, or fall back to a remote CI run if local install is infeasible in the execution sandbox.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go.uber.org/goleak` (already a direct dep, `go.mod:38`) |
| Config file | none — Go's native `go test` with build tags (`db_integration`, `neo4j_integration`, `cot_eval`), matching the project-wide convention documented in CLAUDE.md |
| Quick run command | `go test ./internal/gateway/ ./internal/agui/ ./internal/conversations/ ./internal/reasoningtrace/ ./internal/secret/ ./internal/config/` (untagged unit tier, seconds) |
| Full suite command | `bash scripts/coverage_gate.sh` (owned-surface ≥85% floor across the tag matrix) — run in WSL per CLAUDE.md's standing environment rule |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SEC-04 | Injected shell/file/MCP tool calls are denied under `server_production` (deterministic gate) | unit | `go test ./internal/gateway/ -run TestInjectionSuite -v` | ❌ Wave 0 — new `injection_suite_test.go` |
| SEC-04 | Negative controls (`fs_read`, `document_search`, MCP ReadOnlyHint, `skill{list}`) are still Allowed | unit | same file, same run, distinct sub-tests | ❌ Wave 0 |
| SEC-04 | LLM injection-resistance (honest-scope, NOT CI-blocking) | manual-only, `cot_eval` tag | `go test -tags cot_eval -run TestInjectionCoTEval -timeout 600s -v ./internal/eval/` (requires `OPENROUTER_API_KEY`, run by a human per no-unsolicited-paid-runs) | ❌ Wave 0 — new `injection_cot_eval.go`/`_test.go` |
| SEC-09 | `HashLookupToken` produces a deterministic, peppered HMAC (pure function) | unit | `go test ./internal/agui/ -run TestHashLookupToken` | ❌ edit existing test file (check for `recovery_hash_test.go`) |
| SEC-09 | Online mint → lookup round-trip resolves the SAME identity post-pepper-threading | `db_integration` | `go test -tags db_integration -race -count=1 -p 1 ./internal/agui/ -run TestPasswordReset` (existing tier, extend) | check for existing `serve_password_reset_test.go`/`password_reset_test.go` at plan time |
| SEC-09 | Break-glass CLI mint + guard-fails-on-empty-secret | unit + `db_integration` | new test in `cmd/aura/recovery_test.go` (check if it exists) exercising `runIdentity`'s new guard, plus a `db_integration` round-trip mirroring the online case across two separate invocations | ❌ Wave 0 for the guard; extend existing for the round-trip |
| SEC-09 | CodeQL `go/weak-sensitive-data-hashing` resolves (or is documented-FP-dismissed) | manual-only | `gh codeql database analyze ...` (see Code Examples) — **not automatable in CI-gate form this phase**; the real gate is the existing `codeql.yml` schedule/push trigger, already wired | N/A — infra-gated, not a new test file |
| SEC-01 | Configured secrets never persist verbatim in turn body, spill, or `.result` sidecar | unit (pure exact-match function) + `db_integration` (full round-trip through `appendTurnWrites`) | `go test ./internal/secret/ -run TestRedactExact` (unit) + `go test -tags db_integration -race -count=1 -p 1 ./internal/conversations/ -run TestAppendTurnRedaction` | ❌ Wave 0 for both — new test files |
| SEC-01 | `agent/tools.NewResult`'s `.result` sidecar write is redacted | unit | `go test ./internal/agent/tools/ -run TestNewResultRedaction` (extend existing `result_test.go`) | edit existing file |
| SEC-01 | `reasoningtrace.Record` folds `redact.String` (DSN/credential-URL leak closed) | unit | `go test ./internal/reasoningtrace/ -run TestRecordRedaction` (check for existing `reasoningtrace_test.go`, extend or add) | check at plan time |
| SEC-01 | `gateReasoningTraceFull` fails closed under `server_production` without ACK | unit | `go test ./internal/config/ -run TestGateReasoningTraceFull` (extend existing `config_validate_test.go`) | edit existing file |
| SEC-01 | Encrypted sink: fresh nonce per record, fail-closed on missing/malformed key, round-trip encrypt/decrypt | unit | `go test ./internal/tracesink/ -run TestSink` | ❌ Wave 0 — new package |
| SEC-02 | No `Access-Control-*` header is ever emitted; same-origin requests still work | unit (httptest) | `go test ./internal/agui/ -run TestNoCORSHeaders` (new sub-test, likely in an existing `server_test.go`) | check at plan time |
| SEC-03 | Non-loopback bind refused by default; unsafe-without-token refused; unsafe+token binds with a warning log | unit | `go test ./cmd/aura/ -run TestConsoleBindGuard` (new test file, e.g. `integrations_console_test.go` if absent) | check at plan time |
| SEC-06 | Strict-decode helper: trailing JSON rejected, unknown field rejected, empty body per `allowEmpty`, wrong content-type rejected, oversized body rejected (table-driven) | unit | `go test ./internal/agui/ -run TestStrictDecodeJSON -v` | ❌ Wave 0 — new `strict_decode_test.go` |
| SEC-06 | Each of the 6 acceptance-set routes actually uses the new helper (regression, not just unit-tested in isolation) | unit (httptest per route) or a static grep-based structural test | extend the existing per-route test files (`approvals_api_test.go`, etc. — check at plan time) | check at plan time |
| SEC-05 | `scripts/workflow_pin_gate.sh` correctly accepts/rejects fixture YAML (multi-segment paths, `run: \|` bodies, goreleaser `version:` input false-positive) | shell self-test | `bash scripts/workflow_pin_gate_test.sh` (mirrors `quality_snapshot_gate_test.sh`'s tmpdir+fixture+rc-assertion pattern) | ❌ Wave 0 — new script + self-test |
| SEC-05 | `goreleaser` config is syntactically valid with the new `sboms:` block | manual/local smoke | `goreleaser check && goreleaser build --snapshot --clean` (documented at `.goreleaser.yaml:2`, already the established local-validation command) | N/A — existing command, new config to validate against it |
| SEC-05 | govulncheck / deadcode / go-mutesting pinned to exact versions, no `@latest` anywhere | CI job (the new workflow_pin_gate job itself covers this) | covered by the workflow_pin_gate test above | — |

### Sampling Rate
- **Per task commit:** the untagged unit tier for the package(s) touched (seconds) — e.g. `go test ./internal/gateway/` after a SEC-04 commit.
- **Per wave merge:** `go test -race -tags 'db_integration neo4j_integration' -count=1 -p 1 ./internal/...` (the full tagged matrix, WSL) for any wave touching persistence (SEC-01, SEC-09).
- **Phase gate:** `bash scripts/coverage_gate.sh` full suite green before `/gsd-verify-work` — **verify the `db_integration`-only Skills-job number specifically** (per CLAUDE.md's standing rule: this is the stricter of the two coverage gates and the one to report at phase close), not just the Knowledge job's `db_integration neo4j_integration` number.

### Wave 0 Gaps
- [ ] `internal/gateway/injection_suite_test.go` — covers SEC-04's deterministic-gate acceptance criterion
- [ ] `internal/eval/injection_cot_eval.go` + `_test.go` — covers SEC-04's D-04 LLM-resistance tier (manual-only, `cot_eval` tag)
- [ ] `internal/secret/redact_exact.go` + `_test.go` — covers SEC-01's D-10 inbound exact-match detector (pure unit)
- [ ] `internal/conversations/` — a `db_integration`-tagged test proving redaction survives the full `appendTurnWrites` round-trip (inline AND spill) — new test file or extend `store_test.go`
- [ ] `internal/tracesink/sink.go` + `_test.go` — covers SEC-01's D-13 encrypted sink (new package, pure unit — encrypt/decrypt round-trip + fail-closed-on-bad-key)
- [ ] `internal/agui/strict_decode.go` + `_test.go` — covers SEC-06's D-16/D-16b centralized helper (pure unit, table-driven, this is the single highest-value test file in the phase for mutation-score purposes since the trailing-JSON logic is exactly the kind of boundary condition mutation testing excels at catching)
- [ ] `scripts/workflow_pin_gate.sh` + `workflow_pin_gate_test.sh` — covers SEC-05's D-19 gate (shell self-test, fixture-file based)
- [ ] `cmd/aura/` — a test covering the D-06b break-glass presence guard (new or extend existing `identity_test.go`/`recovery_test.go` — check at plan time which exists)
- Framework install: none — `go.uber.org/goleak` and `testing` are already wired project-wide; no new test framework needed for any of the seven items.

*(No gaps for SEC-02/SEC-03: these are edits to already-tested files — `internal/agui`'s existing `server_test.go`/console tests just need new sub-tests, not new frameworks.)*

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (SEC-09) | keyed-hash (HMAC-SHA-256) credential-material hashing instead of a fast unsalted/unkeyed hash — this phase's own primary fix |
| V3 Session Management | yes (SEC-02) | `SameSite=Strict` + `Secure` cookie (already shipped, `auth_cookie.go:129-130`) is the sole CSRF control; same-origin-only removes the CORS wildcard without weakening this |
| V4 Access Control | yes (SEC-03) | the validation console's loopback-by-default + explicit-unsafe-flag-plus-token pattern is a standard "secure by default, explicit escalation" access-control shape |
| V5 Input Validation | yes (SEC-06) | strict JSON decoding (size cap, content-type check, unknown-field rejection, trailing-data rejection) on every privileged route |
| V6 Cryptography | yes (SEC-01, SEC-09) | HKDF (RFC 5869) for key derivation, AES-256-GCM (or XChaCha20-Poly1305) for authenticated encryption, HMAC-SHA-256 for keyed hashing — never hand-rolled, all via `crypto/*` stdlib or the already-vetted `golang.org/x/crypto` |
| V14 Configuration | yes (SEC-05) | SBOM publication, blocking vulnerability scanning, and SHA-pinned CI dependencies are the standard "software supply chain" ASVS/SLSA controls |

### Known Threat Patterns for This Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Prompt injection coercing a mutating tool call | Elevation of Privilege | the `gateway.Decide` PEP's fail-closed deny under `server_production` when no interactive human responder is present (this phase's SEC-04 acceptance gate) |
| Offline hash-lookup-table attack against a leaked `token_hash` column | Information Disclosure / Spoofing | keyed hash (HMAC with a server-side secret) instead of a bare fast hash — a DB leak alone no longer enables forging or scanning valid tokens (SEC-09) |
| Secret exfiltration via a tool that dumps environment/config into its own output, later re-displayed or exported | Information Disclosure | dual-layer redaction: exact-match inbound (protects Aura's own configured secrets at every at-rest surface) + pattern-based outbound (catches unknown secret-like values at the display/export boundary) (SEC-01) |
| Cross-origin credentialed request exploiting a permissive/wildcard CORS policy | Tampering / Information Disclosure | same-origin-only, zero CORS headers, for an application with no legitimate cross-origin consumer (SEC-02) |
| An operator accidentally exposing a debug/validation console beyond loopback | Elevation of Privilege / Information Disclosure | secure-by-default loopback bind + an explicit, logged, token-gated escalation path for the rare legitimate non-loopback need (SEC-03) |
| JSON body-smuggling via trailing payload data after a valid first JSON value | Tampering | two-decode idiom: the mandatory second `Decode` must observe `io.EOF` (SEC-06) |
| Supply-chain compromise via a re-tagged/re-pointed GitHub Action or a floating `@latest` CI tool install | Tampering (of the build pipeline itself) | SHA-pinning every Action reference with a Dependabot-trackable version comment; pinning every CI tool install to an exact version (SEC-05) |

## Sources

### Primary (HIGH confidence — direct file reads at HEAD, 2026-07-22)
- `D:\Aura\.planning\phases\40-security-supply-chain-pack\40-CONTEXT.md` — the locked decisions and canonical refs this research verifies
- `D:\Aura\.planning\REQUIREMENTS.md` §SEC — SEC-01..06, SEC-09 definitions
- `D:\Aura\internal\gateway\{decide,approve,gateway,reserve,classify}.go`, `internal\scoring\scoring.go`
- `D:\Aura\internal\agui\recovery_hash.go`, `cmd\aura\{identity,recovery,serve_password_reset}.go`, `internal\agui\password_reset.go`, `internal\db\queries\identity_recovery.sql`, `internal\db\migrations\0023_identity_recovery.up.sql`
- `D:\Aura\internal\conversations\{store_append,store_branch}.go`, `internal\agent\tools\result.go`, `internal\toolinvocations\store.go`, `internal\redact\string.go`, `internal\secret\envkey.go`, `internal\reasoningtrace\reasoningtrace.go`
- `D:\Aura\internal\config\{config,config_validate,config_knobs,config_runtimeprofile}.go`
- `D:\Aura\internal\agui\{server_cors,auth,auth_cookie,server}.go`, `cmd\aura\{integrations_console,integrations_proxy}.go`
- `D:\Aura\internal\agui\{server_run_request,approvals_api,onboarding_api,assets_api,governance_write_api,governance_write_skills_api,conversations_api,governance_write_scheduler,connect_api}.go`
- `D:\Aura\.github\workflows\{ci,codeql,release,skills}.yml`, `D:\Aura\.goreleaser.yaml`, `D:\Aura\.github\dependabot.yml`, `D:\Aura\.github\codeql\codeql-config.yml`
- `D:\Aura\internal\objectstore\identity_store.go` (the HKDF+AES-GCM idiom mirrored throughout)
- `D:\Aura\go.mod`, `D:\Aura\.golangci.yml`, `D:\Aura\Makefile`, `D:\Aura\scripts\quality_snapshot_gate_test.sh`
- `gh api repos/*/{commits,tags,releases}` — 20+ live GitHub API queries resolving exact SHAs/versions for all 13+1 action refs, 2026-07-22
- `go list -m *@latest` against the live Go module proxy — govulncheck/deadcode/go-mutesting version resolution, 2026-07-22
- `D:\tmp\authula\{internal\security\hmac_signer.go, plugins\oauth2\services\crypto.go, plugins\jwt\services\refresh_token_service.go, internal\repositories\crypto_token_repository.go}` — the cited HMAC/hash precedents, read in full
- `D:\tmp\spike-librechat\{packages\data-schemas\src\crypto\index.ts, packages\api\src\agents\hitl\policy.spec.ts}` — the cited hashToken + policy-test precedents, read in full

### Secondary (MEDIUM confidence — official docs, WebSearch cross-verified)
- [GoReleaser SBOMs documentation](https://goreleaser.com/customization/sbom/) — confirmed `sboms:` block syntax, `artifacts: archive|source|binary|...` options
- [anchore/sbom-action](https://github.com/anchore/sbom-action) — confirmed the `download-syft` sub-action exists and is the purpose-built way to install syft in CI
- [Go 1.24 Release Notes](https://go.dev/doc/go1.24) — `crypto/hkdf` stdlib addition, RFC 5869
- [Managing Tool Dependencies in Go 1.24+ (Alex Edwards)](https://www.alexedwards.net/blog/how-to-manage-tool-dependencies-in-go-1.24-plus) and related 2026-current sources — `go get -tool`/`tool (...)` directive syntax
- [Setting up the CodeQL CLI (GitHub Docs)](https://docs.github.com/en/code-security/how-tos/find-and-fix-code-vulnerabilities/scan-from-the-command-line/setting-up-the-codeql-cli) and [github/gh-codeql](https://github.com/github/gh-codeql) — local CodeQL CLI install path

### Tertiary (LOW confidence — none used without cross-verification this session)
- None — every WebSearch finding used in this document was cross-verified against either an official doc page, the live GitHub API, or the live Go module proxy before being stated as fact.

## Metadata

**Confidence breakdown:**
- Canonical-refs verification: HIGH — every citation independently re-read at HEAD; 45+ file:line matches confirmed exact, one minor path-label correction noted
- Discretion-item resolutions (SHA pins, tool versions, HKDF labels, per-route decode defaults): HIGH — resolved via live tool calls (`gh api`, `go list -m`) against authoritative sources, not training-data recall
- Validation Architecture: MEDIUM-HIGH — test-tier mapping follows the project's own well-established conventions exactly, but several "does this test file already exist?" checks are flagged for a quick re-confirmation at plan time rather than exhaustively verified this session (noted inline as "check at plan time")
- Security Domain / ASVS mapping: MEDIUM — standard, well-established mappings, not independently re-verified against the ASVS document text this session (this phase's own decisions already ARE the ASVS-standard controls, so the mapping is closer to "labeling what's already decided" than net-new research)

**Research date:** 2026-07-22
**Valid until:** ~7 days for the SHA-pin table specifically (GitHub Action tags move; re-resolve via `gh api` immediately before committing pins, do not trust this document's SHAs beyond a spot-check); ~30 days for everything else (internal architecture, established patterns, ASVS mappings are stable)
