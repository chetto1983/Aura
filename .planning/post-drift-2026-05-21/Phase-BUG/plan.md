# Phase-BUG — Critical bug fixes

**Status:** 🔴 **NEXT MILESTONE** (user-locked 2026-05-22 — ships immediately after Phase-TOOL completes, before Phase-MODERNIZE)
**Provenance:** web-telegram-consolidation scout (2026-05-21) + codebase-cleanup-audit scout (2026-05-21) + live verification 2026-05-22
**Estimated effort:** ~1 session, 3 atomic stories
**LOC delta:** +50 / -10 = +40 + 3 real bugs fixed
**Dependencies:** none — runs immediately after Phase-TOOL Ralph queue completes; Phase-MODERNIZE shifts to slot #4 in the locked sequence

---

## Why now

User decision 2026-05-22: "pianifica Phase-BUG come prossima milestone". Three independent bugs surfaced by the 2026-05-21 research scouts are still present in master (verified by grep 2026-05-22):

| Bug | Verified at | Status |
| --- | --- | --- |
| US-BUG-01: `/api/chat` overlay loading | `cmd/aura/web_chat.go:109` uses `RenderSystemPrompt` (base + runtime only, no overlay) | 🔴 STILL BROKEN |
| US-BUG-02: `logging → internal/api` import | `internal/logging/zap_slog.go:9` imports `internal/api` (broken leaf) | 🔴 STILL BROKEN |
| US-BUG-03: health JSON + backup gzip silent failures | `internal/api/health_server.go:167,172,185` + `internal/backup/export.go:185,228,256,284,313` | 🔴 STILL BROKEN |
| ~~US-BUG-04~~: `appendUniqueSorted` dead code | `grep -rn appendUniqueSorted internal/` returns 0 results | ✅ ALREADY CLEANED — story dropped |

Per CLAUDE.md "BUGS ARE ALWAYS FIXED WHEN FOUND — NEVER DEFER". US-BUG-01 explicitly invalidates web-channel bench data (Telegram users see SOUL.md/AGENT.md personality; web users see a slim base prompt — non-equivalent system prompts mean web bench numbers measure a different system). US-BUG-02 inflates `internal/logging` transitive deps from <20 to 590, polluting build times and test isolation across the whole codebase. US-BUG-03 hides silent corruption on disk-full (backup) and silent client-facing 200 responses with no body (health probe).

Phase-MODERNIZE was planned to come next after Phase-TOOL; user re-prioritized Phase-BUG ahead because these are visible-impact bugs while MODERNIZE is structural hygiene that benefits from cleanup-on-touch already enforced by Phase-MODERNIZE INFRA gates. Shipping BUG first reduces risk that the MODERNIZE god-splits introduce a regression hidden by these existing silent failures.

---

## Stories

### US-BUG-01 — Fix `/api/chat` overlay loading

- **Scope:** Web channel currently uses `conversation.RenderSystemPrompt(now, loc)` — base prompt + runtime block ONLY, no operator overlays. Telegram uses `agent.ComposeAgentPrompt(...)` which loads `AGENT.md` / `SOUL.md` / `USER.md` / `TOOLS.md`. Web users see a slim base prompt; Telegram users see the full agent personality. Fix: extract the overlay-loading logic into a shared function that both transports call. Specific extraction shape — read `internal/channels/telegram/invocation_builder.go::buildPromptPlan()` (currently at ~line 200-300) and pull the overlay-loading branch into `internal/conversation/promptplan.go::ComposePromptForChannel(channel, now, loc, store)` that returns the assembled system prompt + the prompt-modules manifest. Both transports then call this single function.
- **Files:**
  - MODIFY [internal/conversation/system_prompt.go](internal/conversation/system_prompt.go) (or new `internal/conversation/promptplan.go`) — add `ComposePromptForChannel` shared function.
  - MODIFY [cmd/aura/web_chat.go](cmd/aura/web_chat.go) — replace `conversation.RenderSystemPrompt(time.Now(), time.Local)` at line 109 with the shared call.
  - MODIFY [internal/channels/telegram/invocation_builder.go](internal/channels/telegram/invocation_builder.go) — call the same shared function instead of inlining overlay loading.
  - VERIFY [internal/agent/promptplan.go](internal/agent/promptplan.go) — if `ComposeAgentPrompt` already exists there, the refactor moves the overlay-loading layer ABOVE it; `ComposeAgentPrompt` keeps its current contract.
- **LOC delta:** +100 (new shared func) / -60 (deduplicated overlay loading from telegram) = **+40**.
- **Acceptance:**
  - `go test ./internal/conversation/... ./internal/channels/... ./internal/api/...` green.
  - **Byte-equality probe**: send the same query via `/api/chat` and via Telegram fixture; system prompts must match byte-for-byte modulo the runtime timestamp block. Test added to `cmd/probe_chat/` or `internal/channels/*/fixture/`.
  - **Personality probe**: `curl -d '{"message":"come ti chiami?"}' /api/chat` returns Italian-by-default response driven by SOUL.md, not English.
  - **Web bench re-grade**: existing `docs/quality-bench/runs/*.json` for web channel are marked OBSOLETE in commit body; new bench scheduled post-merge.
- **Per-module deep refactor (CLAUDE.md)**: `cmd/aura/web_chat.go` is currently 612 LOC (over the 600 cap). The overlay fix replaces a 1-line call with a 1-line call — net LOC delta ~0 in that file, but does NOT bring it under 600. **Mark `web_chat.go` for Phase-MODERNIZE god-split (or fold into this commit IF the split is clean).**
- **Single atomic commit:** `fix(web): wire operator overlays into /api/chat prompt (US-BUG-01)`

### US-BUG-02 — Invert `internal/logging → internal/api` import

- **Scope:** `internal/logging/zap_slog.go:9` imports `internal/api` to satisfy a health-shape interface. Logging is supposed to be a LEAF — depends only on `zap` + `slog` + stdlib. Today's wrong-direction import inflates `internal/logging`'s transitive deps from <20 to 590 (verified 2026-05-21 audit). Every package that imports `internal/logging` (~30+ packages including agent core) drags `internal/api` along, slowing builds and breaking test isolation. Fix: invert the dependency. `internal/api` provides its own logging adapter; `internal/logging` exports a small interface (e.g. `HealthLogger`) that `api` implements at registration time.
  - Read `internal/logging/zap_slog.go` end-to-end (282 LOC total in the logging package per audit).
  - Identify the exact use case — what does logging need from `internal/api`? Likely a health-status struct OR a writer that posts JSON to a debug endpoint.
  - If it's a struct shape: define a minimal interface in `internal/logging` (`type HealthSnapshot interface { Status() string; ... }`) and have `internal/api` implement it.
  - If it's a writer: define a `LogSink interface { Write([]byte) error }` in `internal/logging` and have `internal/api` provide an adapter.
  - Also update `internal/logging/logging_test.go` which imports `internal/api` for the same reason — fixture should use a local mock implementing the new interface.
- **Files:**
  - MODIFY [internal/logging/zap_slog.go](internal/logging/zap_slog.go) — remove `internal/api` import; define minimal interface.
  - MODIFY [internal/logging/logging_test.go](internal/logging/logging_test.go) — use local mock instead of `internal/api`.
  - MODIFY [internal/api/](internal/api/) wherever the adapter lives — implement the new interface; register with logging at boot.
  - VERIFY [cmd/aura/app.go](cmd/aura/app.go) — adapter wiring lands in the composition root, not inside `internal/logging`.
- **LOC delta:** ~0 net (move + tiny adapter, lines roughly equal in + out).
- **Acceptance:**
  - `go list -f '{{.ImportPath}} {{len .Deps}}' ./internal/logging` reports <50 transitive deps (was 590 per 2026-05-21 audit).
  - `go build ./... && go test ./...` green.
  - `grep -rn '"github.com/aura/aura/internal/api"' internal/logging/` returns 0 results.
  - No NEW import of `internal/api` from any package outside `cmd/aura/` and `internal/api/` itself (the composition root + the package itself).
- **Single atomic commit:** `fix(logging): invert internal/api dependency — restore leaf contract (US-BUG-02)`

### US-BUG-03 — Fix errcheck-hidden silent failures (health + backup)

- **Scope:** Two real bugs hiding inside the 50-item errcheck noise from the 2026-05-21 audit. Both involve unchecked `Write`/`Encode`/`Close` calls that silently corrupt user-visible output.
  - **(a) `internal/api/health_server.go` — silent JSON-write failure on `/health`.** Three sites at lines 167, 172, 185 call `json.NewEncoder(w).Encode(...)` and discard the error. If the HTTP write fails mid-response (e.g. client disconnect), the server logs nothing AND sends a half-truncated 200 response that monitoring tools parse as "OK". Wrap with explicit logging via `deps.Logger.Warn("health: response write failed", "error", err)`.
  - **(b) `internal/backup/export.go` — silent gzip-flush + tar-writer failure.** Five+ sites (lines 185, 228, 256, 284, 313 per grep 2026-05-22) call `gzip.NewWriter(w)` and `tar.NewWriter(gz)` and don't propagate close-time errors. On disk-full mid-backup, the file truncates without error, leaving an "OK" status code AND a corrupted tarball on disk. Audit every close path; ensure `defer gz.Close()` + `defer tw.Close()` propagate via named return or wrapped panic-recovery.
- **Files:**
  - MODIFY [internal/api/health_server.go](internal/api/health_server.go) — 3 unchecked Encode sites → explicit error logging.
  - MODIFY [internal/backup/export.go](internal/backup/export.go) — 5+ unchecked gzip/tar close paths → propagate.
  - Tests covering the error paths added to `internal/api/health_server_test.go` and `internal/backup/export_test.go`.
- **LOC delta:** +40 / -0 = +40 (wrappers + tests).
- **Acceptance:**
  - **Health bug**: simulated client-disconnect mid-write → `Logger.Warn("health: response write failed", ...)` fires; observable in log fixture test.
  - **Backup bug**: simulated disk-full (or write-to-/dev/full on Linux) → `BackupExport(...)` returns non-nil error; existing partial tarball deleted or marked invalid.
  - `go test ./internal/api/... ./internal/backup/... -count=1` green.
  - `~/go/bin/golangci-lint run --no-config --default=none -E errcheck ./internal/api/health_server.go ./internal/backup/export.go` reports 0 unchecked errors on these two files (other files keep their noise — surface, not deep clean).
- **Single atomic commit:** `fix(api,backup): propagate silent JSON-write + gzip-close failures (US-BUG-03)`

---

## Sequencing

US-BUG-01 first — highest user-visible impact; unblocks valid web-channel benchmarks.
US-BUG-02 second — boundary inversion clears the deps inflation that affects every subsequent module-cleanup pass in Phase-MODERNIZE.
US-BUG-03 third — silent corruption bugs; lower visibility but high severity for production reliability.

**One story = one commit per `feedback_one_module_per_slice`.** Total: 3 atomic commits in ~1 session.

---

## Risks

- **R1 (US-BUG-01)**: changing the system prompt seen by `/api/chat` invalidates all existing web bench data. Mitigation: mark `docs/quality-bench/runs/*.json` for web channel as OBSOLETE in the commit body; schedule a re-bench post-merge (auto-trigger via the existing bench harness). Memory `feedback_aura_as_product` mandates `docs/aura-quality-snapshot.md` stays current.
- **R2 (US-BUG-02)**: boundary inversion may surface latent assumptions where `internal/api` was relied upon transitively (e.g. test fixtures using api types via logging). Mitigation: run full `go test ./...` not just touched packages; any failure that surfaces is an additional opportunity to clean.
- **R3 (US-BUG-03)**: making backup return errors on close-time failures may trip callers that currently ignore them. Mitigation: audit callers of `BackupExport`/`HealthHandler`; verify they handle the new error returns. Same for health writer — failure logging should not change the HTTP response shape (already 503 on health failure, just NOW we know why).
- **R4 (overlay loading)**: shared function `ComposePromptForChannel` becomes a god-class candidate if extended ad-hoc later. Mitigation: keep it ≤300 LOC, single responsibility (compose overlay-applied prompt), no channel-specific branching inside.

---

## Verification — phase exit criteria

After all 3 stories ship:

1. `go test ./... -count=1` green.
2. `go vet ./...` clean.
3. `golangci-lint run --no-config --default=none -E errcheck ./internal/api/health_server.go ./internal/backup/export.go` reports 0 unchecked errors on these two files.
4. `go list -f '{{.ImportPath}} {{len .Deps}}' ./internal/logging` returns count <50.
5. **Byte-parity probe**: `/api/chat` and Telegram fixture produce identical system prompts on the same query (modulo timestamp block).
6. **Re-bench scheduled**: `docs/quality-bench/runs/post-bug-2026-XX-XX.md` planned; old web-channel bench rows marked OBSOLETE.

---

*Updated 2026-05-22 — re-prioritized as next milestone after Phase-TOOL; US-BUG-04 dropped (already cleaned).*
