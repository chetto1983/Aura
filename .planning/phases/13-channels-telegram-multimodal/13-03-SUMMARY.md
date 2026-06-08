---
phase: 13-channels-telegram-multimodal
plan: 03
subsystem: api
tags: [telegram, markdownv2, escaper, fuzzing, png, x-image, gofont, opentype, model-capability, vision, multimodal]

# Dependency graph
requires:
  - phase: 13-channels-telegram-multimodal (plan 13-01)
    provides: golang.org/x/image v0.41.0 + telebot.v4 pins + telegram package goleak main_test.go + internal/channels/deps.go anchor
provides:
  - "llm.SupportsVision / llm.SupportsAudio — model-capability lookup (net-new internal/llm/models.go; Model is a bare string, no struct existed)"
  - "telegram.EscapeMarkdownV2 — entity-aware MarkdownV2 escaper (escape outside entities only) + PlainTextFallback contract helper"
  - "telegram.ParseMarkdownTable / RenderTablePNG / PreBlockTable — markdown table -> deterministic gridded PNG (x/image + embedded gofonts) + pre-block monospace fallback"
affects: [13-05 renderer, 13-06 telegram-core, 13-08 photo client, 13-09 telegram integration tier]

# Tech tracking
tech-stack:
  added: []  # x/image was promoted DIRECT in 13-01; this plan is its first genuine consumer
  patterns:
    - "Capability-by-lookup: routing decided by a package-level model->{vision,audio} table (no Model struct), conservative unknown=false"
    - "Entity-aware escaper: single-pass fence-state machine (outside/pre/inline), full reserved set outside, backtick+backslash inside, deterministic fence-close on unterminated input"
    - "Pure-transform PNG render via embedded gofonts (opentype + gomono/gomonobold), byte-deterministic; structural golden (dims + grid-line + non-blank-pixel) over byte-golden"

key-files:
  created:
    - internal/llm/models.go
    - internal/llm/models_test.go
    - internal/channels/telegram/mdv2.go
    - internal/channels/telegram/mdv2_test.go
    - internal/channels/telegram/tables.go
    - internal/channels/telegram/tables_test.go
    - internal/channels/telegram/testdata/table_four_col_dims.txt
    - internal/channels/telegram/testdata/table_six_col_dims.txt
  modified:
    - internal/channels/deps.go

key-decisions:
  - "models.go is a package-level lookup, not a method on Config (Config.Model stays a bare string, no struct to extend); OpenRouter :-suffix stripped before lookup"
  - "Escaper closes unterminated fences deterministically so output never 400s even on malformed model input; in-fence pipes/dashes/dots flow through unescaped"
  - "Structural golden (dims 439x163 / 658x217 + grid-line + non-blank-pixel) chosen over byte-golden for robustness; render is still asserted byte-deterministic across two calls"
  - "Removed the x/image anchor from internal/channels/deps.go (tables.go now imports opentype+gomono+gomonobold genuinely; go mod tidy + go build ./... stay green, x/image stays DIRECT v0.41.0); telebot/qrterminal anchors retained until their consumers land"

patterns-established:
  - "Pattern: model-capability lookup with conservative unknown-default false (anti-spoofing T-13-03-UnknownModelVision)"
  - "Pattern: entity-aware MarkdownV2 escaper proven by a >=10K-Unicode FuzzMdv2 corpus + a strict parser-model oracle (no would-400)"
  - "Pattern: deterministic markdown-table PNG via x/image embedded gofonts, structural golden stored in testdata"

requirements-completed: [UX-02, UX-04]

# Metrics
duration: ~10min
completed: 2026-06-08
---

# Phase 13 Plan 03: Telegram Leaf Transforms Summary

**Net-new model-capability lookup (SupportsVision/SupportsAudio), an entity-aware MarkdownV2 escaper proven fuzz-clean over a 10K-Unicode corpus, and a deterministic markdown-table→PNG renderer using x/image + embedded Go fonts — the three pure-transform leaves the Telegram renderer (13-05) and photo client (13-08) consume.**

## Performance

- **Duration:** ~10 min of implementation work (additional wall-clock spent disentangling parallel-session index contamination — see Issues)
- **Started:** 2026-06-08T08:16:19Z
- **Completed:** 2026-06-08T08:26:00Z
- **Tasks:** 3 (all `tdd="true"`)
- **Files modified:** 9 (8 created, 1 modified)

## Accomplishments

- `internal/llm/models.go`: net-new `SupportsVision`/`SupportsAudio` lookup over a `model→{vision,audio}` table — `minimax/minimax-m3` (incl. `:`-suffixed routing variants) = vision true, `deepseek/deepseek-v4-flash` + unknown = false (conservative). This is the `AURA_VISION_CLOUD=true` routing precondition the photo client (13-08) reads.
- `internal/channels/telegram/mdv2.go`: entity-aware MarkdownV2 escaper with a single-pass fence-state machine — escapes the full reserved set outside entities, only backtick+backslash inside fences, never whole-string, closes unterminated fences deterministically. `FuzzMdv2` + a >=10K-Unicode/prose seed corpus and a strict parser-model oracle prove zero `400 can't parse entities`; the 20s `-fuzz` campaign ran clean (77K execs, 0 crashes). `PlainTextFallback` documents the renderer's 400-fallback contract.
- `internal/channels/telegram/tables.go`: the spike-018b pipeline — `ParseMarkdownTable` → rectangular grid, `RenderTablePNG` → byte-deterministic gridded PNG via embedded `gomonobold`/`gomono` opentype faces (per-column `MeasureString` width + 16px pad + 1px black grid), `PreBlockTable` → ≤56-char monospace fallback. Golden test pins 4-col (439×163) / 6-col (658×217) dims + grid-line + non-blank-pixel assertions.

## Task Commits

Each task was committed atomically (TDD RED→GREEN folded per leaf — test + impl land together as the additive capability):

1. **Task 1: model capability table (SupportsVision/SupportsAudio)** — `e412c145` (feat)
2. **Task 2: entity-aware MarkdownV2 escaper + 10K-Unicode fuzz** — `75cfc891` (feat)
3. **Task 3: markdown-table→PNG renderer + pre-block fallback** — `8da3306d` (feat)

_TDD note: each leaf was written test-first (RED verified: undefined symbols / build-fail) then implemented to GREEN; the test+impl pair is committed together because the leaf is a pure additive data/transform with no separable behavior to stage._

## Files Created/Modified

- `internal/llm/models.go` — model→{vision,audio} capability table + SupportsVision/SupportsAudio lookups + OpenRouter `:`-suffix normalization
- `internal/llm/models_test.go` — table-driven truth table (suffix, unknown-default, stability)
- `internal/channels/telegram/mdv2.go` — EscapeMarkdownV2 fence-state machine + PlainTextFallback
- `internal/channels/telegram/mdv2_test.go` — unit cases + FuzzMdv2 + >=10K-Unicode seed corpus + parser-model oracle
- `internal/channels/telegram/tables.go` — ParseMarkdownTable / RenderTablePNG / PreBlockTable + errEmptyTable sentinel
- `internal/channels/telegram/tables_test.go` — parse cases + render determinism + structural golden + pre-block padding
- `internal/channels/telegram/testdata/table_four_col_dims.txt` — golden dims `439x163`
- `internal/channels/telegram/testdata/table_six_col_dims.txt` — golden dims `658x217`
- `internal/channels/deps.go` — removed the now-redundant x/image anchor (real consumer landed); telebot/qrterminal anchors retained

## Decisions Made

- **models.go = package-level lookup, not a method.** `Config.Model` stays a bare string (no Model struct exists, per PATTERNS "No Analog Found"). The OpenRouter routing suffix (`:exacto`/`:thinking`) is stripped before the table lookup; matching is on the full base id so a misleading suffix/prefix never promotes an unknown model.
- **Escaper closes unterminated fences deterministically.** Telegram treats an unclosed entity as a 400 the same as a naked reserved char, so the escaper emits the matching closer (`` ` `` / ` ``` `) when input leaves a fence open — guaranteeing the output is always a well-formed stream.
- **Structural golden over byte-golden for the PNG.** The render is asserted byte-deterministic (two renders compared `bytes.Equal`), but the persisted golden is dims + grid-line presence + non-blank-pixel count (stored in testdata) — robust against x/image patch-version metric drift while still catching layout regressions. `UPDATE_GOLDEN=1` refreshes after an intentional change.
- **deps.go x/image anchor removed.** tables.go now imports `opentype`+`gomono`+`gomonobold` genuinely; verified `go mod tidy` produces no go.mod diff and x/image stays in the DIRECT require block at v0.41.0. The telebot.v4 pin gate (literal grep) and qrterminal anchor remain intact (their consumers are still downstream).

## Deviations from Plan

None — plan executed exactly as written. (The deps.go anchor edit is the explicitly-sanctioned cleanup the prompt authorized once tables.go became x/image's real consumer; it is refactor-on-touch, not a deviation.)

## Issues Encountered

- **Parallel-session git index contamination (resolved).** A concurrent Codex session was committing Phase-15 spike files (`.planning/spikes/031-*`) to the same branch while this plan ran. My first Task-2 commit swept two of those staged spike files in; a soft-reset to surgically remove them collided with a stale `index.lock` left by the aborted reset. The contention resolved cleanly: the Codex session committed its own spike files as separate commits (`7b4058a0`, `0da17b71`), and I re-staged only my 2 mdv2 files explicitly and re-committed. One intermediate empty commit (no files in its tree) was created and immediately reset away. **Net effect on this plan: zero — all three 13-03 commits contain exactly their intended files** (verified via `git ls-tree`). This is the documented `gsd-tools commit --files sweeps parallel work` footgun; mitigation was explicit per-file `git add` + `git ls-tree` verification after every commit.
- **Windows git `-F` message-file path translation.** `git commit -F /tmp/...` resolved the path against the native Windows binary's `%TEMP%`, not `D:/tmp`; fixed by passing the absolute `D:/tmp/...` path. (Backticks in the table-renderer commit body also required moving the message to a file rather than an inline `-m` heredoc, to avoid bash fence-parsing.)

## Known Stubs

None. All three transforms are complete implementations with no placeholder values, empty returns, or TODO markers.

## Threat Flags

None beyond the plan's `<threat_model>`. The plan's mitigated threats are satisfied:
- **T-13-03-MdV2** — entity-aware escape + the >=10K fuzz oracle proves no would-400 output; `PlainTextFallback` documents the renderer's resend-without-ParseMode contract.
- **T-13-03-UnknownModelVision** — unknown model → `SupportsVision` false (conservative), tested.
- **T-13-03-PNGoom** (accept) — render is a pure bounded transform; a pathological table degrades to `PreBlockTable`, never an OOM.

## Self-Check: PASSED

- Files exist in HEAD tree: `internal/llm/models.go`, `internal/channels/telegram/mdv2.go`, `internal/channels/telegram/tables.go` — all confirmed via `git cat-file -e HEAD:<path>`.
- Commits exist: `e412c145`, `75cfc891`, `8da3306d` — all confirmed in `git log --grep="feat(13-03)"`.
- Verification gate green: `go vet`, `go build ./...`, `go test ./internal/llm/ -run Supports`, `go test -race ./internal/channels/telegram/` (unit + fuzz-seed + golden), and the 20s `-fuzz FuzzMdv2` campaign.

## Next Phase Readiness

- **13-05 renderer / 13-06 telegram-core:** `EscapeMarkdownV2` + `PlainTextFallback` are the text-send path; `RenderTablePNG`/`PreBlockTable` are the table path. Both are pure and channel-agnostic — the renderer wires them to `sendPhoto`/`Send`.
- **13-08 photo client:** `llm.SupportsVision` is the `AURA_VISION_CLOUD` env-branch precondition.
- **13-09 integration tier:** the live `400 can't parse entities` send-assertion (deferred from Task 2) and the on-device table-PNG verdict land there.
- No blockers. The branch carries interleaved Phase-15 spike commits from a parallel session, all orthogonal to this plan's files.

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
