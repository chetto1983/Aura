---
spike: 018a
name: table-pre-block
type: comparison
validates: "Given an LLM-style markdown table, when rendered as aligned monospace in a MarkdownV2 pre block and sent live, then it is readable on a phone-width Telegram client"
verdict: VALIDATED
related: [017-telebot-v4-sha-pin-live-send, 018b-table-as-image, 018c-table-restructured]
tags: [telegram, tables, markdownv2, pre-block, phase-13]
---

# Spike 018a: Table → Aligned Monospace Pre-Block

## What This Validates

Given an LLM-style markdown table (narrow 2×4, realistic 4×5, wide 6×6), when rendered as
space-aligned monospace columns inside a MarkdownV2 ```pre``` block and sent live, then it
is readable on a phone-width Telegram client — wrapping behavior measured via a width ruler.

## Research

- Bot API entity list has **no table type** (mention…date_time only) — the gap is real.
- Inside pre/code entities only `` ` `` and `\` are reserved (verified live: payloads full
  of unescaped `|`, `-`, `.` inside the fence delivered clean).
- Approach: zero-dep stdlib — parse `|`-rows, pad to per-column rune widths, header rule.

## How to Run

```bash
set -a; source <(tr -d '\r' < .env); set +a
go run -tags spike_telegram ./.planning/spikes/018a-table-pre-block
```

## What to Expect

T1/T2/T3 pre-blocks + a 20→56-char width ruler delivered to the operator chat; forensic log
reports widest line + payload bytes per table; exit 0.

## Investigation Trail

1. Wire tier green first run: T1 (26 char), T2 (38), T3 (**109** — stress) + ruler, all with
   `pre` entity confirmed in the sendMessage response (msg 3419-3422).
2. Payload cost trivial: 222-976 bytes vs the 4096 cap — length is not the constraint,
   *width* is.
3. Human checkpoint (operator's phone): ruler pasted back intact through 56| — no wrap
   reported up to 56 chars on the operator's device/font-size. T3 (109 char) readability
   still lost head-to-head to the image variant.

## Results

**VALIDATED — comparison loser** (head-to-head winner: 018b image, on both T2 and T3).

- Wire-level the approach is sound and free: zero deps, copy-pasteable, searchable text.
- Width budget on the operator's device ≥56 chars unwrapped — T1/T2-class tables (≤~40
  chars) fit comfortably; T3-class (109 chars) exceeds any phone budget.
- Keep as the **no-dependency fallback** when image rendering fails, and as the natural
  shape for narrow (≤2-3 col, short-cell) tables if the phase wants a hybrid policy.
