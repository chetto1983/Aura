---
spike: 019
name: artifact-file-delivery
type: standard
validates: "Given real artifact files (xlsx, pdf, docx, csv), when sent via sendDocument with proper filenames, then they arrive typed correctly (filename + MIME + size) and open on the operator's device"
verdict: VALIDATED
related: [017-telebot-v4-sha-pin-live-send, 018b-table-as-image]
tags: [telegram, artifacts, sendDocument, xlsx, pdf, docx, phase-13]
---

# Spike 019: Artifact File Delivery via sendDocument

## What This Validates

Operator requirement (2026-06-07, mid-session): the Telegram channel MUST deliver file
artifacts — documents, Excel, PDF, etc. Given real files of each type, when sent via
`sendDocument` with proper filenames, then the API-returned `Document` metadata matches
(filename, Telegram-detected MIME, byte size) and each file opens on the operator's device.

## Research

- Fixtures are **real files**, not stubs: xlsx via host openpyxl 3.1.5; PDF and DOCX
  handcrafted minimal-valid (PDF 1.4 xref table; DOCX = OOXML zip with
  `[Content_Types].xml` + `_rels/.rels` + `word/document.xml`); all carry a unique
  `AURA-SPIKE-019-<unix>` tag and were read back host-side before sending.
- telebot surface: `&tele.Document{File: tele.FromDisk(path), FileName, Caption}`;
  the sendDocument response carries Telegram's own MIME detection — the wire ground truth.
- Bot upload cap is 50 MB (not probed — phase artifacts are KB-MB scale).

## How to Run

```bash
python3 ./.planning/spikes/019-artifact-file-delivery/fixtures.py D:/tmp/spike-019-fixtures
set -a; source <(tr -d '\r' < .env); set +a
go run -tags spike_telegram ./.planning/spikes/019-artifact-file-delivery
```

## What to Expect

Four `[READBACK]` lines asserting filename + MIME + size per artifact; exit 0.

## Investigation Trail

1. Fixture generation + host read-back: xlsx reopened via openpyxl (tag cell intact),
   docx zip parts enumerated, PDF magic verified.
2. Live run 1/1 green (msg 3433-3436): all four MIME types detected exactly
   (`…spreadsheetml.sheet`, `application/pdf`, `…wordprocessingml.document`, `text/csv`),
   sizes byte-identical, 112-155ms per send.
3. Human checkpoint: **all 4 files open correctly on the operator's phone** in their
   respective viewers.

## Results

**VALIDATED.**

- `sendDocument` round-trips arbitrary artifact types losslessly with correct typing —
  the Phase-13 renderer needs only a path + filename, no MIME plumbing (Telegram detects).
- Phase obligation: wire sandbox/skill-produced artifacts (`$AURA_RUN_DIR`, `/workspace`
  exports) to this path — a `send_file`-style capability on the channel, symmetric with
  the AG-UI artifact story.
- The sendDocument response is the read-back ground truth (same pattern as 017/018):
  assert on returned metadata, never on the reply text.
