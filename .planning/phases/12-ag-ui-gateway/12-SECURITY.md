---
phase: 12
slug: ag-ui-gateway
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-07
---

# Phase 12 — Security

> Per-phase security contract for the AG-UI Gateway (Slice 8): threat register, accepted risks, and audit trail.
> All declared mitigations verified against the working tree with file:line evidence (grep/read/live `go list`/`go test`), not against SUMMARY prose.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| `internal/agent` → `internal/agui` | One-way import only; the agent runtime must NOT transitively import the transport adapter (D-17). CI-enforced via `go list -deps` closure. | Go package dependency graph |
| untrusted `*agent.Event` content → AG-UI wire | The pure translator forwards LLM/tool content onto the SSE wire. Empty/odd Events and empty ids/deltas are skipped before reaching SDK `Validate()`. | LLM/tool prose, tool-call ids, usage deltas |
| local HTTP client → `POST /agent/run` | Untrusted-ish local JSON. Size-capped, content-validated, malformed-JSON-rejected. UNAUTHENTICATED by design this phase — loopback bind is the compensating control. | RunAgentInput JSON (threadId, messages, resume) |
| local HTTP client → `GET /threads/{id}/messages` | thread-id is the access chokepoint (`conv.Get`); single-user `local` v1, no cross-identity isolation yet. | Persisted conversation history (MESSAGES_SNAPSHOT) |
| agent/tool error → SSE RUN_ERROR / 4xx body | Internal error strings could leak DSN/key/path onto the wire. Redacted in-flight. | error strings |
| LLM wire (provider reasoning field) → `llm.Chunk` → agent Event → AG-UI REASONING_* / CLI 💭 | Provider CoT, byte-for-byte, stream-only (never persisted). Same loopback trust domain as assistant content. | chain-of-thought deltas |
| go get (AG-UI SDK) → build | Single external module, immutable pseudo-version pin behind go build/go.sum + CI literal grep. | third-party code |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status | Evidence |
|-----------|----------|-----------|-------------|------------|--------|----------|
| T-12-01 | Tampering | internal/agent import closure (D-17) | mitigate | `go list -deps ./internal/agent/...` closure excludes internal/agui; wired as CI step | closed | `scripts/agui_boundary_check.sh:20` (`grep -qx "${AGUI_PKG}"`); CI `ci.yml:42`; live `go list -deps ./internal/agent/...` → 0 matches |
| T-12-02 | Tampering | SDK supply chain | mitigate | Pseudo-version pin immutable via go.sum; CI greps the literal (exactly 1 match) | closed | `go.mod:30` (`v0.0.0-20260514093510-e9e910b230b9`); CI `ci.yml:96` (`[ "$(grep -cF '…' go.mod)" = "1" ]`) |
| T-12-03 | Info Disclosure | translator RUN_ERROR | mitigate | translator forwards `err.Error()`; server-side sanitization owns the surfaced string | closed | `translator.go:39` (`NewRunErrorEvent(err.Error())`); sanitized in-flight by `server.go:221,426-433` (`redactEvent`); test `TestServer_RunErrorRedaction` PASS |
| T-12-04 | DoS | malformed Event stream | mitigate | per-event Validate() + skip-empty-delta + non-empty-id; never panics on nil/odd Event | closed | `translator.go:42-44` (nil guard), `152-160`/`188-199` (lazy START, skip-empty), IDGenerator mints non-empty ids `types.go:77-83`; `TestTranslatorProperty` PASS |
| T-12-05 | DoS | Fanout producer | mitigate | cap-64 buffered subscriber channels + default: drop-with-WARN; producer never blocks | closed | `fanout.go:17` (`fanoutBuffer = 64`), `93-111` (`send`: select + `default:` drop+WARN) |
| T-12-06 | DoS | goroutine leak | mitigate | sole sender closes; every send select has `<-ctx.Done()`; goleak TestMain | closed | `fanout.go:71-84` (sole producer, `defer closeAll`), `98,104` (ctx.Done arms), `113-120` (`closeAll`); `main_test.go` goleak.VerifyTestMain |
| T-12-07 | Info Disclosure | aliased SDK types | accept | client.go only re-exports event TYPE aliases; no new data crosses a trust boundary | closed | `client.go:16-21` (`type Event = events.Event`, `type EventType = events.EventType` only); logged in Accepted Risks |
| T-12-08 | EoP | unauthenticated endpoint beyond loopback | mitigate | Bind hardcoded 127.0.0.1:9080 via config default; no --bind flag; loopback IS the control | closed | `config.go:230` (`envDefault("AURA_AGUI_BIND","127.0.0.1:9080")`); `grep -c '--bind' cmd/aura/serve.go` == 0 (per Plan 03 SUMMARY, verified) |
| T-12-09 | DoS | slow-client SSE (slow-loris) | mitigate | cap-N buffered SSE pump + drop-with-WARN; ReadHeaderTimeout-class bound on daemon | closed | `server.go:240-258` (`pumpSend`: select + `default:` drop+WARN), `196-230` (ctx.Done unwind); `bufferCap()` 275-280 |
| T-12-10 | Info Disclosure | RUN_ERROR message | mitigate | sanitizeErr/redactEvent strip DSN/key/path; redaction test with synthetic DSN | closed | `server.go:372-433` (DSN/userinfo/token regexes + `redactEvent`); applied at `221` (SSE), `140,179` (4xx); `TestServer_RunErrorRedaction` + `TestSanitizeErr` (9 cases) PASS |
| T-12-11 | Info Disclosure | cross-thread history read | mitigate | conv.Get chokepoint; unknown/malformed thread → 404; single-user local v1 | closed | `server.go:126-137` (POST uuid.Parse→404 + Get→404), `159-176` (GET same); `TestServer_MalformedThreadID404`, `TestServer_*Unknown*404` PASS; smoke 404 chokepoint `agui_smoke.sh:226-232` |
| T-12-12 | Tampering/DoS | malformed/oversized request body | mitigate | MaxBytesReader (1 MiB)→400; SDK UnmarshalJSON→400; ValidateRunInput rejects empty | closed | `server.go:29` (`maxRunBodyBytes = 1<<20`), `111` (MaxBytesReader first stmt), `113-120` (decode + ValidateRunInput→400); `TestServer_RunBadRequests` (4 subtests incl. over-cap) PASS |
| T-12-13 | Spoofing/Tampering | CSRF from malicious local web page | mitigate | ACAO `*` only behind AURA_AGUI_CORS_PERMISSIVE (default false); loopback bind | closed | `config.go:231` (default false); `server.go:90-105` (`withCORS` returns mux unchanged when off), `75-80` (Mux wraps all routes); `TestServer_CORSPermissive` (on/off + ACAO-on-error) PASS |
| T-12-14 | DoS | CI no-skip-as-green | mitigate | agui db_integration tier exports exact DSN env; t.Fatal under CI=true when unset | closed | `ci.yml:142-147` (CI=true + AURA_DB_URL/MIGRATE_URL), `179` (`./internal/agui/...` in db_integration tier), `204` (smoke step); smoke no-skip guard `agui_smoke.sh:57-64` |
| T-12-15 | Info Disclosure | live smoke output | mitigate | smoke asserts SSE FRAME ground truth (event types incl. REASONING_*), never DSN/key | closed | `agui_smoke.sh:166-205` (greps `^event:` types only), `214-224` (snapshot body), `7-8` doc ("WIRE GROUND TRUTH … never the model's prose") |
| T-12-16 | Info Disclosure | reasoning CoT content | accept | CoT crosses only loopback SSE; stream-only, never persisted; documented (ASVS L1) | closed | stream-only proven by T-12-17 evidence; loopback `config.go:230`; logged in Accepted Risks |
| T-12-17 | Tampering | reasoning leaking into persisted content | mitigate | consume loop reasoning case does NOT write to accumulated text; accumulate.go untouched | closed | `llm_agent.go:425-432` (`case c.Reasoning != ""` yields event, NO `b.WriteString`; comment 426-427); `accumulate.go` has 0 reasoning refs; `event.go:54` omitempty; `TestLlmAgent_ReasoningChunk_StreamOnly` |
| T-12-18 | Tampering | wire field confusion (accept-both) | mitigate | decode both `reasoning` (vLLM) and `reasoning_content` (DeepSeek); dual-field golden fixtures | closed | `sse.go:26-27` (both fields), `133-135` + `155` (`reasoningDelta` resolver); fixtures `reasoning-field.txt` / `reasoning-content-field.txt`; `TestStream_ReasoningDualField` |
| T-12-19 | Info Disclosure | REASONING_* CoT on AG-UI wire | accept | same loopback SSE trust domain; stream-only (Plan 05 invariant); documented (ASVS L1) | closed | translator REASONING branch `translator.go:114-122,180-215`; stream-only per T-12-17; logged in Accepted Risks |
| T-12-20 | DoS | malformed REASONING lifecycle | mitigate | per-event Validate() + skip-empty-delta + non-empty rsn- id; interrupted run closed (END pair) before next family | closed | `translator.go:33` (`closeRuns`), `188-215` (`reasoningRunState.content`/`close`: lazy START, END pair on close), `115,128` (close-on-other-family); `TestTranslatorReasoning*` (5 tests) PASS |
| T-12-SC | Tampering | supply chain (carried, plans 01/04/06) | mitigate | single external package, three live spikes provenance, immutable pin behind go build/vet gate | closed | same pin as T-12-02 (`go.mod:30` + CI `ci.yml:96`); no `[ASSUMED]`/`[SUS]`/`[SLOP]` packages; only AG-UI SDK + transitive logrus added |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-12-07 | T-12-07 | client.go re-exports only the SDK event TYPE aliases (`Event`, `EventType` — `client.go:16-21`). No new data crosses a trust boundary versus the translator; RUN_ERROR sanitization is owned by Plans 01/03. No data-plane risk. | gsd-security-auditor (ASVS L1) | 2026-06-07 |
| AR-12-16 | T-12-16 | Reasoning CoT may contain prompt-derived text, but it crosses ONLY the loopback SSE trust domain (`AURA_AGUI_BIND` default `127.0.0.1:9080`, `config.go:230`). It is stream-only — never written to `conversation_turns` (verified: `llm_agent.go:425-432` does not touch the accumulator; `accumulate.go` has zero reasoning references), so no at-rest exposure is added. At ASVS L1 loopback no new mitigation is required. | gsd-security-auditor (ASVS L1) | 2026-06-07 |
| AR-12-19 | T-12-19 | REASONING_* events ride the same loopback SSE wire as the assistant content they accompany. Stream-only (Plan 05 invariant, same code-path evidence as AR-12-16). At ASVS L1 loopback, surfacing CoT on the local wire is documented, not mitigated. | gsd-security-auditor (ASVS L1) | 2026-06-07 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-07 | 21 | 21 | 0 | gsd-security-auditor |

Verification method: each `mitigate` threat verified by grep/read of the cited file plus, where applicable, a live offline `go test ./internal/agui/` run of the named test; the boundary closure (T-12-01) verified by a live `go list -deps ./internal/agent/...` (0 matches); the SDK pin (T-12-02/T-12-SC) verified against `go.mod:30` and the CI literal-grep gate. Each `accept` threat verified by confirming the accepted-risk rationale holds against the code (reasoning stream-only path) and logged above. No `## Threat Flags` sections were present in the six SUMMARYs; plans 02 and 03 explicitly state "No new threat surface introduced beyond the register" — no unregistered flags.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-07
