# Phase 3: LLM Client + ToolResult - Context

**Gathered:** 2026-05-30
**Status:** Ready for planning
**Method:** 6 discussion rounds (~32 HOW decisions) + targeted web/curated-source research on 3 forks (time-awareness, system-prompt structure, loop-output semantics). SPEC already locked all 14 requirements (ambiguity 0.08); this CONTEXT captures only the HOW the planner/researcher would otherwise guess.

<domain>
## Phase Boundary

Replace the LLM skeleton with a real, handrolled OpenAI-compatible SSE streaming client (OpenRouter → `deepseek/deepseek-v4-flash:exacto`), land the ToolResult preview+sidecar pattern, and drive both through a full `LlmAgent.Run` loop behind an interactive `aura chat` REPL — with budget-contract enforcement, per-call OpenTelemetry tracing (real exporter), token+USD cost reporting, and a cache-safe `current_time` builtin.

This is the **first phase where Aura actually talks to a model.** It consumes the Phase-2 cornerstone (`Agent`/`InvocationContext`/`Event`/`Budget`) by providing the first real `Agent` implementation, and it makes the OTel-shape Phase 2 deferred (SpanID minting + real exporter) genuinely live.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**14 requirements are locked.** See `03-SPEC.md` for full requirements, boundaries, and acceptance criteria (ambiguity 0.08, gate ≤0.20).

Downstream agents MUST read `03-SPEC.md` before planning or implementing. Requirements are not duplicated here — but **this CONTEXT amends two SPEC details** (A4 offset units, A5 env catalog — see `### PRD-Amendments Required`). Where this CONTEXT and SPEC conflict, the amendments here win.

**In scope (from SPEC.md):**
- Real OpenAI-compat SSE client (`internal/llm/openai_compat`): parser, tool-call accumulator, ctx-cancel, no-retry `HTTPError`, OpenRouter default + attribution headers.
- `LLMConfig` + load-order chain + `~/.aura/llm.json` read/write; `aura config` read/write of that file.
- ToolResult pattern: `(ToolResult, error)` signature, preview/sidecar spillover, `read_tool_output` builtin, session-scoped sidecar layout.
- `current_time` builtin (non-deferred) — cache-safe time awareness, RFC-3339 UTC + optional IANA timezone; live clock never in the cached prefix.
- `LlmAgent` implementing `Agent` (Run/iter.Seq2), tool dispatch, budget-contract enforcement (step/wallclock/dedup terminal Events).
- `aura chat` interactive in-memory REPL with live streaming + per-turn token+USD cost.
- OTel `llm.request` span per call with a real (stdout/OTLP) exporter.

**Out of scope (from SPEC.md):**
- Conversation **persistence** (Phase 4 / Slice 1.8; `aura chat` history is in-memory only).
- `ask_user` pause/resume (Phase 4 / Slice 1.5).
- Identity + `capability_grants` (Phase 4 / Slice 1.7).
- KV-cache builder / stable-prefix construction (Phase 6 / Slice 4) — the client reads `Messages` as given.
- Microcompact / history trimming (Phase 4 / Slice 1.8b).
- Wire-level retry (deferred to the caller; only the `HTTPError` signal is surfaced).
- New capability tools (web/sandbox/swarm) — only `text_response`, `tool_search`, `read_tool_output`, `current_time` registered.
- Conversation full-text search (Slice 1.8.5).

</spec_lock>

<decisions>
## Implementation Decisions

> All decisions are HOW (implementation), not WHAT (SPEC owns WHAT). Each is the planner's default unless research surfaces a concrete reason to deviate.

### Commit & PRD-amendment strategy
- **D-01 — N atomic sub-commits in dependency order, NOT a mega-commit.** Seven commits, Gate 2 (`go vet + build + test + race`) green between each: (1) PRD-amendment commit → (2) `LLMConfig` + load-order → (3) `internal/llm/openai_compat` SSE client + golden fixtures → (4) ToolResult coupled commit (`spec.go` + `text_response.go` + `search.go` + agent runTool path) + `read_tool_output` + sidecar helper → (5) `current_time` builtin → (6) `LlmAgent` + budget enforcement → (7) `aura chat` REPL + cost + OTel exporter. Aligned to `feedback_one_module_per_slice` + Phase 1/2 precedent. Planner may split (4) further if a file exceeds the ≤600 LOC budget, but the ToolResult signature change itself stays one coupled commit (SPEC Constraint).
- **D-02 — One combined PRD-amendment commit FIRST.** A1+A2+A3 (SPEC-declared deviations) + A4+A5 (new, discovered here) land in a single PRD-amendment commit at the head of the phase, before any implementation. Same-slice deviations; mirrors Phase 2's grouped A1–A7.

### OTel (Phase 3 becomes "the OTel slice" Phase 2 deferred)
- **D-03 — Phase 3 lands the full real OTel stack.** Phase 2 deferred both the `go.opentelemetry.io/otel` import AND SpanID minting (`InvocationContext.SpanID` is `[8]byte{}` today, `ParentSpanID` nil — see `agent.go:51-52` "DEFERRED to the OTel slice WR-04"). SPEC Req#13's real exporter forces resolution now: add the otel deps, mint SpanIDs, wire a real `TracerProvider`. No half-measure (rejected "span-only" minting — two span-ID regimes is incoherent).
- **D-04 — SpanID minting full-tree.** Root mints an 8-byte `crypto/rand` SpanID (D-16 of Phase 2: NOT UUIDv7 — W3C SpanID is 8 bytes); children chain `ParentSpanID = parent.SpanID`. Populate the previously-zeroed `InvocationContext.SpanID`/`ParentSpanID`. `RequestID` (UUIDv7 TraceID) already minted at root in Phase 2.
- **D-05 — Real `TracerProvider`, default OTLP/gRPC → `localhost:4317`, silent-drop without collector.** No collector in dev = OTLP exporter fails in background (standard retry, errors only at debug log), never crashes `aura chat` nor pollutes stdout. (Rejected auto-fallback-to-stdout = pollutes REPL; rejected fail-fast = makes tracing a hard dev dependency.)
- **D-06 — Exporter env-gated + override.** `AURA_OTEL_EXPORTER` ∈ {`stdout`,`otlp`,`none`} (default `otlp`); `none` = no-op provider (zero overhead for users who don't want tracing). `AURA_OTEL_ENDPOINT` overrides the OTLP target. Tests use an in-memory recorder regardless of env (SPEC Req#13 acceptance).
- **D-07 — go.mod OTel set, OTLP/gRPC default.** Add `go.opentelemetry.io/otel` + `otel/sdk` + `otel/exporters/stdout/stdouttrace` + `otel/exporters/otlp/otlptrace/otlptracegrpc`. `github.com/google/uuid` already present (Phase 2). **Researcher pins exact compatible stable versions before the commit** (no `replace`, no breaking pre-release).

### `current_time` delivery (researched)
- **D-08 — Tool-only in Phase 3; ambient tail-injection deferred to Phase 6.** `current_time` is a non-deferred builtin (RFC-3339 UTC default + optional IANA tz) the model calls on demand; the system prompt makes Aura aware it exists (D-09). **Always-on tail-injection of the ambient date (Claude Code's `<system-reminder>`-on-last-user-message pattern) is deferred to Phase 6** (Slice 4 KV builder), where stable-prefix-vs-tail construction is the explicit domain and SPEC puts "stable-prefix construction" out-of-scope for Phase 3. *Research consensus (web + Codex + Claude Code + Honcho/Hermes anti-pattern issues): never put a live clock in the cached prefix; the tool path is cache-safe and sufficient now; tail-injection is a prompt-builder concern.*

### System prompt (researched)
- **D-09 — Minimal, tool-aware, EN, mechanism-not-list, byte-stable.** First real Aura system prompt = one-line identity (Aura, a domain-neutral agentic substrate) + an explanation of the tool MECHANISM (you have tools in your tool-list; discover more via `tool_search`; terminate by calling `text_response`) **without enumerating tool names** (enumerating = cache-bust when tools change) + an explicit `Always respond in Italian` directive (per `feedback_all_prompts_in_english_only`: prompt in EN, output IT via directive). Concrete tool schemas ride in `req.Tools` rendered from the `Registry`, OUTSIDE the prefix → prefix stays byte-stable even as tools change. No timestamp (KV discipline). Lives as a constant/small function in `internal/agent` (e.g. `prompt.go`), planner-overridable. *Research: Codex uses a one-line identity + concise sections + time-via-tool; nanobot composes from snippets — that composable prefix-builder is Phase 6, not here.*

### `aura chat` REPL UX
- **D-10 — Two-stage Ctrl+C.** First Ctrl+C aborts the in-flight turn (uses SPEC Req#3 ctx-cancel: HTTP aborts ~100ms) and returns to the REPL prompt; second consecutive Ctrl+C, or EOF, or `/exit`, quits cleanly. goleak-clean.
- **D-11 — One-line per-turn cost footer.** After each reply: `· {total} tok ({in} in / {out} out) · ${usd} · {latency}s`. USD from OpenRouter's actual reported cost, price-table fallback (A3). Compact, does not invade the streamed text.
- **D-12 — Dim activity feedback during tool use.** On a tool_call, print a dim one-liner (e.g. `· tool_search("…")`) so the user sees activity; on the tool_result, print nothing (or a dim ✓). Then stream the final `text_response` text. (Rejected silence = user can't tell if Aura is stuck; rejected verbose args+result = invades streaming.)

### Loop output semantics (researched — Design A reinforced)
- **D-13 — `text_response` is the sole terminal output channel; stream its `text` argument; content-stop as robustness fallback.** Keeps the Phase-2 design (`text_response.go`: "canonical terminal … the string becomes the assistant message of record … terminates the loop"; precedent: smolagents `final_answer`). The REPL **incrementally extracts the `text` value from the streaming tool-call JSON arguments** and renders it as plain prose (~30 LOC JSON-string extractor → the user never sees raw JSON), satisfying Req#11 token-by-token. Any `content` the model emits BEFORE the tool call stays hidden as "thinking." **Robustness fallback:** if a turn ends `finish_reason=stop` with non-empty `content` and NO `text_response` call, accept that `content` as the answer (don't lose model output) — same error-back-to-model philosophy as D-15. *No PRD-amendment (preserves Phase-2 semantics) — this was the deciding factor over Design B (natural-content-streaming), which would have required re-doc'ing `text_response.go` + a two-primary-branch terminator and broken tool-first uniformity needed for ask_user/swarm.* *Research: tool-call argument deltas ARE wire-streamable (it's what Req#2 accumulates); the "don't show JSON" caveat is neutralized by extracting the `text` value.*
- **D-14 — Multiple tool_calls per assistant message supported, dispatched sequentially.** The model may batch ≥2 tool_calls in one assistant message; Phase 3 executes them sequentially (each `ConsumeStep` + dedup check), appends all `RoleTool` results in `tool_call_id` order, then makes one next LLM call. OpenAI-correct shape: 1 assistant msg with `tool_calls[]` → N `tool` msgs. Concurrent tool execution deferred to Phase 9 (ParallelAgent).
- **D-15 — Tool errors → error tool-result back to the model, never terminal.** Malformed JSON args or unknown tool name → construct a `RoleTool` message carrying the error (`"parse error: …"` / `"unknown tool X"`) so the model self-corrects; the infinite-retry case is already bounded by step-budget + dedup (Phase 2). The `iter.Seq2` error slot is reserved for REAL infra failures (LLM wire dead) only — consistent with Phase 2 D-04. SPEC's "unknown `read_tool_output` id hard-fails" = the tool's `Execute` returns an error → becomes a tool-result the model sees (not a process kill).
- **D-16 — `tool_choice="auto"` + content-stop fallback.** Model decides; pairs with D-13's content-stop fallback. (Rejected `"required"` = risks needless tool-calls/loops on trivial turns and inconsistent provider handling; researcher may revisit if DeepSeek-via-OpenRouter mis-steers.)

### SSE client wire
- **D-17 — Defensive parser.** Skip SSE comment lines (leading `:`, e.g. `: OPENROUTER PROCESSING` keep-alives); recognize `data: [DONE]` sentinel (do not JSON-parse it); use `bufio.Reader.ReadString('\n')` **NOT `bufio.Scanner`** (Scanner's 64KB token cap would truncate a large tool-call arguments delta); accumulate deltas split across read boundaries.
- **D-18 — `stream_options.include_usage=true` → final usage chunk for tokens; USD = OpenRouter actual cost first, price-table fallback.** **Researcher must verify** where OpenRouter exposes the real cost: in the streaming usage chunk (OpenRouter `usage:{include:true}`) vs a follow-up `GET /generation/{id}`. Capture `prompt_tokens`/`completion_tokens`/`cache_hit_tokens` from the usage chunk.
- **D-19 — Timeout wiring (footgun-aware).** Total 120s via `context.WithTimeout` on the request ctx (NOT `http.Client.Timeout`, which counts body-read time and would abort a long healthy stream at 120s); connect 10s via `Transport.DialContext`/dialer; no idle/first-byte timeout (SPEC). Both overridable via `AURA_LLM_*`.
- **D-20 — OpenRouter attribution headers + minimal privacy-first routing.** `HTTP-Referer` = Aura repo/project URL, `X-Title` = `Aura` on every request. Send `provider.data_collection="deny"` (privacy — relevant to the SMB bundle); `allow_fallbacks` default. **Advanced routing** (prompt-caching-aware provider selection for Phase 6, `order`, `sort` price/throughput, `require_parameters`) is **flagged for researcher** (the `:exacto` suffix is already a routing variant) and overridable via `~/.aura/llm.json`.
- **D-21 — `finish_reason="length"` → partial + truncation notice, no auto-continue.** Emit the partial text + a clear `[risposta troncata: max_tokens]` notice, end the turn; the partial enters history so a follow-up "continua" works naturally. No auto-continue (cost/loop risk, deferrable). Golden fixture for `length` alongside `stop`/`tool_calls`.

### Config & generation params
- **D-22 — `LLMConfig` shape + defaults.** `{Provider, Model, BaseURL, APIKey, TotalTimeoutSec, ConnectTimeoutSec, Temperature, MaxTokens, Headers, Prices}`. Defaults: provider `openrouter`, model `deepseek/deepseek-v4-flash:exacto`, base `https://openrouter.ai/api/v1`, Temperature `0.7`, MaxTokens `4096` (makes `finish_reason=length` realistically testable), timeouts 120s/10s. Load order per SPEC Req#5 (default < `.env`(`OPENROUTER_API_KEY`→APIKey) < `~/.aura/llm.json` < `AURA_LLM_*`). Lives in `internal/llm` (e.g. `config.go`), composed into `config.Config.LLM`. Empty APIKey = clear non-panic error.
- **D-23 — A3 price-table.** Seed map `model → {input_per_1M, output_per_1M}` USD in the `llm` package, seeded with `deepseek/deepseek-v4-flash:exacto`; overridable via a `prices` field in `~/.aura/llm.json`. Unknown model → report tokens with USD `n/a` (NOT `$0` — don't lie). OpenRouter's actual cost is always preferred; the table is the safety net.
- **D-24 — `aura config show/get/set` (cobra).** `show` prints the effective config with APIKey REDACTED; `get <key>` reads; `set <key> <value>` writes `~/.aura/llm.json` (creates it if absent). Keys like `llm.model`, `llm.base_url`, `llm.temperature`. No interactive editor.

### ToolResult spillover & session wiring
- **D-25 — Shared spillover helper, ctx-injected ids (DRY).** A single helper (e.g. `tools.NewResult(ctx, content) (ToolResult, error)`) performs the cap/preview/sidecar logic uniformly, reading `session_id` + `tool_call_id` + `run_dir` injected into the ctx by the agent before each `Execute`. Tools never reimplement spillover (CLAUDE.md reusable-code rule). `≤ AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048, already in config) → preview only, no disk write; `>` cap → preview + footer pointer in history + full bytes to the sidecar. (Rejected per-tool spillover = duplicated logic in N tools; rejected agent-post-process = changes SPEC's `ToolResult{FullPath,Truncated}` producer semantics.)
- **D-26 — `session_id = Event.ThreadID` (UUIDv7), minted once per chat session.** Each turn mints a fresh `RequestID`; the session keeps one `ThreadID`. Sidecar dir = `$AURA_RUN_DIR/conversations/<ThreadID>/<tool_call_id>.result`, created lazily. Aligns Phase 4 where `ThreadID` becomes the durable `conversation_id` (same shape, no migration — SPEC Req#8).
- **D-27 — `read_tool_output` offset/limit = BYTES (= Amendment A4).** Resolves the SPEC Req#7 contradiction (`limit?=200 lines` vs acceptance `offset=50000`). Byte-based offset+limit (default limit ~2048, aligned to the preview cap) is robust on arbitrary/newline-free content (a 100 KB JSON blob makes line-based useless) and matches the byte-shaped acceptance example. Footer: `showing bytes X-Y of Z, next offset Y` → deterministic paging by the model.

### Security / redaction
- **D-28 — Structural redaction + anti-leak test.** APIKey never enters any serialized/logged struct; the `Authorization` header is set only at request-build time; `HTTPError` captures status + retry-after + body (OpenRouter's body won't contain the key); span attrs explicitly exclude the key. Plus a test asserting the value of `OPENROUTER_API_KEY`/`AURA_LLM_*` does NOT appear in any `Event`/span/error/log output (SPEC Constraint "keys/DSNs never in logs/errors/Events/spans").

### Degradation policy
- **D-29 — Degrade clean, never crash, history stays coherent.** Ctrl+C mid-stream discards the partial in-flight assistant message (in-memory history never holds half a reply → next turn starts clean). `context_length_exceeded` from the provider → clear user error, NO trimming in Phase 3 (microcompact = Phase 4). Sidecar write-failure (disk full / permissions) → return the preview + a `full output unavailable` note, the turn continues. None of these is hard-terminal.

### Observability (slog)
- **D-30 — Thin slog, secondary to OTel, request_id-correlated, no secrets.** The `llm.request` span is primary observability; slog is thin: DEBUG for wire lifecycle (model, request_id, NO key/auth header), WARN/ERROR for `HTTPError`/cancel. `request_id` attr on every record for trace correlation. Cost/usage stays in the REPL footer (D-11), not duplicated to INFO. Per `golang-observability` + D-28.

### Test strategy
- **D-31 — Deterministic CI tier + manual real smoke (honest no-skip-as-green).** CI tier (always runs, deterministic, covers the 85% floor without network): golden SSE fixtures + `httptest.Server` for parser/accumulator/cancel/429/length; fake `llm.Client` for `LlmAgent` (ordered Events, budget-trip terminal Events, redaction); `goleak.VerifyTestMain` per package + `-race`. **Real-OpenRouter end-to-end smoke** (SPEC acceptance "aura chat streams a real reply") = `scripts/llm_smoke.sh`, gated on `OPENROUTER_API_KEY`, run by dev/local, **NOT in CI** (paid + non-deterministic). This respects the no-skip-as-green rule: nothing in CI pretends to run a live LLM; the deterministic tier genuinely exercises the parsing + loop paths.
- **D-32 — Golden fixtures in `internal/llm/openai_compat/testdata/*.sse`.** Raw SSE byte files (`text_stop.sse`, `toolcall_multichunk.sse`, `error_429.sse`, `premature_close.sse`, `length_truncation.sse`); the relevant ones include a keep-alive comment + `[DONE]` + a usage chunk. Tests stream them via `httptest.Server` (or a custom `RoundTripper`). `testdata/` = Go convention (build-ignored).

### Claude's Discretion (defaulted, planner-overridable)
- Exact `Temperature`/`MaxTokens` defaults (0.7 / 4096) — overridable via `AURA_LLM_*`; lower temp (~0.3) is defensible for tool-reliability if the planner prefers.
- Exact otel module versions — researcher pins (D-07).
- `read_tool_output` default byte limit (~2048) and the price-table seed numbers.
- `aura config` key naming (`llm.model` dotted vs nested) — cobra-idiomatic, planner picks.
- Whether the spillover helper lives in `internal/agent/tools` vs a shared `internal/agent` location — DRY intent is the constraint, not the path.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Locked requirements (read first)
- `.planning/phases/03-llm-client-toolresult/03-SPEC.md` — 14 locked requirements, boundaries, constraints, acceptance criteria. **MUST read** — apply A4 (offset bytes) + A5 (env catalog) from this CONTEXT where they extend it.

### PRD (source of truth)
- `prd.md` §Slice 1 (LLM client OpenAI-compat + ToolResult pattern) — goal, smoke, acceptance, file targets, env catalog, commit template. The three SPEC-declared deviations (A1 REPL, A2 real exporter, A3 cost actual+table) amend this section.
- `prd.md` §Naming convention — `AURA_<DOMAIN>_<UNIT>` env discipline (exception: `OPENROUTER_API_KEY` canonical third-party).
- `prd.md` §Slice Q&A discipline — 3 gates (DoR / Implementation / DoD).

### Prior phases (this repo)
- `.planning/phases/02-agent-cornerstone/02-CONTEXT.md` — Phase-2 decisions Phase 3 consumes: D-04 (budget exhaustion = Event-only, `ErrBudgetExhausted` sentinel), D-09/D-10/D-11 (Budget tree, `ConsumeStep` decrement-then-check), D-16 (8-byte crypto/rand SpanID), D-17 (`MessageID`/`ToolCallID`/`ThreadID` for AG-UI forward-compat), D-07 (`agenttest` mocks reused for LlmAgent tests), D-22 (`iter.Seq2` discipline).
- `.planning/phases/02-agent-cornerstone/02-SPEC.md` — Event/Actions/LLMResponse shape.
- `.planning/phases/01-infra-db-knowledge/01-CONTEXT.md` — D-07 config composition (`config.Config` thin root; per-subsystem config in owning package — Phase 3 adds `LLM llm.Config`), boot fail-fast discipline.
- `.planning/ROADMAP.md` Phase 3 entry — goal + Success Criteria.
- `.planning/REQUIREMENTS.md` CORE-01 — slice-mapped acceptance.
- `.planning/PROJECT.md` — substrate identity, OpenRouter/DeepSeek decision, Go 1.26 constraint.

### Existing code (drop-in / integration points)
- `internal/llm/client.go` — `Client` interface + wire types (`Message`/`ToolCall`/`ToolDef`/`Chunk`/`Request`) to IMPLEMENT (the `openai_compat` package fills `Stream`). `Request` has `Temperature`/`MaxTokens`; `Message` has `ToolCalls`/`ToolCallID`/`Name`.
- `internal/agent/agent.go` — `Agent` interface (LlmAgent implements) + `InvocationContext` (note `SpanID [8]byte`/`ParentSpanID *[8]byte` are DEFERRED-zeroed at L51-52 — D-04 mints them now).
- `internal/agent/event.go` — `Event`/`LLMResponse`/`Actions`; `ThreadID` (= session_id, D-26), `RequestID`, `MessageID`, surfaced `ToolCallID`.
- `internal/agent/budget.go` + `internal/agent/budget_dedup.go` — `ConsumeStep`, dedup ring; `LlmAgent.Run` checks these before each LLM call (SPEC Req#10).
- `internal/agent/errors.go` — `ErrBudgetExhausted` sentinel; Phase 3 adds the LLM `HTTPError` (SPEC Req#4).
- `internal/agent/tools/spec.go` — `Tool` interface (migrate `Execute → (ToolResult, error)`) + `Registry` (renders `req.Tools`).
- `internal/agent/tools/text_response.go` — canonical terminal tool (D-13 keeps its semantics; stream its `text` arg).
- `internal/agent/tools/search.go` — `tool_search`; adapt to ToolResult signature.
- `internal/agent/agenttest/` — reuse mock agents for LlmAgent tests (Phase 2 D-07).
- `internal/config/config.go` — root composite; add `LLM` field + `AURA_OTEL_*` wiring; `ToolPreviewCap`/`RunDir` already present.
- `cmd/aura/main.go` — subcommand dispatcher; add `chat` + `config` cases.

### Codebase maps
- `.planning/codebase/STACK.md`, `STRUCTURE.md`, `CONVENTIONS.md`, `TESTING.md`, `INTEGRATIONS.md` — target layout, naming, test discipline, OpenRouter integration notes.

### Project discipline
- `CLAUDE.md` §Behavioral rules (≤600 LOC, refactor-on-touch, reusable code, 3-strike), §Tool design (deferred-tool pattern; `read_tool_output`/`current_time` are non-deferred), §Post-edit validation (Gate 2), §Coverage floor 85%, §No-skip-as-green, §Env vars.

### Memory priors that constrain decisions
- `feedback_agent_must_know_tools_exist` — canonical tool-aware system prompt (drives D-09).
- `feedback_all_prompts_in_english_only` — prompt EN + `Always respond in Italian` directive (D-09).
- `feedback_one_module_per_slice` — atomic sub-commits (D-01).
- `reference_openrouter_provider_capabilities_2026-05-27` — DeepSeek-V4 Flash 80% cache, OpenAI-wire shape (informs D-18/D-20).
- `reference_aura_cache_poisoning_sites_2026-05-27` — KV-cache poisoning sites; reinforces D-08/D-09 prefix byte-stability.
- `feedback_no_regex_for_nlp` — D-13's text extraction parses the streaming tool-call JSON structurally, not regex over prose.
- `feedback_aura_as_product` — privacy-first routing (D-20 `data_collection:deny`), quality gates.
- `feedback_check_tmp_sources_then_brainstorm_best` + `feedback_codex_more_precise_than_claude` — drove the 3 research forks instead of easiest-default.

### External validation (research, 2026-05-30)
- Large tool-output preview+pointer+artifact: arXiv 2511.22729 (validated in SPEC).
- Prompt-caching prefix-stability for agents: arXiv 2601.06007 "Don't Break the Cache"; Honcho #13631 / Hermes time-in-prompt anti-pattern; Claude Code ~92% cache-hit via reminder-on-next-user-message (drives D-08/D-09).
- Final-answer-tool loop precedent: smolagents `FinalAnswerTool` (`huggingface/smolagents`) — validates D-13.
- Streaming function-call arguments: OpenAI function-calling guide + `openai/openai-agents-python#834` (arg deltas are wire-streamable) — validates D-13/D-17.
- OTel/W3C: W3C trace-context (TraceID 16B / SpanID 8B); OTel exporters (stdouttrace, otlptracegrpc) — D-04/D-07.
- OpenRouter provider routing + usage accounting docs — researcher confirms D-18/D-20 fields.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/llm/client.go` (78 LOC) — wire types + `Client` interface are drop-in; Phase 3 implements `Stream` in a new `internal/llm/openai_compat` package. `Request.Temperature`/`MaxTokens` already exist (D-22 defaults populate them).
- `internal/agent/agenttest/` — mock agents reused for `LlmAgent` tests (no mock duplication).
- `internal/agent/{budget,budget_dedup,event,errors}.go` — consumed as-is by `LlmAgent` (budget enforcement, dedup, Event emission, sentinel).
- `internal/agent/tools/{spec,text_response,search,manifest}.go` — Registry + Tool pattern; the ToolResult migration touches `spec.go` + `text_response.go` + `search.go` in one coupled commit (SPEC Constraint).
- `internal/config/config.go` — `ToolPreviewCap` (2048) and `RunDir` already wired (D-25 sidecar uses `RunDir`); add `LLM` + `AURA_OTEL_*`.

### Established Patterns
- Module `github.com/chetto1983/aura`; Go 1.26.x; `iter.Seq2` GA; `github.com/google/uuid` present (Phase 2); `go.uber.org/goleak` present.
- Phase-1/2 test discipline: `goleak.VerifyTestMain`, build-tag integration tiers, `-race`, owned-surface coverage ≥85%.
- Deferred-tool pattern: `read_tool_output` and `current_time` are **non-deferred** (small, always-visible); the spec metadata constant lives in each tool file.
- Boot fail-fast on missing required config (Phase 1 D-07) — extended to empty APIKey (SPEC Req#5).

### Integration Points
- `cmd/aura/main.go` dispatcher — add `chat` (REPL, SPEC Req#11) + `config` (D-24) subcommands.
- `internal/llm/openai_compat/` (new) — SSE client implementing `llm.Client`.
- `internal/agent/LlmAgent` (new) — first real `Agent` impl; threads ToolResult into history, enforces budget, emits Events.
- `internal/agent/prompt.go` (new) — minimal system prompt constant (D-09).
- shared spillover helper (D-25) — `tools.NewResult` (or `internal/agent` location) consumed by all tools.
- `~/.aura/llm.json` — config file (D-22 load order, D-23 prices, D-24 `aura config`).
- `$AURA_RUN_DIR/conversations/<ThreadID>/` — sidecar layout (D-26).
- `.env.example` + PRD env catalog — A5 additions.

</code_context>

<specifics>
## Specific Ideas

- **Streaming the `text_response` answer (D-13):** ~30 LOC incremental JSON-string extractor — scan the accumulating tool-call `arguments` for the `"text":"` key, then emit characters until the closing unescaped quote (handle `\"`/`\\`/`\n` escapes). The user sees prose, never JSON.
- **Real smoke (`scripts/llm_smoke.sh`, D-31):** `aura chat` two-turn scripted stdin against real OpenRouter, gated on `OPENROUTER_API_KEY`; asserts a streamed reply + a non-zero token+USD footer; run locally, documented as the manual acceptance gate for SPEC Req#11. NOT a CI job.
- **Golden fixtures (D-32):** include a `: OPENROUTER PROCESSING` keep-alive line + `data: [DONE]` + a trailing usage chunk in `text_stop.sse` so the parser's comment-skip, sentinel, and usage paths are all exercised deterministically.
- **Attribution comment** on the `openai_compat` client: cite the handrolled-no-SDK constraint + OpenRouter wire shape.

</specifics>

<deferred>
## Deferred Ideas

- **Always-on ambient-date tail-injection** (Claude Code `<system-reminder>`-on-last-user-message pattern) → Phase 6 (Slice 4 KV builder), where stable-prefix-vs-tail construction lives. The `current_time` tool covers Phase 3 (D-08).
- **Caching-aware OpenRouter provider routing** (require providers supporting prompt caching, `order`/`sort` preferences) → Phase 6 KV work; Phase 3 sends only `data_collection:deny` + attribution (D-20).
- **Auto-continue on `finish_reason="length"`** (multi-request continuation) → future; Phase 3 surfaces the partial + truncation notice (D-21).
- **Concurrent tool execution** within a turn (parallel tool_calls) → Phase 9 (ParallelAgent); Phase 3 dispatches sequentially (D-14).
- **Conversation persistence / microcompact / history trimming** → Phase 4 (Slice 1.8/1.8b); `context_length_exceeded` is surfaced as a clean error now (D-29).
- **Composable snippet-based prompt builder** (nanobot-style) → Phase 6 prefix builder; Phase 3 uses a minimal constant (D-09).
- **`tool_choice="required"` enforcement** of tool-first output → revisit if `auto` + content-stop fallback proves insufficient with DeepSeek-via-OpenRouter (D-16).

</deferred>

---

*Phase: 3-llm-client-toolresult*
*Context gathered: 2026-05-30*
