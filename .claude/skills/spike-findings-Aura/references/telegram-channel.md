# Telegram Channel (Phase 13 / Slice 9b)

Implementation blueprint from session-5 spikes (2026-06-07): transport pin, MarkdownV2
discipline, table rendering, artifact file delivery. All five spikes VALIDATED live
against the operator's real bot (`TELEGRAM_BOT_TOKEN` in `.env`, chat `AURA_E2E_CHAT_ID`).

## Requirements

- **telebot.v4 pin = tag `v4.0.0-beta.9`** (commit `9c28310e`, 2026-06-02). Amendment #5's
  "untagged repo → SHA-pin" premise is STALE — the repo is tagged now. CI gate = literal
  `gopkg.in/telebot.v4 v4.0.0-beta.9` grep in go.mod. **PRD amendment required.**
- **Table rendering = PNG image primary** (operator head-to-head verdict on-device, both
  the common 4-col case AND the wide 6-col stress case): markdown tables → gridded PNG →
  `sendPhoto`. Pre-block monospace is the zero-dep fallback when rendering fails;
  key-value cards only for `key|value` 2-col tables. **PRD amendment required** (the PRD's
  renderer.go design is silent on tables).
- **The channel MUST deliver file artifacts** (operator directive 2026-06-07): xlsx, pdf,
  docx, csv, etc. via `sendDocument` — sandbox/skill artifacts (`$AURA_RUN_DIR`,
  `/workspace`) reach the user as files, not just text. **PRD amendment required.**
- The mdv2.go escaper (~80 LOC budget confirmed) must be **entity-aware**: escape reserved
  chars *outside* intended entities, never whole-string (that destroys bold/code spans).
- Test sends go ONLY to the operator's own chat; assert on the Bot-API response payload.

## How to Build It

### Transport (spike 017)

```go
import tele "gopkg.in/telebot.v4"

b, err := tele.NewBot(tele.Settings{Token: token})   // performs live getMe; Offline: true for unit tests
to := tele.ChatID(chatID)                            // ChatID int64 implements Recipient
msg, err := b.Send(to, escaped, tele.ModeMarkdownV2) // ParseMode as plain vararg
```

- `go get gopkg.in/telebot.v4@v4.0.0-beta.9` — resolves, builds, runs under Go 1.26.
- The **send response is the read-back ground truth**: bot-sent messages NEVER appear in
  getUpdates. Assert on `msg.Text` (entities stripped to plain text), `msg.Entities`
  (e.g. `tele.EntityBold`, `tele.EntityCodeBlock`), `msg.Document.MIME`, `msg.Photo`.

### MarkdownV2 discipline (spikes 017/018a)

- Reserved set outside entities: `_*[]()~` + backtick + `>#+-=|{}.!\` — **strict**: one
  naked `-` anywhere = the whole send 400s (`can't parse entities: Character '-' is
  reserved and must be escaped`). Pitfall #18 verified verbatim.
- Inside ```pre```/`code` fences only `` ` `` and `\` are reserved — pipes, dashes, dots
  flow through unescaped (verified live with table payloads).
- Always keep the plain-text fallback: if the escaped send 400s, resend without ParseMode.

### Table rendering (spikes 018a/b/c — comparison, 018b WINNER)

Detect markdown tables in the LLM reply (`|`-rows + `|---|` separator), render each to a
gridded PNG, send via `sendPhoto`; surrounding prose goes out as normal MarkdownV2 text.

Proven pure-Go pipeline (~150 LOC, see `sources/018b-table-as-image/main.go`):

```go
import (
    "golang.org/x/image/font/opentype"
    "golang.org/x/image/font/gofont/gomono"     // cells
    "golang.org/x/image/font/gofont/gomonobold" // header row
)
// 28px = 14pt @2x — survives Telegram's JPEG re-encode legibly
face, _ := opentype.NewFace(ft, &opentype.FaceOptions{Size: 28, DPI: 72, Hinting: font.HintingFull})
// measure per column with font.Drawer.MeasureString, pad 16px, draw grid + rows, png.Encode
photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(pngBytes)), Caption: caption}
```

- Measured costs: 5-21ms render, 22-67KB PNG (4-6 col tables). Latency is a non-issue.
- `golang.org/x/image` is already an *indirect* dep — promote to direct at v0.41.0+.
  Embedded Go fonts: no filesystem fonts, no CGO, no fontconfig.
- Telegram re-encodes photos but does NOT downscale at these sizes (1923px served as-is);
  2x font scale keeps text crisp on-device.
- Fallback pre-block (spike 018a, `sources/018a-table-pre-block/main.go`): pad cells to
  per-column rune widths inside a ``` fence. Operator device showed **no wrap up to ≥56
  chars** — fine for ≤3-col short-cell tables, unreadable ≥100 chars.

### Artifact file delivery (spike 019)

```go
doc := &tele.Document{File: tele.FromDisk(path), FileName: name, Caption: caption}
msg, err := b.Send(to, doc)
// ground truth: msg.Document.{FileName, MIME, FileSize} — Telegram detects MIME itself
```

- 4/4 types round-tripped byte-identical with exact MIME detection (OOXML xlsx/docx,
  pdf, csv), 112-155ms per send; all opened in the right viewer on-device.
- No MIME plumbing needed channel-side: path + filename suffice. Bot upload cap: 50 MB.
- Phase obligation: a `send_file`-shaped path from `$AURA_RUN_DIR`/`/workspace` exports
  to the active chat, symmetric with the AG-UI artifact story.

## What to Avoid

- **Don't SHA-pin telebot** — amendment #5 as written demands a pin format the ecosystem
  moved past; the tag is the pin now (mirror of the spike-014 AG-UI lesson, inverted).
- **Don't whole-string-escape MarkdownV2** — it neutralizes the bot's own formatting.
  Escape outside entities only; the 017 throwaway escaper is a NEGATIVE example for this.
- **Don't read getUpdates for send verification** — bot-sent messages never appear there.
- **Don't ship wide tables as pre-blocks** — 109-char rows lost to the image on-device
  even though the client didn't hard-wrap; columnar legibility ≠ no-wrap.
- **Don't reach for fogleman/gg or a headless browser** for table PNGs — x/image +
  embedded gofonts cover it with zero new dependency surface.
- **Don't trust `AURA_E2E_TOKEN`** — it's the pre-rewrite web-dashboard bearer, not a
  Telegram token. The real token is `TELEGRAM_BOT_TOKEN` in `.env` (gitignored).

## Constraints

- Message text cap 4096 chars; photo caption cap 1024. Table payloads measured 222-976
  bytes — width, not length, is the pre-block constraint.
- MarkdownV2 rejection is all-or-nothing per message: one bad char kills the whole send.
- Telegram re-encodes `sendPhoto` (larger-than-source JPEG, dimensions preserved at
  ≤1923px tested); `sendDocument` is lossless.
- Bot API has NO table entity (full entity list verified) — rendering is always a
  client-side transformation.
- `tele.NewBot` does a live getMe — use `Offline: true` in tests that must not hit the
  network.

## Origin

Synthesized from spikes: 017, 018a, 018b, 018c, 019 (session 5, 2026-06-07)
Source files available in: sources/017-telebot-v4-sha-pin-live-send/,
sources/018a-table-pre-block/, sources/018b-table-as-image/,
sources/018c-table-restructured/, sources/019-artifact-file-delivery/
