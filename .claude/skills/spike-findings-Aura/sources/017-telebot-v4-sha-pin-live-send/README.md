---
spike: 017
name: telebot-v4-sha-pin-live-send
type: standard
validates: "Given telebot.v4 resolved to a commit ≥ 2026-05-08 (amendment #5), when pinned in Aura's module under Go 1.26 and a live bot sends a MarkdownV2 message, then go build stays green and the message lands without 400 can't-parse-entities"
verdict: VALIDATED
related: [014-agui-sdk-module-pin]
tags: [telegram, telebot, go-mod, markdownv2, phase-13]
---

# Spike 017: telebot.v4 Tag-Pin + Live MarkdownV2 Send

## What This Validates

Given telebot.v4 resolved to a commit ≥ 2026-05-08 (amendment #5), when pinned in Aura's
module under Go 1.26 and a live bot sends a MarkdownV2 message with reserved characters,
then `go build ./...` stays green and the escaped message is delivered without
`400 Bad Request: can't parse entities` (Pitfall #18).

## Research

- `go list -m -json gopkg.in/telebot.v4@HEAD` → **`v4.0.0-beta.9`**, commit
  `9c28310edc878df040e40e2b079637ffc8f53c9c`, dated **2026-06-02** (≥ 2026-05-08 ✓).
- **Amendment #5's premise is stale**: the repo is no longer untagged. HEAD *is* the
  `v4.0.0-beta.9` tag — pin by tag, immutable via go.sum. No pseudo-version gymnastics
  (the spike-014 AG-UI problem does not exist here); a CI grep gate on the literal
  `gopkg.in/telebot.v4 v4.0.0-beta.9` in go.mod is trivially satisfiable.
- Module-cache enumeration (`$(go env GOMODCACHE)/gopkg.in/telebot.v4@v4.0.0-beta.9`):
  `ModeMarkdownV2 ParseMode = "MarkdownV2"` (`telebot.go:152`), `type ChatID int64`
  implements `Recipient` (`chat.go:213`), `Bot.Send(to, what, opts...)` accepts a
  `ParseMode` vararg directly. `NewBot` performs a live `getMe` unless `Offline: true`.
- No competing-approach table: the lib choice is already a PRD decision (raw Bot API HTTP
  rejected in SUMMARY.md Stack research; telegramify-markdown-go rejected by amendment #4).

## How to Run

```bash
go get gopkg.in/telebot.v4@v4.0.0-beta.9            # re-arm (go.mod reverted at session end)
set -a; source <(tr -d '\r' < .env); set +a          # TELEGRAM_BOT_TOKEN + AURA_E2E_CHAT_ID
go run -tags spike_telegram ./.planning/spikes/017-telebot-v4-sha-pin-live-send
```

Sends go ONLY to the operator's own chat (`AURA_E2E_CHAT_ID`), tagged `AURA-SPIKE-017-<unix>`.

## What to Expect

- `[SETUP]` authenticated bot identity from live getMe.
- `[PROBE-1]` unescaped reserved chars under MarkdownV2 → rejected with the exact
  Pitfall #18 error (negative control).
- `[PROBE-2]` escaped payload + live `*bold*`/`` `code` `` entities → delivered.
- `[READBACK]` API-returned message contains the unique tag and a parsed bold entity.
- Exit 0 + `[SUMMARY] VALIDATED`.

## Investigation Trail

1. Token recovery detour: `.env`'s `AURA_E2E_TOKEN` looked like the bot token but is the
   **pre-rewrite web-dashboard bearer** (Playwright E2E; Telegram getMe → 404, shape has no
   `<bot_id>:` prefix). The real token lived in the pre-rewrite `data/aura.db`, wiped by
   tabula-rasa. Operator recovered the token from @BotFather → stored as
   `TELEGRAM_BOT_TOKEN` in `.env` (canonical upstream naming). `AURA_E2E_CHAT_ID` survives
   as the operator chat ID.
2. Resolved `@HEAD` → discovered the tag (amendment #5 premise stale, see Research).
3. Pinned, `go vet -tags spike_telegram` + `go build ./...` green first try (Go 1.26).
4. Live run 1/1 green: 400-on-unescaped confirmed verbatim (`Character '-' is reserved and
   must be escaped`), escaped send delivered as `message_id=3417`, tag + bold entity
   round-tripped in the sendMessage response (API-level read-back, no getUpdates needed —
   bot-sent messages never appear in getUpdates).

## Results

**VALIDATED.**

- Pin: `gopkg.in/telebot.v4 v4.0.0-beta.9` resolves, builds, runs under Go 1.26.
- **Amendment needed**: #5 should pin the *tag* `v4.0.0-beta.9` (repo is tagged now), not a
  raw SHA; CI gate = literal version grep in go.mod.
- Pitfall #18 is real and *strict*: even a bare `-` outside entities is a hard 400. The
  Phase-13 escaper cannot be "best effort" — every reserved char outside code entities must
  be escaped or the whole send fails.
- The sendMessage **response is the read-back ground truth**: it carries rendered `text`
  (entities stripped to plain text) + the `entities[]` array — assert formatting by entity
  type, not by markup chars in `text`.
- The throwaway whole-string escaper used here escapes backticks/asterisks too, so it
  destroys intended formatting — the real mdv2.go must be entity-aware (escape *outside*
  entities, preserve intended `*`/`` ` `` spans). Scope confirmation for the ~80 LOC budget.
