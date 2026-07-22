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

## Claude's Discretion

- File placement, helper/identifier names, error strings, test-fixture shapes (≤600 LOC/file).
- Secret-harvest length floor + HKDF info labels; per-route allowEmpty + size-cap values.
- Whether to move `go install` tools into go.mod `tool` directives (recommended for Dependabot tracking).
- Wave ordering across the seven largely-independent items.

## Deferred Ideas

- Image-level SBOM attestation; zizmor as a blocking gate; osv-scanner / container+lockfile scanning.
- F-019 ops half (load/chaos, backup/DR drill) + honest-10/10 evidence bundle → Phase 41 (OPS/REL).
- Full all-routes strict-JSON sweep.
