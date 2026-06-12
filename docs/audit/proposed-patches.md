# Proposed Patches — Aura `internal/agent` (2026-06-12)

Patch-style recommendations for each major open finding from the 2026-06-12 re-audit. **None are applied** — this is design guidance. Snippets are illustrative pseudocode anchored to real symbols; adapt to exact signatures when implementing.

Each patch: affected file/function · reason · before/after behavior · suggested approach · tests required before merging · rollback considerations.

> The prior cycle's patches (PP-1..PP-18, the P0/P1 pass) landed in code and are recorded in the bug-report appendix. This file covers the open set.

---

## PP-A1 — Write-ahead tool-intent + recovery marker (B-01)

- **Affected:** `internal/agent/llm_agent.go` `dispatch`; `internal/runner/runner_persist.go`; `internal/conversations/store_helpers.go` `repairToolMessagePairsWith`.
- **Reason:** A mutating tool's host side effect commits before its result turn is persisted; crash-recovery drops the dangling call, so the model re-issues it → duplicated side effects.
- **Before:** `executeBatch` runs → on a later event the assistant tool_calls + RoleTool result persist. Crash between them → repair drops the group silently.
- **After:** the assistant tool_calls turn + a `pending` intent row persist in one tx *before* execution; the result + `done` flip persist after. Recovery surfaces an unmatched `pending` as a synthetic RoleTool "verify before re-running".
- **Approach:**
  ```go
  // before executeBatch, in one tx with the assistant tool_calls turn:
  for _, i := range runnable {
      persistIntent(tx, sessionID, calls[i].ID, calls[i].Function.Name, canon[i]) // status=pending
  }
  // after each result:
  markIntentDone(tx, calls[i].ID) // same tx as AppendTurn(result)
  // in repairManagedToolMessagePairs: an assistant tool_call whose intent is pending-with-no-result
  // becomes a synthetic RoleTool("[recovery] previous result unknown — verify before re-running"),
  // NOT a dropped group.
  ```
  Add an idempotency token to mutating tool specs where the op supports it (e.g. `fs_write` is naturally idempotent on identical content; `shell_exec` is not — mark it).
- **Tests:** integration — persist tool_call turn, kill before result commit, reload, assert a recovery marker (not a drop, not a re-execution); unit on `repair` for the pending-no-result case.
- **Rollback:** the intent table is additive; drop the recovery branch to revert to drop-on-dangling. Migration is forward-only — gate behind a feature flag for the first release.

## PP-A2 — Wrap swarm reports in the untrusted envelope (B-02)

- **Affected:** `internal/swarm/runner_adapter.go` (the `tools.NewResult` site, ~:54).
- **Reason:** swarm child summaries re-enter the parent prompt outside the `<tool_output trust="untrusted">` envelope.
- **Before:** `res := tools.NewResult(ctx, out)` — no provenance → parent reads it as trusted.
- **After:** `res.Provenance = &tools.ToolResultProvenance{Source:"swarm", Trust:tools.TrustUntrusted}` → `renderToolResultForPrompt` wraps + NFKC/HTML-escapes it.
- **Approach:** one-line provenance set; or add `"swarm_spawn"` to `untrustedToolNames` in `trust.go`. Prefer the provenance set (it's the structural direction — see PP-A18).
- **Tests:** a swarm result containing `</tool_output>` bytes returns HTML-escaped inside an untrusted envelope in the parent history.
- **Rollback:** trivial revert; no schema/state change.

## PP-A3 — Per-thread in-flight guard (B-03)

- **Affected:** `internal/runner/runner.go` `Turn`; `internal/agui/server.go` `handleRun`.
- **Reason:** concurrent runs on one thread interleave history appends → corruption + budget double-spend.
- **Before:** `Turn` loads + mutates history with no serialization.
- **After:** a per-`threadID` lock (or singleflight) serializes (or rejects with 409) concurrent runs.
- **Approach:**
  ```go
  // Runner:
  locks sync.Map // threadID -> *sync.Mutex
  func (r *Runner) Turn(ctx, threadID, ...) {
      mu := r.lockFor(threadID); 
      if !mu.TryLock() { return ErrThreadBusy } // AG-UI maps to 409; channels may block instead
      defer mu.Unlock()
      ... // existing body
  }
  ```
  Decide reject-vs-serialize per channel: AG-UI → 409; Telegram → serialize (queue) so a fast double-tap doesn't drop a message.
- **Tests:** concurrent `handleRun` on one thread → 409 (or provable serialization); race-detector test on interleaved turns.
- **Rollback:** remove the lock; behavior reverts. No persistence change.

## PP-A4 — Honest self-extension contract + restored alert (B-04)

- **Affected:** `internal/agent/tools/skill.go` (schema `description` ~:99-112; doc comment ~:14-25); `internal/skills/writer.go` (the ungated `modelMutationBypassesGate` path ~:146-148).
- **Reason:** the schema/comments claim human approval that no longer happens for `always:false`; the operator alert no longer fires.
- **Before:** schema says "STAGED as pending … require explicit human approval"; reality auto-activates; no alert.
- **After:** schema states the true policy; an alert/audit row fires on the ungated auto-activate.
- **Approach:** edit the schema string + comments to: "create/update with `always:false` activate immediately in this container; `always:true` and delete require approval." In `writer.go`, on the ungated path call `w.alerter.Alert(...)` (or emit an audit row) before returning `StatusActive`.
- **Tests:** schema-snapshot test that the description matches live gating; assert an alert/audit row on ungated model auto-activate.
- **Rollback:** the schema edit is text; the alert is additive. To revert the policy itself (re-gate), flip `modelMutationBypassesGate` — but that is a product decision, out of audit scope.

---

## PP-A5 — `obs.Init(cfg)`: tracer in `serve` + JSON slog (O-01)

- **Affected:** new `internal/obs/init.go`; `cmd/aura/serve.go` `runServe`; `cmd/aura/chat_repl.go`; `cmd/aura/main.go`.
- **Reason:** the daemon never boots the tracer; the agent core has no structured logging.
- **Before:** tracer installed only in the REPL; default text `slog`, no correlation.
- **After:** both entry points call `obs.Init(ctx, cfg)` → installs the OTel provider (deferred bounded `Shutdown`), `slog.SetDefault(JSONHandler)` with base attrs + a redacting `ReplaceAttr`; the loop logs WARN/ERROR with `request_id`/`thread_id`.
- **Approach:**
  ```go
  func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
      slog.SetDefault(slog.New(redacting(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))).
          With("service","aura","version",buildVersion))
      tp, err := agent.NewTracerProvider(ctx, cfg.OtelExporter, cfg.OtelEndpoint)
      return tp.Shutdown, err
  }
  ```
  Thread a `*slog.Logger` (or `slog.With(...)`) into `LlmAgent` at the `reasoningtrace.Record` WARN/ERROR chokepoints.
- **Tests:** a turn under `serve` produces a span (stdout exporter capture); a capture handler asserts correlated JSON records.
- **Rollback:** `obs.Init` is additive; revert by not calling it (REPL keeps its own path).

## PP-A6 — Prometheus `/metrics` (O-02)

- **Affected:** `internal/agent/metrics.go` (replace expvars); `internal/agui/server.go` (mount); `llm_agent.go:366` (record tool duration).
- **Reason:** no latency/error/cost metrics.
- **Before:** four monotonic expvar counters at `/debug/vars`.
- **After:** Prometheus histograms/counters/gauges at `GET /metrics`.
- **Approach:** define a `Metrics` struct over `prometheus.Registry`; record turn/tool/LLM durations, errors, in-flight, SSE, cost; `mux.Handle("/metrics", promhttp.HandlerFor(reg, ...))`.
- **Tests:** registry assertion that a turn moves the histograms/error counters; `/metrics` returns Prometheus text.
- **Rollback:** keep expvars in parallel for one release; remove `/metrics` to revert.

## PP-A7 — Production container + hardened compose (D-01)

- **Affected:** new `Dockerfile`, `.dockerignore`; `compose.yaml`.
- **Reason:** the privileged agent runs uncontainerized; sidecars unhardened.
- **Approach:** multi-stage build (`golang:1.x` → `gcr.io/distroless/base-nossl`), non-root `USER 65532`; an `aura` compose service with `read_only: true`, `cap_drop:[ALL]`, `mem_limit`, `cpus`, `healthcheck: curl /healthz`; add `cap_drop`/`mem_limit` to sidecars. (Note: `shell_exec` may need a writable workspace mount + minimal caps — scope them explicitly rather than running full-privilege.)
- **Tests:** CI image build; runs-non-root assertion; healthcheck smoke.
- **Rollback:** the host-binary path is unaffected; the image is additive.

---

## PP-A8 — L1 microcompact: only pointer-rewrite sidecar-backed turns (M-01)

- **Affected:** `internal/conversations/context.go` `applyL1` (~:208-229).
- **Reason:** rewriting an `ask_user`/small result to a `read_tool_output` pointer destroys it (no sidecar to page back).
- **Before:** every `RoleTool` turn older than `evictAfter` becomes a pointer.
- **After:** only rewrite when `t.ContentSidecarPath != ""` or the `[output truncated:` footer is present; leave the rest inline (L2.5 pair-drop handles them).
- **Approach:** add the sidecar-presence guard to the rewrite condition.
- **Tests:** an `ask_user` answer older than `evictAfter` survives verbatim, not a pointer.
- **Rollback:** revert the guard.

## PP-A9 — One-transaction `SubmitAnswer` (M-02)

- **Affected:** `internal/runner/runner_resume.go`; `internal/askuser/store.go`; a new `Conv` store seam.
- **Reason:** inject + mark-resumed are two transactions → duplicate answer turn on retry.
- **After:** `InsertConversationTurn` + `markResumedSQL UPDATE … WHERE resumed_at IS NULL` in one `db.WithTx`; `RowsAffected==0` makes a re-submit a no-op.
- **Tests:** a resume retry → exactly one answer turn + `ErrPauseNotFound`.
- **Rollback:** revert to the two-call path.

## PP-A10 — `hardCap<=0` is a config error (M-03)

- **Affected:** `internal/conversations/context.go` (~:66, :153).
- **Reason:** clamping to 0 disables L2.5 silently on small-window models.
- **After:** `hardCap<=0` returns `ErrContextWindowExceeded` (or applies a per-model floor) instead of returning raw history.
- **Tests:** small-window over-cap history → error/compaction, never raw history.
- **Rollback:** revert the guard.

## PP-A11 — Process-lifetime breaker + graceful open (B-05, B-06)

- **Affected:** `internal/runner/runner.go` (own the breaker); `internal/agent/llm_agent.go` (accept it via config; route open to finalize).
- **After:** the breaker persists across turns; breaker-open → `finalize(reason="breaker_open")`, not the error slot.
- **Tests:** two turns vs a 503 client → second short-circuited; pre-open → non-empty terminal Event.
- **Rollback:** revert to per-turn construction.

## PP-A12 — Stream idle-timeout watchdog (B-08)

- **Affected:** `internal/llm/openai_compat` stream; `internal/agent/llm_agent.go` `consume`; config (`AURA_LLM_STREAM_IDLE_TIMEOUT_SEC`).
- **After:** if no chunk arrives within the idle window, close the stream and surface a retryable transport error (the existing mid-stream retry re-issues once).
- **Tests:** an open-then-stall fake client → turn aborts within the idle window.
- **Rollback:** disable via env (set to 0).

## PP-A13 — Unify `secretEnvKey` (B-09)

- **Affected:** new `internal/secret/envkey.go`; callers in `shell_exec_env.go`, `mcp/client.go`, `mcp/manager/config.go`.
- **After:** one `IsSecretEnvKey` (markers incl. `"key"`, `"private"`, `"cert"`) used everywhere.
- **Tests:** `PRIVATE_KEY` redacts identically in shell + MCP.
- **Rollback:** revert callers to the local copies.

---

## PP-A14 — Boot validation + log redaction (O-04); health dep probes (O-05)

- **Affected:** `internal/config/config.go` (`Validate()`); `cmd/aura/serve.go` (`HealthCheck`, `/readyz`).
- **After:** boot fail-fasts on empty required secrets; a redacting `ReplaceAttr` strips DSNs from logs; `/healthz` (liveness) + `/readyz` (PG+Neo4j+embed).
- **Tests:** empty `NEO4J_PASSWORD` → non-zero exit; DSN-bearing log sanitized; `/healthz` 503 with Neo4j down.
- **Rollback:** make `Validate()` warn-only; keep the single `/healthz`.

## PP-A15 — Split `shell_exec.go` (B-11); destructive-gate defaults+docs (B-10)

- **Affected:** `internal/agent/tools/shell_exec.go` → `shell_exec_args.go`; docs.
- **After:** no file >600 LOC; the destructive gate ships a conservative default pattern set and is documented as advisory, not a sandbox.
- **Tests:** file-size gate green; gate-on-by-default smoke.
- **Rollback:** the split is mechanical; the doc/defaults are additive.

## PP-A16 — Lifecycle sweeps + session eviction (M-06, R-41, M-04)

- **Affected:** `internal/conversations/orphan_scan.go` (periodic sweep); `internal/reasoningtrace` (rotation); session-scoped tools (`Evict`); `conversations/store.go` (spill-inside-tx).
- **After:** archived-conversation sidecars + dead session state are reclaimed; reasoningtrace rotates; spill no longer orphans on rollback.
- **Tests:** reasoningtrace rotates at cap; deleting N conversations frees their `todo`/`shell_bg` entries; a rolled-back oversized turn leaves no orphan spill.
- **Rollback:** sweeps are additive (can be disabled by interval=0).

---

## PP-A17 (P3 cluster) — small, low-risk fixes

- **`anyInt` json.Number (M-07):** add `case json.Number: n,err := f.Int64(); if err==nil { return int(n) }` — mirror `anyFloat`. Test: `anyInt(json.Number("100"))==100`. Rollback: trivial.
- **Typed stream-retry classification (B-13):** prefer `errors.Is(err, syscall.ECONNRESET)`/`io.ErrUnexpectedEOF` over substring text. Test: typed-path classification. Rollback: keep the text fallback.
- **`Registry.Register` fail-loud (B-14):** return an error on duplicate name. Test: double `Register` → error. Rollback: revert to overwrite.
- **Cap+frame MCP arg descriptions (B-15):** apply `frameMCPDescription`+cap to bridged `Parameters`. Rollback: revert.
- **`fs_grep`/`fs_glob` node budget (B-16):** count nodes, abort at cap/deadline. Rollback: revert.
- **Buffer stream chunks until clean completion (B-12):** emit on success, drop on retry. Test: render path shows no garbled duplicate. Rollback: revert.
- **Exclude `agenttest` from the coverage floor (T-04):** add the path to `coverage_gate.sh:44`. Rollback: revert.

---

## Cross-cutting rollback note

All Phase 0/1 patches are additive or behind a flag; none alter the verified-correct loop core. The one forward-only migration is the tool-intent table (PP-A1) — gate it behind a feature flag for the first release so a rollback degrades to the current drop-on-dangling behavior rather than failing on an unknown table.
