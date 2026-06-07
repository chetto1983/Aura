---
spike: 018c
name: table-restructured
type: comparison
validates: "Given the same markdown table, when transformed to per-row key-value blocks (tabular shape abandoned), then content survives and reads naturally on mobile with zero dependencies"
verdict: VALIDATED
related: [018a-table-pre-block, 018b-table-as-image]
tags: [telegram, tables, key-value, markdownv2, phase-13]
---

# Spike 018c: Table → Restructured Key-Value Blocks

## What This Validates

Given the same T1/T2/T3 markdown tables, when each data row is restructured to a bold row
header (`▪ *first-col*`) plus indented `header: value` lines (tabular shape abandoned),
then the content survives MarkdownV2 escaping and reads naturally on a mobile client.

## Research

No new deps — stdlib + the 017 escaper prototype. Empty/em-dash cells (`—`) skipped to cut
noise. Headers taken from row 0 and inlined per row (mobile-native "card" reading).

## How to Run

```bash
set -a; source <(tr -d '\r' < .env); set +a
go run -tags spike_telegram ./.planning/spikes/018c-table-restructured
```

## What to Expect

Three restructured renderings delivered with bold/italic entities parsed; payload bytes
logged per table; exit 0.

## Investigation Trail

1. First run green (msg 3430-3432): T1 242B/13 entities, T2 317B/11, T3 857B/11. The
   escaper handled `(`/`)` literals inside the italic header annotation only with explicit
   pre-escaped `\\(`/`\\)` — entity-aware escaping confirmed non-optional (matches 017).
2. T3's 109-char-wide table flows with no width constraint — the structural advantage —
   but the human checkpoint still preferred the image: scanning across rows (comparing the
   same column between rows) is lost when each row becomes a card.

## Results

**VALIDATED — comparison loser** (winner: 018b image on both T2 and T3).

- Zero-dep and width-proof, but it destroys *columnar scanning* — fine for record-style
  data ("show me phase 13's details"), wrong for comparison tables (the common LLM case).
- Keep as a possible policy for 1-row tables or `key | value` two-column tables, where the
  card shape is actually the more natural rendering. Not the default.
