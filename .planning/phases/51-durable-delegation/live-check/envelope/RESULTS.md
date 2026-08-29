# Phase 51 Plan 11 - LIVE Delivery Envelope Results

Date: 2026-08-29

Overall result: **PASS (5/5 verdicts pass)**

The final characterization was driven from the Telegram-owned conversation and
verified against PostgreSQL, the container filesystem, and the authenticated
Telegram Web and Aura cockpit UIs. No verdict below is derived from SSE output.

Human checkpoint: **approved by the operator on 2026-08-29**.

## Final Run Identity

- Marker: `TG51C`
- Telegram conversation: `[redacted; derived from private chat ID]`
- Fan-out key: `f-68731a5fe88d3345`
- Fast job: `d7b2127c-9f54-46a1-a0d5-3752346979be`
- Slow job: `9e2a3ae1-24b1-4a04-9321-9033cd75f3d3`
- Fast child: `w1-7269ada1`
- Slow child: `w2-aad7a80e`
- Fast steer row: `6aa4e06a-ce01-4208-8bae-1c987e934e60`
- Slow steer row: `0d555a94-eed6-4a44-be7e-199029fa3c0e`
- Routed model: `deepseek/deepseek-v4-flash-0731:nitro`
- Context window shown by the cockpit: `1M`

## Image Freshness

The final run used:

- image: `aura:local`
- digest: `sha256:02385f3766a7eeddbae54d1d82f71eb7e854e306bb18f29a474be392f79a6e6a`
- image created: `2026-08-29T18:53:17.077373578Z`
- container created: `2026-08-29T18:53:30.160680911Z`
- container state: `running`, `healthy`
- schema migration: `109`, `dirty=false`

The Telegram request began at `2026-08-29T18:54:15Z`, after the image and
container were created. `aura version` still embeds `commit: unknown` and
`built: unknown`; source provenance therefore comes from the recorded rebuild
and digest, not binary build metadata.

## Remediation Proven Live

The selected policy is origin-scoped delivery. Delegation completion continues
to use `DeliverToConversation`; a cockpit conversation is not sent to Telegram.

Two defects were found and fixed while establishing the Telegram-origin proof:

1. Telegram turns had an Aura identity but no trusted parent idempotency
   operation. `swarm_spawn` therefore failed closed with `operation context
   missing`. The ingress now derives a stable root from the deterministic
   Telegram conversation (`convID(chatID)`) and inbound `messageID`. A replay of
   one update gets the same root; different messages do not collide. Synthetic
   continuations without an inbound message use an explicit UUIDv7 nonce.
2. The first successful retry (`TG51B`, fan-out `f-2816868a5048bc92`)
   exposed a claim/render race. The candidate snapshot contained only FAST;
   SLOW was inserted before `MarkFanoutNudged`, so the SQL marked both rows but
   returned only IDs and the renderer sent the stale one-row snapshot. The
   grouped claim now uses `UPDATE ... RETURNING id, identity_id,
   conversation_id, body, fanout_key`, and the message is rendered only from
   those atomically claimed rows.

Regression coverage:

- `TestTelegramTurnMintsDistinctInteractiveOperationRoots`
- `TestTelegramTurnOperationIsStableForRetriedUpdate`
- `TestFanoutClaimIncludesRowInsertedAfterCandidateSnapshot`
- all `TestFanout*` tests

## Verdict 1 - Cards, Not Raw JSON: PASS

Source: `aura.conversation_turns`, final-run rows 146 and 147.

Row 146 contains the FAST card:

```text
✅ TEST E2E TG51C-FAST: nel workspace esegui controlli reali via shell_exec. Atten…
w1-7269ada1 · 28s
TG51C-FAST: E2E test completato con comandi reali in /workspace...
Report completo: w1-7269ada1.md
```

Row 147 contains the SLOW card:

```text
✅ TEST E2E TG51C-SLOW: nel workspace esegui controlli reali via shell_exec. Atten…
w2-aad7a80e · 1m11s
TG51C-SLOW: attesa di 55 s eseguita...
Report completo: w2-aad7a80e.md
```

Neither row starts with `[` or exposes raw `goal_index` JSON. Playwright showed
both cards in the authenticated cockpit thread.

The authenticated UI was inspected live; private visual evidence was not retained.

## Verdict 2 - Full Reports Are Listed and Previewable: PASS

Source: `aura.assets`.

| id | file_name | mime_type | source_kind | size_bytes | created_at |
|---|---|---|---|---:|---|
| `cf58a35a-606c-4575-b30e-a1951a492170` | `w1-7269ada1.md` | `text/markdown` | `agent` | 876 | `18:54:52.086696Z` |
| `543aeefa-bb67-4d52-b2fa-dc3c6488e0fc` | `w2-aad7a80e.md` | `text/markdown` | `agent` | 2322 | `18:55:34.967690Z` |

Both assets belong to the Telegram conversation. Playwright listed both in the
Artefatti panel and opened `w2-aad7a80e.md`; the preview contained the complete
TG51C-SLOW report, ten-file list, and three SHA-256 hashes.

The authenticated UI was inspected live; private visual evidence was not retained.

## Verdict 3 - Exactly One Telegram Message Per Fan-Out: PASS

### Mid-flight gate

Captured after FAST finished and while SLOW was still running:

- FAST: `succeeded`, attempt `1/3`, completed `18:54:52.115688Z`
- SLOW: `running`, attempt `1/3`
- only steer row: FAST, `drained_at=NULL`, `nudged_at=NULL`
- Telegram: no TG51C completion bubble

The authenticated UI was inspected live; private visual evidence was not retained.

### Terminal grouped claim

Both jobs succeeded on attempt `1/3`. The two steer rows were marked by one
claim with the exact same timestamp:

| row | created_at | nudged_at |
|---|---|---|
| FAST | `18:54:52.113112Z` | `18:55:52.545180Z` |
| SLOW | `18:55:35.061747Z` | `18:55:52.545180Z` |

There were zero matching `pending_notifications` rows.

Playwright enumerated individual Telegram bubbles. Exactly one bubble matched
both markers plus the fixed closing line:

- match count: `1`

```text
TEST E2E TG51C-FAST: ...: completato
TEST E2E TG51C-SLOW: ...: completato

Dettagli nel cockpit.
```

The authenticated UI was inspected live; private visual evidence was not retained.

## Verdict 4 - Distinct Self-Terminating Workers: PASS

Source: container filesystem
`/var/lib/aura/runs/<redacted-conversation-id>/swarm/`.

```text
 18293 w1-7269ada1.jsonl
137717 w2-aad7a80e.jsonl
```

FAST terminal marker:

```json
{"author":"w1-7269ada1","actions":{"state_delta":{"swarm_child_duration_sec":27,"swarm_child_id":"w1-7269ada1","swarm_child_status":"ok"}},"timestamp":"2026-08-29T18:54:52.018218988Z"}
```

SLOW terminal marker:

```json
{"author":"w2-aad7a80e","actions":{"state_delta":{"swarm_child_duration_sec":70,"swarm_child_id":"w2-aad7a80e","swarm_child_status":"ok"}},"timestamp":"2026-08-29T18:55:34.965480857Z"}
```

The child IDs are distinct and stable across job rows, cards, assets, and
transcripts. Both workers terminated themselves with status `ok`.

## Verdict 5 - `swarm_status` Answers From Facts: PASS

The required mid-flight proof comes from the first cockpit drive, conversation
`01a04ea5-26b8-78b9-8635-ceb657d98e3f`, rows 11 and 12. While both workers
were still running, row 11's `swarm_status` result reported:

| child | status | attempt | elapsed | last observed action |
|---|---|---:|---:|---|
| `w1-a383c1aa` | `running` | `1/3` | `11s` | `tool_call shell_exec` at `17:50:53.957Z` |
| `w2-4b5b9eeb` | `running` | `1/3` | `11s` | `tool_call shell_exec` at `17:50:55.946Z` |

Row 12 named both child IDs, statuses, attempts, elapsed seconds, and those
observed actions. The corresponding transcript timestamps and job rows agree;
the answer did not call `swarm_spawn` again.

As a supplementary channel check, at `18:59:22Z` Playwright sent a separate
Telegram turn instructing Aura to load `swarm_status`, call it exactly once for
both final TG51C child IDs, and never call `swarm_spawn`.

Source: `aura.conversation_turns` rows 152-155 and the Telegram device:

| child | status | attempt | elapsed | last observed action |
|---|---|---:|---:|---|
| `w1-7269ada1` | `succeeded` | `1/3` | `28s` | final TG51C-FAST report, then `worker finished: ok` at `18:54:52Z` |
| `w2-aad7a80e` | `succeeded` | `1/3` | `71s` | final TG51C-SLOW report, then `worker finished: ok` at `18:55:34Z` |

The device answer states both completed successfully on the first attempt and
that no `swarm_spawn` call was made. The job ledger and transcript timestamps
agree with the tool result.

## Verification Commands

Passed:

- `go test ./internal/channels/telegram -run TestTelegramTurn -count=1`
- `go test ./internal/channels/telegram -count=1`
- `go test ./internal/swarm -run TestFanout -count=1`
- `go test ./internal/swarm -count=1`
- `go test ./internal/steer -count=1`
- `go build ./cmd/aura`
- `git diff --check`

## Discovery History

The original cockpit-origin run passed cards, assets, transcripts, and status
but intentionally had no Telegram owner. The first Telegram request then failed
closed with `operation context missing`, leading to the ingress-root fix.

The first post-fix Telegram run (`TG51B`) spawned both workers but delivered a
single-worker FAST notification because of the stale snapshot race described
above. Evidence is retained at:

Private visual evidence from this discarded attempt was not retained.

The final `TG51C` run is the first clean 5/5 characterization and supersedes
those failed attempts for the checkpoint verdict.

## What This Does Not Prove

- It does not exercise failed, dead-lettered, or `awaiting_input` fan-outs.
- It does not exercise an artifact at the size or wall-clock cap.
- It does not prove rendering on a physical phone; the device surface was the
  authenticated Telegram Web session supplied by the operator.
- It does not prove delivery through a second channel implementation.
- It does not provide embedded git provenance because the local binary reports
  unknown commit/build metadata.
