# Phase 40: Security & Supply-Chain Pack - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-22
**Phase:** 40-security-supply-chain-pack
**Areas discussed:** Injection-suite architecture (SEC-04), Lookup-token hash fix (SEC-09), Redaction + full-trace policy (SEC-01), Supply-chain gate shape (SEC-05), CORS (SEC-02), Validation console (SEC-03), Strict JSON (SEC-06)

**Method:** User requested a D:/tmp + 2026-web-best-practice search for the four primary areas. Four `gsd-advisor-researcher` agents ran in parallel (agent-infra-sandbox, codex, authula, librechat, agent-memory, nanobot/picobot + web) and produced comparison tables grounded against the live Aura code before each decision.

---

## SEC-04 — Injection / tool-policy-bypass suite

| Option | Description | Selected |
|--------|-------------|----------|
| Deterministic gate now, LLM tier deferred | Deterministic policy-denial CI gate as the acceptance artifact; LLM eval registered opt-in, not built | |
| Gate + build LLM eval tier now | Also build the OPENROUTER-gated LLM injection-resistance eval this phase | ✓ |
| Deterministic gate only, no LLM tier | No LLM tier planned at all | |

**User's choice:** Gate + build LLM eval tier now.
**Notes:** Deterministic `gateway.Decide` gate IS the SEC-04 acceptance artifact (DB-free, no paid calls, CI-native); the LLM tier stays `cot_eval`-tagged / OPENROUTER-gated / not-CI-blocking. Honest-scope caveat (enforcement ≠ resistance) recorded as mandatory acceptance-evidence language. Corpus hand-written (payload text is inert to the classifier).

---

## SEC-09 — weak-hash at `recovery_hash.go`

| Option | Description | Selected |
|--------|-------------|----------|
| HMAC+pepper, FP fallback | HMAC-SHA-256 + HKDF pepper from AURA_AUTHULA_SECRET; verify CodeQL clears, else documented FP-dismissal | ✓ |
| Documented FP dismissal only | Keep SHA-256 (OWASP/Authula-identical), dismiss alert with justification | |
| Fixed-salt argon2id | Literal "salted KDF" wording; rejected (anti-pattern + memory-DoS) | |

**User's choice:** HMAC+pepper, FP fallback.
**Notes:** Deterministic lookup by `token_hash` PRIMARY KEY makes a random-salt KDF impossible → ROADMAP/PRD amendment REQUIRED before code. Local CodeQL run with CI's pinned pack decides HMAC-vs-FP-dismissal (Go query is new, Dec-2025, may still flag HMAC). No migration (≤10-min TTL).

---

## SEC-01 — redaction + full-trace policy

| Option | Description | Selected |
|--------|-------------|----------|
| Defer sink, ship ACK gate | Fail-closed ACK gate + Phase-39 24h TTL; reserve encrypt-key knob | |
| Implement encrypted sink now | Build the encrypted full-trace sink this phase, reusing in-tree AES-GCM+HKDF | ✓ |

**User's choice:** Implement encrypted sink now.
**Notes:** Enforcement seam = single choke-point `appendTurnWrites`; detector = layered (exact-match configured-secrets inbound to protect LLM re-feed fidelity, pattern-based outbound). Real bug found on touch: `reasoningtrace.redactString` leaks DSNs/`AURA_DB_URL` in full-trace mode → fix by folding in `redact.String`. Sink reuses `objectstore/identity_store.go` AES-GCM+HKDF, fresh nonce per record, no new dep.

---

## SEC-05 — supply-chain gate shape

| Option | Description | Selected |
|--------|-------------|----------|
| Binaries/archives, per-release | goreleaser syft SPDX-JSON per-release, attached to the Release | ✓ |
| Add image-level attestation | Also scan the pushed Docker image with syft post-release | |
| Per-commit CycloneDX from go.mod | cyclonedx-gomod in CI per-commit | |

**User's choice:** Binaries/archives, per-release (SBOM depth). The other three sub-questions were locked by the research as minimal-industrial: pin-everything-to-SHA (+ `# vX.Y.Z` comment for Dependabot), custom grep pin-lint gate (dodges the bootstrap paradox), reachable-any govulncheck (a fortiori ≥ "high-severity").
**Notes:** 68 `uses:` lines / zero SHA pins today; 3 `@latest` tool installs. Peer repos all float tags — pinning raises Aura's bar. Image-level SBOM + zizmor + osv-scanner deferred.

---

## SEC-02 — CORS (F-022)

| Option | Description | Selected |
|--------|-------------|----------|
| Allowlist + echo origin + credentials | New AURA_AGUI_CORS_ORIGINS; echo matched origin + Allow-Credentials, Vary:Origin, methods synced to routes; `*` dev-only | ✓ |
| Allowlist, no credentials | Omit Allow-Credentials — breaks authed cross-origin | |
| Remove CORS entirely | Same-origin only, drop the knob | |

**User's choice:** Allowlist + echo origin + credentials.
**Notes:** Grounded in the fact that auth is a session cookie (`setSessionCookie`/`r.Cookie`), so `*` is invalid for credentialed requests; the cockpit itself is same-origin. `config_validate.go:226` already Fatals permissive CORS under strict.

---

## SEC-03 — validation console (F-047)

| Option | Description | Selected |
|--------|-------------|----------|
| Unsafe flag + token | Refuse non-loopback unless `--unsafe-non-loopback` AND AURA_INTEGRATIONS_CONSOLE_TOKEN; proxy requires token; warn | ✓ |
| Refuse non-loopback, no escape | Always refuse; SSH-tunnel only | |
| Token auth always (incl. loopback) | Bearer token on every request even locally | |

**User's choice:** Unsafe flag + token.
**Notes:** Matches the requirement literally; loopback happy path stays unauthenticated. Console proxy injects the PIM admin token server-side (`integrations_proxy.go`), which is why a non-loopback bind is dangerous.

---

## SEC-06 — strict JSON decoding (F-052)

| Option | Description | Selected |
|--------|-------------|----------|
| Audit-named set + refactor-on-touch | Strict helper on /agent/run, approvals, onboarding, assets, governance MCP+skills; adopt elsewhere on touch | ✓ |
| All mutating routes | Sweep every mutating agui decoder now | |
| Every agui JSON decoder | All ~20+ decoders incl. read/query bodies | |

**User's choice:** Audit-named set + refactor-on-touch.
**Notes:** Surface is ~20+ decoders (far beyond the 3 the audit named); one centralized helper (size-cap + content-type + DisallowUnknownFields + single-decode EOF + per-route allowEmpty). Fold in the existing `conversations_api.go:162` io.EOF/allowEmpty pattern.

## Adversarial validation round (2026-07-22, post-CONTEXT)

Three parallel adversarial reviewers (refute-not-confirm mandate) checked every load-bearing claim against the live code. Outcome folded into CONTEXT.md as corrections:

- **SEC-04:** "network" has no denyable gateway tool (`web_fetch`/`web_search` are Safe→Allowed) → model as shell/MCP egress; D-03 nil-deref rationale was wrong (Normal-mutating returns Allow) → restriction kept, reason fixed.
- **SEC-09:** break-glass CLI path never receives the pepper + no env guarantee → thread pepper through all sites + presence/equality guard (else recovery tokens die silently).
- **SEC-01:** single-choke-point true only for the turns table — the `agent/tools/result.go` `.result` sidecar persists full tool output unredacted → added as a second at-rest seam; exact-match doesn't literally satisfy "secret-like values" for unknown secrets → REQUIREMENTS amendment; encrypted sink is a rewrite (unexported methods) + must be fail-closed on missing key.
- **SEC-02 — DECISION REVERSED:** the credentialed allowlist has no production cross-origin consumer, SameSite=Strict already blocks cross-site cookies, and it trips the auth.go CSRF re-eval; "methods synced to routes" is un-syncable (current list already misses PATCH/PUT) → **switched to same-origin-only, no CORS headers** (+ REQUIREMENTS amendment).
- **SEC-06:** the `conversations_api.go:162` single-decode idiom IS the trailing-JSON vuln → helper needs a second-decode-expects-EOF step; the 3 cited precedents are the bug, not the fix.
- **SEC-05:** default `sboms:` = Go-modules only (web/ not covered) → add a source-artifact SBOM; "govulncheck blocks merges" needs branch-protection confirmation; grep gate must handle 3-segment paths + indented run-body lines.

Three REQUIREMENTS/ROADMAP amendments now required before code: SEC-09 (D-08), SEC-01 (D-10b), SEC-02 (D-14b).

## Claude's Discretion

- File placement, helper/identifier names, error strings, test-fixture shapes (≤600 LOC/file).
- Secret-harvest length floor + HKDF info labels; per-route allowEmpty + size-cap values.
- Whether to move `go install` tools into go.mod `tool` directives (recommended for Dependabot tracking).
- Wave ordering across the seven largely-independent items.

## Deferred Ideas

- Image-level SBOM attestation; zizmor as a blocking gate; osv-scanner / container+lockfile scanning.
- F-019 ops half (load/chaos, backup/DR drill) + honest-10/10 evidence bundle → Phase 41 (OPS/REL).
- Full all-routes strict-JSON sweep.
