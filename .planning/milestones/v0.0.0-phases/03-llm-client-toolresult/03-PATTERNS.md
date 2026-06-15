# Phase 03: LLM Client + ToolResult - Pattern Map

**Mapped:** 2026-05-30
**Files analyzed:** 17 (12 new, 5 edited) + 4 test/fixture targets
**Analogs found:** 16 / 17 (1 partial — the SSE wire parser is the deliberate handrolled exception with only a structural analog)

> **Scope note for the planner.** Phase 3 ships with a 14-req SPEC (ambiguity 0.08), 32 locked decisions (D-01..D-32), and a 70KB AI-SPEC carrying copy-paste skeletons. This PATTERNS.md does NOT re-derive the design — it maps each target file to the **real current shape** of its closest in-repo analog, so the planner replicates established Aura conventions (error-wrap, goleak TestMain, env-load, secret-redaction, deferred-tool registry, Event/iter.Seq2 emission) rather than the AI-SPEC pseudocode. **Two AI-SPEC/reality deltas the RESEARCH flagged are load-bearing and reproduced below** (Run signature, zeroed SpanID).

---

## CRITICAL: AI-SPEC pseudocode vs real interface shape

The AI-SPEC §4 writes `func (a *LlmAgent) Run(ctx context.Context) iter.Seq2[...]`. **This is wrong.** The real interface (`internal/agent/agent.go:33`) is:

```go
Run(InvocationContext) iter.Seq2[*Event, error]
```

- The cancellable `context.Context` rides inside `InvocationContext.Ctx` (named field, line 48 — NEVER embedded). `LlmAgent.Run` must read `ic.Ctx`, wrap it `context.WithTimeout(ic.Ctx, totalTimeout)`, and thread that into `http.NewRequestWithContext`.
- `InvocationContext.SpanID` is `[8]byte{}` and `ParentSpanID` is `nil` TODAY (`agent.go:51-52`), documented as "DEFERRED to the OTel slice (WR-04)". **D-03/D-04 mint them now**: root mints an 8-byte `crypto/rand` SpanID, children chain `ParentSpanID = parent.SpanID`. `event.go:24-31` already documents this exact deferral and the wire is hex-string ready (`event.go:39-40,108-109,168-174`) — minting is the only missing piece, no Event-shape change.
- `RequestID` (UUIDv7 TraceID) is already minted at root (Phase 2); reuse `uuid.NewV7()` exactly as `cmd/aura/agent.go:135-148` does.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/llm/openai_compat/client.go` (NEW) | service (wire client) | streaming / request-response | `internal/knowledge/ping.go` (HTTP+ctx) + `internal/knowledge/client.go` (stream reader) | role-match |
| `internal/llm/openai_compat/sse.go` (NEW) | service (parser) | streaming | `internal/knowledge/client.go` `bufio.Reader` line loop | partial (handrolled, SPEC-locked) |
| `internal/llm/openai_compat/accumulate.go` (NEW) | utility | transform | — (no analog; new wire-delta merge) | none |
| `internal/llm/openai_compat/httperror.go` (NEW) | model (error type) | — | `internal/agent/errors.go` (sentinel) | role-match |
| `internal/llm/config.go` (NEW) | config | — | `internal/config/config.go` env-load + `internal/agent/budget.go` load-order | exact |
| `internal/llm/prices.go` (NEW) | config (static table) | — | `internal/agent/budget.go` const-default block | role-match |
| `internal/agent/llm_agent.go` (NEW) | agent (Run loop) | event-driven / streaming | `internal/agent/agenttest/mocks.go` `CountingAgent.Run` + `cmd/aura/agent.go` `dryRun` | exact |
| `internal/agent/prompt.go` (NEW) | utility (const) | — | `internal/agent/tools/text_response.go` const-string style | role-match |
| `internal/agent/tracing.go` (NEW) | provider (OTel bootstrap) | — | `internal/knowledge/ping.go` resource self-test + AI-SPEC §3 | partial |
| `internal/agent/tools/result.go` (NEW) | utility (spillover helper) | file-I/O | `internal/config/config.go` `defaultRunDir` + AI-SPEC §3 Pattern 3 | role-match |
| `internal/agent/tools/read_tool_output.go` (NEW) | tool | file-I/O | `internal/agent/tools/search.go` (`ToolSearch`) | exact |
| `internal/agent/tools/current_time.go` (NEW) | tool | transform | `internal/agent/tools/text_response.go` (`TextResponse`) | exact |
| `internal/agent/tools/spec.go` (EDIT) | model (interface) | — | self — `Tool.Execute` signature migration | self |
| `internal/agent/tools/text_response.go` (EDIT) | tool | — | self — adapt to `(ToolResult, error)` | self |
| `internal/agent/tools/search.go` (EDIT) | tool | — | self — adapt to `(ToolResult, error)` | self |
| `internal/config/config.go` (EDIT) | config | — | self — add `LLM llm.Config` + `AURA_OTEL_*` | self |
| `cmd/aura/main.go` + new `cmd/aura/chat.go` + `cmd/aura/config.go` | controller (CLI) | request-response / event-driven | `cmd/aura/agent.go` (loop driver) + `cmd/aura/db.go` (subcommand dispatch) | exact |
| `scripts/llm_smoke.sh` (NEW) | test (manual smoke) | — | `internal/knowledge/smoke_test.go` (gated live test) | role-match |
| `*_test.go` + `main_test.go` + `testdata/*.sse` | test | — | `workflow_test.go` (goleak) + `client_unit_test.go` (httptest) | exact |

---

## Pattern Assignments

### `internal/llm/openai_compat/client.go` + `sse.go` (service, streaming)

**Analog:** `internal/knowledge/ping.go` (HTTP-client + ctx + goleak hygiene) and `internal/knowledge/client.go` (bufio.Reader stream loop).

**HTTP client + ctx + the goleak keep-alive footgun** (`ping.go:80-93`) — this is the single most important analog: it documents a goleak trap the SSE client MUST also handle.
```go
func pingEmbed(ctx context.Context, baseURL string, expectedDim int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/embeddings",
		bytes.NewReader([]byte(`{"input":"ping","model":"embedding"}`)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	// Drop the pooled keep-alive connection on return; otherwise the default
	// transport's persistConn read/write goroutines linger until IdleConnTimeout
	// and make goleak.VerifyTestMain order-dependent (passes in the full suite,
	// trips on short subsets).
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
```
**Planner: copy the `defer client.CloseIdleConnections()` discipline** (or `Transport{DisableKeepAlives:true}` as `smoke_test.go:38-41` does) into the SSE client — Req#3 goleak-clean depends on it. NOTE the deviation per D-19: the SSE client uses `context.WithTimeout` on the request ctx and a `net.Dialer{Timeout}` on `Transport.DialContext`, NOT `http.Client{Timeout}` (which would abort a healthy long stream — Pitfall #2). The AI-SPEC §3 entry-point skeleton (lines 199-248) is the authoritative request-build + parse loop.

**bufio.Reader stream loop** (`internal/knowledge/client.go:39` uses `stdout *bufio.Reader`; the JSON-RPC read loop reads line-delimited). The SSE parser uses `bufio.NewReader(resp.Body).ReadString('\n')` — NOT `bufio.Scanner` (Pitfall #1, SPEC-locked D-17). No existing SSE parser exists; the AI-SPEC §3 loop is the reference. `:`-comment skip + `[DONE]` sentinel + `data:` JSON-unmarshal are documented in AI-SPEC §3 "Common Pitfalls" 1-7.

**Error wrapping** — every analog wraps with `%w` and a context phrase (`ping.go:95` `"embed sidecar unreachable: %w"`, `client.go:119` `"initialize error %d: %s"`). Match this for `httperror.go`.

---

### `internal/llm/openai_compat/httperror.go` (model, error type)

**Analog:** `internal/agent/errors.go` (sentinel pattern) + `internal/knowledge` error literals.

`errors.go` is a 1-symbol file (`ErrBudgetExhausted = errors.New(...)`). `HTTPError` is a struct (carries StatusCode/RetryAfterSec/Body, Req#4), so it implements `error` via an `Error()` method. **Convention to follow:** the error string must NOT contain the API key — `HTTPError.Body` is the *provider's* response body (safe, never contains the key — AI-SPEC Pitfall #3, D-28). Mirror the knowledge package's load-bearing-literal discipline (`crashHint` in `client.go:28`) if any test asserts the error text byte-for-byte.

---

### `internal/llm/config.go` (config) + `prices.go` (static table)

**Analog (load-order):** `internal/agent/budget.go:99-205` — the canonical Aura env load-order + fail-fast pattern.

`budget.go` shows the exact precedence machinery Phase 3 Req#5 needs (default < env < explicit override), with fail-fast on malformed-but-set values:
```go
// envIntFailFast reads key as an int, returning fallback when unset/empty and a
// verbatim errMalformed when set-but-unparseable (D-06).
func envIntFailFast(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, errMalformed(key, v)
	}
	return n, nil
}
```
**Difference for D-22:** the LLM load order inserts a FILE tier (`~/.aura/llm.json`) between `.env` and `AURA_LLM_*`. `internal/config/config.go:39-84` shows the simpler silent-absorb tier (`envDefault`/`envIntDefault`, lines 103-124) used for non-critical knobs — use fail-fast (budget style) for the LLM config, since an empty APIKey is a clear error (Req#5), and silent-absorb (config style) for the `AURA_OTEL_*` knobs. The `defaultRunDir()` helper (`config.go:128-133`, uses `os.UserCacheDir`) is the template for resolving `~/.aura/llm.json`'s home path.

**Const-default block** (`budget.go:38-45`) is the style for the `prices.go` seed map and the D-22 defaults (model/baseURL/timeouts).

**Secret structural redaction (D-28):** the closest analog is `internal/knowledge/client.go:33,224-229`:
```go
var pwAssignRE = regexp.MustCompile(`(?i)(password|pass)(\s*[=:]\s*)\S+`)

func (c *Client) redactSecrets(s string) string {
	if c.password != "" {
		s = strings.ReplaceAll(s, c.password, "***")
	}
	return pwAssignRE.ReplaceAllString(s, "$1$2***")
}
```
**For Phase 3 the preferred discipline is STRUCTURAL, not regex** (D-28): the `APIKey` is set only at request-build time, never enters any logged/serialized struct or span attr. Use the knowledge anti-leak TEST shape (`client_unit_test.go:84-100`) as the template for the mandatory D-28 anti-leak test (assert the key value is absent from every Event/span/error/log).

---

### `internal/agent/llm_agent.go` (agent, event-driven streaming) — THE core file

**Analog:** `internal/agent/agenttest/mocks.go` `CountingAgent.Run` (lines 188-210) — the canonical budget-gated `iter.Seq2` emitter — plus `cmd/aura/agent.go` `dryRun` (the loop driver).

**Agent.Run skeleton + budget gate + terminal-Event-not-error** (`mocks.go:188-210`):
```go
func (a *CountingAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	author := a.Name()
	return func(yield func(*agent.Event, error) bool) {
		for {
			ok, reason := ic.Budget.ConsumeStep()
			if !ok {
				yield(&agent.Event{
					Author: author,
					Branch: ic.Branch,
					Actions: agent.Actions{StateDelta: map[string]any{
						"termination_reason": "budget_exhausted",
						"limit_hit":          reason,
					}},
				}, nil) // terminal event — the shared budget refused this step
				return
			}
			a.Calls++
			if !yield(&agent.Event{Author: author, Branch: ic.Branch}, nil) {
				return
			}
		}
	}
}
```
**This is the exact shape `LlmAgent.Run` replicates** (Req#9/#10/D-15):
- `ic.Budget.ConsumeStep()` returns `(ok bool, reason string)` with reason ∈ `{"max_steps","wallclock"}` (`budget.go:213-223`) — note the AI-SPEC writes `"max_wallclock"` but the real string is `"wallclock"`; the planner must use the real constant. Dedup is in `budget_dedup.go` (not shown here but consumed via the same `*Budget`).
- Budget exhaustion → `yield(terminalEvent, nil)` then `return` — NEVER the error slot (D-04). The error slot is reserved for REAL infra failure (LLM wire dead, D-15).
- The yield-after-false guard (`if !yield(...) { return }`) is mandatory (D-22 footgun).

**Event emission shape** (`event.go:37-66`): `Event{Author, Branch, RequestID, SpanID, ParentSpanID, ThreadID, LLMResponse, Actions, Timestamp}`. `LLMResponse{Content, ToolCalls, FinishReason}` (lines 52-56) carries model output; `llm.ToolCall` is reused directly (D-17). **Timestamp must be non-zero UTC** (Req#14) — `MarshalJSON` already forces UTC RFC3339Nano (`event.go:116`); the agent must SET `ev.Timestamp = time.Now().UTC()` on emit (Phase 2 left it zero).

**InfiniteToolCallAgent** (`mocks.go:54-71`) shows how to emit a tool-call Event and the consumer-break exit path — the LlmAgent's tool-dispatch branch mirrors it. **Reuse these mocks as the fake-Client test fixtures (D-07)** — do NOT write new mock agents; for the fake `llm.Client`, write a new `fakeClient` returning a canned `<-chan llm.Chunk` (the `Client` interface is at `client.go:66-68`).

**Loop driver / Event consumption** (`cmd/aura/agent.go:118-129`) — how `aura chat` ranges over `Run`:
```go
for ev, runErr := range root.Run(ic) {
	if runErr != nil {
		return fmt.Errorf("dry-run: %w", runErr)
	}
	if ev == nil {
		continue
	}
	ev.RequestID = requestID // stamp the shared run id on every emitted Event
	...
}
```

**Registry.Render() → req.Tools** — the agent builds `req.Tools` from `tools.Registry.Render()` (`manifest.go:22-39`, alphabetical-sorted for cache stability). NOTE: `Render()` returns `[]ManifestEntry`, NOT `[]llm.ToolDef`. The planner must add a small mapping `ManifestEntry → llm.ToolDef` (`client.go:46-53`) OR a new `Registry` render method returning `[]llm.ToolDef` directly. Keep the alphabetical sort (`manifest.go:37`) — it is cache-stability-load-bearing (`feedback_aura_cache_poisoning_sites`).

---

### `internal/agent/tracing.go` (provider, OTel bootstrap)

**Analog:** AI-SPEC §3 "OTel TracerProvider bootstrap" (lines 256-274) is authoritative — no in-repo OTel exists yet (`go.mod` has none; this phase adds the v1.44.0 set per RESEARCH). The closest in-repo *posture* analog is `internal/knowledge/ping.go`'s "degrade-clean, never crash boot" self-test discipline. **D-05:** OTLP silent-drop without a collector. **D-06:** `AURA_OTEL_EXPORTER ∈ {stdout,otlp,none}` (default `otlp`), `none` = no-op provider. Span attrs per Req#13/AI-SPEC §4 (lines 392-399): `llm.model`, `llm.provider`, `llm.prompt_tokens`, `llm.completion_tokens`, `llm.cache_hit_tokens`, `aura.request_id` — NEVER an `api_key` attr (D-28).

---

### `internal/agent/tools/result.go` (utility, file-I/O spillover helper)

**Analog:** AI-SPEC §3 Pattern 3 (lines 193-205, includes the UTF-8 rune-boundary `truncatePreview`) + `internal/config/config.go:128-133` (`defaultRunDir`/`filepath.Join` path building).

The sidecar path is `$AURA_RUN_DIR/conversations/<ThreadID>/<tool_call_id>.result` (D-26, Req#8). `RunDir` and `ToolPreviewCap` (default 2048) are ALREADY in `config.Config` (`config.go:81-82`) — the helper reads them. The helper signature is `tools.NewResult(ctx, content) (ToolResult, error)` with `session_id`+`tool_call_id`+`run_dir` injected into ctx by the agent before each `Execute` (D-25). Path building via `filepath.Join` + lazy `os.MkdirAll`. Sidecar write-failure → return preview + `[full output unavailable: ...]` note, turn continues (D-29).

---

### `internal/agent/tools/read_tool_output.go` + `current_time.go` (tools)

**Analog:** `internal/agent/tools/search.go` (`ToolSearch`) and `text_response.go` (`TextResponse`) — the canonical non-deferred-tool shape.

**Tool struct + Spec() + Execute()** (`search.go:18-67`, `text_response.go:12-44`). Every tool is: a struct, a `Spec()` returning `{Name, Summary, Description, Parameters (raw JSON-schema string), Deferred: false}`, and an `Execute(ctx, raw json.RawMessage)`. **Both new tools are `Deferred: false`** (small builtins — CLAUDE.md §Tool design, D-08). Arg-parse pattern (`text_response.go:35-43`):
```go
func (TextResponse) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var a textResponseArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("text_response args: %w", err)
	}
	if a.Text == "" {
		return "", fmt.Errorf("text_response: text is required")
	}
	return a.Text, nil
}
```
**CRITICAL EDIT (the coupled D-01 commit 4):** `Tool.Execute` signature migrates `(string, error) → (ToolResult, error)` in `spec.go:30-33`. This breaks `TextResponse.Execute` (`text_response.go:35`) and `ToolSearch.Execute` (`search.go:46`) — all three change in ONE coupled commit (SPEC Constraint, line 117). `read_tool_output` offset/limit are BYTES not lines (Amendment A4/D-27 — overrides SPEC Req#7 text). Unknown `tool_call_id` → `Execute` returns an error → becomes a `RoleTool` error message the model sees (D-15), not a panic.

`current_time`: RFC-3339 UTC default + optional IANA tz via `time.LoadLocation` (Req#14). Live clock NEVER in `messages[0]` (D-08).

---

### `cmd/aura/chat.go` + `config.go` (controller, CLI)

**Analog:** `cmd/aura/agent.go` (the Run-loop driver) + `cmd/aura/db.go:19-44` (subcommand dispatch).

**Subcommand dispatch + config.Load + exit-non-zero** (`db.go:19-44`):
```go
func runDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}")
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}
	ctx := context.Background()
	switch args[0] {
	case "migrate":
		dbMigrate(ctx, cfg)
	...
```
**`config` subcommand** (D-24 `show/get/set`) mirrors this `runDB` dispatch exactly. **`chat` subcommand** drives `LlmAgent.Run` like `dryRun` (`agent.go:89-131`): mint session ThreadID via `uuid.NewV7()` (pattern `agent.go:135-148`), build `InvocationContext`, range over `Run`, render Events. Wire into `main.go:29-45` switch (currently `chat`/`shell`/`serve` print TODO at line 41). `buildRegistry()` (`main.go:52-57`) is extended to register `read_tool_output` + `current_time`. Two-stage Ctrl+C via `signal.NotifyContext` (D-10). Cost footer + dim tool-activity are stdout rendering (D-11/D-12) — no in-repo analog, AI-SPEC §4b incremental text extractor (lines 478-505) is the reference.

---

### Tests + fixtures (test)

**Analog:** `internal/agent/workflow/workflow_test.go:17-19` (goleak TestMain) + `internal/knowledge/client_unit_test.go:29-80` (httptest server) + `internal/knowledge/smoke_test.go` (gated live smoke).

**goleak TestMain** (`workflow_test.go:17-19`) — copy verbatim into `internal/llm/openai_compat/main_test.go` and `internal/agent/main_test.go` (if not inherited):
```go
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```
**httptest replay** (`client_unit_test.go:29-48`) — the template for streaming `testdata/*.sse` fixtures through `httptest.NewServer` (Req#1-#4). Set `w.Header().Set("Content-Type", "text/event-stream")` and write raw fixture bytes; `defer srv.Close()`.

**Compile-time interface assertion** (`workflow_test.go:24-27`, `mocks.go:27-32`): `var _ agent.Agent = (*LlmAgent)(nil)` and `var _ llm.Client = (*openai_compat.Client)(nil)` — add both.

**Property tests** (`pgregory.net/rapid` is ALREADY in `go.mod:13`) for UTF-8 truncation (Req#6) and tool-call accumulation (Req#2) per RESEARCH Validation Architecture.

**Manual smoke** (`smoke_test.go:38-45` shows the `envOrSkipCI` gate pattern) — `scripts/llm_smoke.sh` is gated on `OPENROUTER_API_KEY`, NOT in CI (D-31).

---

## Shared Patterns

### Error wrapping with `%w` + context phrase
**Source:** `internal/knowledge/ping.go:95`, `cmd/aura/agent.go:120,127`, `text_response.go:38`
**Apply to:** every new file
```go
return fmt.Errorf("embed sidecar unreachable: %w", err)
```
Lowercase phrase + colon + `%w`. Load-bearing literals (asserted byte-for-byte in tests) follow the `crashHint`/`errMalformed` convention (`client.go:28`, `budget.go:72-74`).

### goleak-clean HTTP teardown
**Source:** `internal/knowledge/ping.go:88-92` (`defer client.CloseIdleConnections()`) + `smoke_test.go:40` (`Transport{DisableKeepAlives:true}`)
**Apply to:** `openai_compat/client.go` (Req#3 depends on it)
The default transport's persistConn goroutines linger and make `goleak.VerifyTestMain` order-dependent unless explicitly closed.

### Secret redaction + anti-leak test
**Source:** `internal/knowledge/client.go:224-229` (redact) + `client_unit_test.go:84-100` (anti-leak test)
**Apply to:** `openai_compat` (APIKey) — D-28. Phase 3 prefers STRUCTURAL redaction (key set only at request-build, never in a struct) over regex; the TEST shape is the reused asset (release-blocking gate).

### Env load-order, fail-fast vs silent-absorb
**Source:** fail-fast `internal/agent/budget.go:182-205`; silent-absorb `internal/config/config.go:103-124`
**Apply to:** `internal/llm/config.go` (fail-fast: empty APIKey is a clear error, Req#5) + `config.go` `AURA_OTEL_*` (silent-absorb knobs)

### Deferred-tool Registry shape
**Source:** `internal/agent/tools/{spec.go,manifest.go,search.go,text_response.go}`
**Apply to:** `read_tool_output.go`, `current_time.go` (both `Deferred:false`); `Registry.Render()` is alphabetical-sorted for cache stability (`manifest.go:37`) — preserve.

### Event emission (iter.Seq2, budget-gated, terminal-Event-not-error)
**Source:** `internal/agent/agenttest/mocks.go:188-210` (`CountingAgent.Run`)
**Apply to:** `llm_agent.go` — budget trip → `yield(terminalEvent, nil); return`; real infra fail → error slot; yield-after-false guard mandatory.

### CLI subcommand dispatch + config.Load + os.Exit
**Source:** `cmd/aura/db.go:19-44`, `cmd/aura/agent.go:46-66`
**Apply to:** `cmd/aura/chat.go`, `cmd/aura/config.go`; wire into `main.go:29-45` switch.

---

## No Analog Found

| File | Role | Data Flow | Reason / Authoritative reference instead |
|------|------|-----------|------------------------------------------|
| `internal/llm/openai_compat/accumulate.go` | utility | transform | No tool-call delta accumulator exists. Use AI-SPEC §3 Pattern 2 (lines 184-191) — private `toolCallDelta` wire struct, merge by `index`, do NOT add `Index` to public `llm.ToolCall` (RESEARCH Pattern 2). |
| `internal/llm/openai_compat/sse.go` (parser core) | service | streaming | The handrolled SSE parse is the deliberate SPEC-locked exception (Framework Decision §2). AI-SPEC §3 entry-point (lines 199-248) is the only reference; `bufio.Reader` line-loop style from `knowledge/client.go` is the structural analog. |
| `internal/agent/tracing.go` (OTel wiring) | provider | — | No OTel anywhere in repo yet. AI-SPEC §3 bootstrap (lines 256-274) authoritative; v1.44.0 module set pinned by RESEARCH. |
| `aura chat` cost footer + incremental text extractor | controller | streaming | No streaming-prose-render analog. AI-SPEC §4b (lines 478-505) is the reference for the `textExtractor.feed` JSON-string scanner (structural, not regex — `feedback_no_regex_for_nlp`). |

---

## Metadata

**Analog search scope:** `internal/llm/`, `internal/agent/` (+ `tools/`, `agenttest/`, `workflow/`), `internal/config/`, `internal/knowledge/`, `cmd/aura/`
**Files scanned (read in full or targeted):** client.go, agent.go, spec.go, text_response.go, search.go, manifest.go, event.go, budget.go, errors.go, config.go, main.go, agent.go (cmd), db.go (cmd), mocks.go, ping.go, client.go (knowledge), workflow_test.go, client_unit_test.go, go.mod
**Pattern extraction date:** 2026-05-30
**Key cross-cutting finding:** Aura already has every convention Phase 3 needs (error-wrap, goleak teardown, env load-order, secret redaction, deferred-tool registry, budget-gated iter.Seq2 emission, CLI dispatch) — the genuinely new surface is the SSE wire parser (handrolled, SPEC-locked) and the OTel bootstrap (deps added this phase). Everything else is replication of existing patterns.
