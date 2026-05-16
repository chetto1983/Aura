# Phase03 Plan - Move Channels Behind Chat

Status: closed for the Telegram-streaming channel migration arc. The later
web `/api/chat` Hub migration was closed during Phase01B repair and Phase01C
falsification.

## Goal

Make `chat` the normalized traffic layer for channels.

## Scope

- Port Telegram outbound into `channels/telegram` after Phase 2 fixture parity.
- Route web chat through `chat` behind a conservative flag.
- Keep `/api/chat` JSON stable.
- Route Telegram through hub only behind a flag and after soak.

## Non-Goals

- No Telegram port without Phase 2 fixture.
- No web API shape change.
- No agent runtime collapse.

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Telegram outbound under channels | this file | `benchmark.md` | `source.md` | met |
| Web chat through chat | Phase01B/Phase01C repair evidence | Phase01B + Phase01C benchmarks | Phase01B/Phase01C source files | met later |
| `/api/chat` shape stable | this file | `benchmark.md` | `source.md` | met |
| Conservative flags | this file | `benchmark.md` | `source.md` | met by byte parity |

## Implementation Gate

Closed: fixture diff is zero for the Telegram-streaming arc. Future channel
work must keep the fixture byte-parity gate.
