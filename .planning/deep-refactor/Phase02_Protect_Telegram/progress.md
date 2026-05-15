# Phase02 Progress

| Date | Actor | Change | Verification | Blockers | Deviations From Plan |
| --- | --- | --- | --- | --- | --- |
| 2026-05-15 | Codex | Recreated clean standalone Phase02 scaffold after phase-folder reset. | Local file contract only. | Needs targeted source audit and verifier. | No old verification inherited. |
| 2026-05-15 | Claude (US-E00) | Added 4th fixture scenario `fallback_entity_edit_to_plain_text` (entity edit fails → adapter retries with plain text → delivery succeeds). Extended `fixture.Capture` with `Option`-based fake-server policies (`WithEntityEditError(n)`); added `APICall.Failed` flag. Existing 3 snapshots byte-stable. | `go test ./internal/channels/telegram/fixture/` green; existing snapshots show zero diff vs HEAD. | None. | None — change scoped strictly to the fixture harness; no production code touched. |

## Phase 2 Gate — Closed

The Phase 2 gate (per `prd.md` §6) is satisfied:

- ✅ **Record-and-replay fixture exists** — `internal/channels/telegram/fixture/` with `Capture()` driving `channels/telegram.Outbound.ConsumeStream` against `httptest.Server`.
- ✅ **Fixture covers simple reply, CoT, tool/entity table** — `TestCaptureSimpleReply`, `TestCaptureWithCoT`, `TestCaptureWithToolCallAndEntityTable`.
- ✅ **Fallback behavior covered** (plan.md §Scope) — `TestCaptureFallback_EntityEditFailsToPlainText` exercises the `editMessage` entity → plain-text fallback inside `outbound.go:197`.
- ✅ **Later adapter output can be byte-compared** — `testdata/*.json` checked in; `go test` regenerates them; `git diff` is the byte-parity oracle.

## Snapshot inventory

| Scenario | Snapshot | Calls | What it proves |
| --- | --- | --- | --- |
| `simple_reply` | `testdata/simple_reply.json` | 2 editMessageText | placeholder edit + final clean edit, both with entities. |
| `with_cot` | `testdata/with_cot.json` | 4 editMessageText | live 🧠 CoT prefix edits + final clean answer edit drops the CoT header. |
| `with_tool_call_and_entity_table` | `testdata/with_tool_call_and_entity_table.json` | 1 editMessageText | entity-rendered Markdown table flushed once before the tool-call Done token; `delivered=false`. |
| `fallback_entity_edit_to_plain_text` | `testdata/fallback_entity_edit_to_plain_text.json` | 3 editMessageText (1 failed) | entity edit returns 400 → adapter retries with plain text → Done-time final entity edit succeeds. `delivered=true`. |

## Next phase

Phase 3 (`Phase03_Move_Channels_Behind_Chat`) is now unblocked. The fixture is the protection contract: any port that changes byte output of the 4 snapshots is a regression.
