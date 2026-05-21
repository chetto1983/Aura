# Phase-BUG — Critical Bug Fixes (concurrent with Phase-WIKI-B)

**Status:** 🔴 ship immediately
**Provenance:** web-telegram-consolidation scout #7 + codebase-cleanup-audit scout #6
**Estimated effort:** ~1 session
**LOC delta:** -30 + 2 real bugs fixed
**Dependencies:** none — concurrent with Phase-WIKI-B; not blocked by anything.

---

## Why now

Per `CLAUDE.md`: "BUGS ARE ALWAYS FIXED WHEN FOUND — NEVER DEFER." Three bugs were surfaced by the research scouts that compound IF deferred:

1. **`/api/chat` skips operator overlays** — web users see a slim base prompt; Telegram users see full agent personality. **All web bench data prior to this fix is partially invalid.**
2. **`internal/logging` imports `internal/api`** — broken leaf contract, 590 transitive deps. Build slows, test isolation harder, dep graph confused.
3. **errcheck noise hides 2 real bugs** — silent JSON-write failure on `/health` (3 sites), silent gzip-flush failure on backup export → corrupted backup on disk error.

Plus one low-effort dead-code delete (`appendUniqueSorted`).

---

## Stories

### US-BUG-01 — Fix `/api/chat` overlay loading

- **Scope:** Extract shared `composeAgentPrompt` function callable from both transports. Web path currently uses `conversation.RenderSystemPrompt(now, loc)` — base + runtime only. Telegram uses `ComposeAgentPrompt` which loads `AGENT.md`/`SOUL.md`/`USER.md`/`TOOLS.md`. Fix: both call the same function.
- **Files:** [internal/conversation/system_prompt.go](internal/conversation/system_prompt.go), [internal/channels/web/chat_service.go](internal/channels/web/chat_service.go) or [cmd/aura/web_chat.go](cmd/aura/web_chat.go), [internal/channels/telegram/invocation_builder.go](internal/channels/telegram/invocation_builder.go).
- **LOC delta:** +80 / -40 = +40 (mostly net additive — the new shared function holds the overlay loading logic now duplicated).
- **Acceptance:**
  - `go test ./internal/conversation/... ./internal/channels/... ./internal/api/...` green.
  - Probe: send identical query to `/api/chat` and via Telegram bot; system prompts must match byte-for-byte (modulo runtime block timestamp).
  - Probe: `/api/chat` reply on a wiki-personality-sensitive query (e.g. "come ti chiami?") returns the Italian-by-default behavior driven by SOUL.md, not English.
- **Per-module deep refactor:** golangci-lint clean + dupl -t 60 clean + LOC ≤600 on all touched files. If `web_chat.go` (currently 612 LOC) doesn't shrink below 600, split it as part of this commit.
- **Note:** This is also CONS-01 in the Phase-CONS plan — it lives in Phase-BUG because it's the live bug fix and should not wait for the rest of the consolidation.

### US-BUG-02 — Invert `internal/logging → internal/api` import

- **Scope:** `internal/logging/zap_slog.go:9` imports `internal/api` to satisfy a health-shape interface. Logging should be a leaf depending only on `zap`+`slog`+stdlib. Invert: `internal/api` provides its own logging adapter; `internal/logging` exports a small interface that `api` implements.
- **Files:** [internal/logging/zap_slog.go](internal/logging/zap_slog.go), [internal/api/health_server.go](internal/api/health_server.go) or wherever the health-shape consumer lives.
- **LOC delta:** ~0 net (move + tiny adapter).
- **Acceptance:**
  - `go list -f '{{.ImportPath}} {{len .Deps}}' ./internal/logging` reports <50 transitive deps (was 590).
  - `go build ./... && go test ./...` green.
  - No package outside `internal/logging` directly imports `internal/api` for logging purposes.

### US-BUG-03 — Fix errcheck-hidden bugs in health + backup

- **Scope:** Two real bugs hiding in the 50-item errcheck noise:
  - `internal/api/health_server.go` — silent JSON-write failure on `/health` (3 sites). Wrap with explicit error logging.
  - `internal/backup/export.go` — silent gzip-flush + tar-writer failure → corrupted backup on disk error. Wrap with error propagation.
- **Files:** [internal/api/health_server.go](internal/api/health_server.go), [internal/backup/export.go](internal/backup/export.go).
- **LOC delta:** +30.
- **Acceptance:**
  - Manual test: induce disk-full on backup target; verify backup returns error rather than producing a corrupted file.
  - `go test ./internal/api/... ./internal/backup/...` green including new error-path test for each site.

### US-BUG-04 — Delete `appendUniqueSorted` dead code

- **Scope:** `internal/wiki/memory_hygiene.go:735` — `appendUniqueSorted` is unused (golangci-lint U1000); supplanted by `sortedStringSet`. Delete + delete the local test if any.
- **Files:** [internal/wiki/memory_hygiene.go](internal/wiki/memory_hygiene.go).
- **LOC delta:** -10.
- **Acceptance:** `golangci-lint run ./internal/wiki/...` no `unused` finding.

---

## Sequencing

US-BUG-01 first (highest impact, fixes bench invalidation). US-BUG-02/03/04 can run in any order; bundle into 1-2 commits per CLAUDE.md "no batching except mechanical".

---

## Risks

- **R1**: US-BUG-01 changes the system prompt seen by /api/chat. Existing bench results from /api/chat are now invalid; re-bench AFTER US-BUG-01 ships, NOT before. Memory `feedback_aura_as_product` mandates `docs/aura-quality-snapshot.md` stays current; re-grade strict-pass numbers post-fix.
- **R2**: US-BUG-02 boundary inversion may surface latent bugs where api was relied upon transitively. Run full `go test ./...` not just the touched packages.
- **R3**: US-BUG-03 surfaces real failure paths. New error returns may trip callers that ignored them. Audit callers.

---

## Verification

- `go test ./...` green.
- `go vet ./...` clean.
- `golangci-lint run ./...` no new findings on touched files.
- `cmd/probe_chat` web-chat smoke case (if exists) passes; if no web case exists, add one as part of US-BUG-01.
- `~/go/bin/dupl -t 60 ./internal/...` no new cluster involving touched files.

---

*Updated 2026-05-21. Per CLAUDE.md DEEP REFACTOR ON TOUCH, every commit must include the touched-file cleanups inline.*
