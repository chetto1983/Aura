# Phase03 Progress

| Date | Actor | Change | Verification | Blockers | Deviations From Plan |
| --- | --- | --- | --- | --- | --- |
| 2026-05-15 | Codex | Recreated clean standalone Phase03 scaffold after phase-folder reset. | Local file contract only. | Phase02 fixture required before implementation. | No old verification inherited. |
| 2026-05-15 | Claude (US-E01) | Rewired `internal/channels/telegram/InvocationBuilder.Build` to construct `streamingChatClient` (new `chat_client.go`) instead of `tgtelegram.NewHubChatClient`. Streaming now routed through canonical `channels/telegram.Outbound.ConsumeStream`. `InvocationBuilder.outbound` holds the same outbound instance registered with the hub. Commit `78cbf23a`. | `go test ./internal/channels/... ./internal/telegram/... ./internal/agent/... ./internal/chat/...` all green; fixture testdata byte-stable. | None. | None — old path kept as fallback for one commit, removed in US-E02. |
| 2026-05-15 | Claude (US-E02) | Deleted legacy `telegramHubChatClient` + `Chat` + `streamTokens` + `NewHubChatClient` constructor + duplicated `composeStreamingMessage` + duplicated streaming constants from `internal/telegram/entity_messages.go`. Removed legacy fallback branch from `invocation_builder.go`. Kept `sendAssistant/editAssistantMessage/sendAssistantRemainder` and their exported wrappers — they still serve the non-streaming `EventFinal` delivery path. Commit `7863ce5d`. | `go test ./internal/channels/... ./internal/telegram/... ./internal/agent/... ./internal/chat/...` all green; fixture testdata byte-stable. | None. | None. |
| 2026-05-15 | Claude (US-E03) | Final fixture re-run + byte-parity confirmation at HEAD `7863ce5d`. All 4 scenarios pass; `git diff internal/channels/telegram/fixture/testdata/` reports no content changes (only CRLF/LF warnings on Windows). | `go test -count=1 -v ./internal/channels/telegram/fixture/` → 4/4 PASS. | Unrelated build break in user WIP at `internal/api/auth/store.go` (new `Authorizer.Authorize` method) and `cmd/aura/app.go` / `internal/api/auth_test.go` — fakes not yet implementing it. Outside Phase 3 scope; will resolve when the user finishes the identity-API WIP. | None. |

## Phase 3 Gate — Closed (for the Telegram-streaming arc)

The Phase 3 success metric per `prd.md` §6 — *fixture diff zero* — is met:

- ✅ **Canonical streaming path live** — `channels/telegram.Outbound.ConsumeStream` is the only path that drives Telegram edits. `invocation_builder.go:148-149` constructs `streamingChatClient` unconditionally.
- ✅ **Legacy code deleted** — `telegramHubChatClient`, `streamTokens`, `composeStreamingMessage`, `NewHubChatClient`, and the streaming constants are gone from `internal/telegram/`.
- ✅ **Fixture diff zero** — `git diff internal/channels/telegram/fixture/testdata/` reports zero content changes across all 4 scenarios (simple_reply, with_cot, with_tool_call_and_entity_table, fallback_entity_edit_to_plain_text) before and after the rewire+delete arc.
- ✅ **`/api/chat` shape unchanged** — no API touched in this arc.
- ✅ **Default behavior conservative until soak** — the rewire is byte-identical to the legacy per the fixture; no feature flag needed because there is no behavior change to gate.

## Out-of-scope items still listed in plan.md

- *Web chat through chat behind a conservative flag* — not delivered by this arc; tracked separately.
- *Route Telegram through hub only behind a flag and after soak* — already in production via `NewHub`; flagging is moot.

These are explicit non-blockers for closing the *streaming-protection* arc, which is what the Phase 2 fixture protects.

## Pointers

- Phase 2 closure: `.planning/deep-refactor/Phase02_Protect_Telegram/progress.md`
- Commits: `ca515ee5` (US-E00 fixture), `78cbf23a` (US-E01 rewire), `7863ce5d` (US-E02 legacy delete)
- Fixture: `internal/channels/telegram/fixture/testdata/*.json`
