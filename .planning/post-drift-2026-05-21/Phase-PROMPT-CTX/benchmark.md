# Phase-PROMPT-CTX Benchmark

Role: benchmark.

Smoke checks are allowed only as prechecks. Phase completion requires the
ground-truth benchmarks below.

## Precheck

Command:

```powershell
go test ./internal/conversation ./internal/agent ./cmd/aura ./internal/channels/telegram -count=1
```

Purpose:

- Confirm the touched prompt/channel packages compile before live probes.

This is not completion evidence.

## Benchmark B1 - Prompt Snapshot

Command:

```powershell
go test ./internal/conversation ./internal/agent -run "Prompt|Capsule|Grounding" -count=1
```

Ground truth:

- Stable prompt contains required section anchors.
- Stable prompt excludes `SOUL.md`, `USER.md`, `AGENT.md`, `TOOLS.md`.
- Runtime capsule includes time metadata and is marked as metadata only.
- Changing current time changes runtime capsule hash but not stable prompt hash.

Pass threshold:

- All assertions pass.

## Benchmark B2 - Channel Parity

Command:

```powershell
go test ./cmd/aura ./internal/channels/telegram -run "Prompt|Invocation|WebChat" -count=1
```

Ground truth:

- Equivalent web and Telegram turns expose the same prompt module names.
- Channel differences are limited to channel/thread metadata.
- ask_user approval flow remains durable in `chat_questions`.

Pass threshold:

- Golden/module parity tests pass.

## Benchmark B3 - Tool Routing And Loop Guard

Command:

```powershell
go test ./internal/agent ./internal/agent/tools/registry -run "Tool|Search|Loop|Validation" -count=1
```

Ground truth:

- Always-on tool set remains bounded.
- Search actions route wiki/source/archive/user/operational recall.
- Repeated validation errors do not create unbounded tool loops.

Pass threshold:

- Tests pass and no new always-on tool is added without explicit plan update.

## Benchmark B4 - Live DB Prompt Health

Command:

```powershell
go run ./cmd/probe_chat --case prompt-health
```

Ground truth:

- Probe reads `/data/aura.db` through the running container.
- Report includes run count, p50/p95/max prompt tokens, LLM-call and tool-call
  outliers, compaction count, top recoverable tool classes, and memory-kind
  distribution.
- Report includes prompt version and stable/capsule hashes for the live turn.
- Report does not include raw prompt, raw tool args, raw tool outputs, secrets,
  or full user facts.

Pass threshold:

- Endpoint/probe completes under 10s.
- Fields are present and non-empty where live data exists.
- Redaction assertion passes.

## Benchmark B5 - Real Conversation Pack

Command:

```powershell
go run ./cmd/probe_chat --case prompt-context-pack
```

Cases and durable checks:

- User-memory PDF summary:
  - output artifact exists in source/document store,
  - PDF bytes are valid,
  - retrieved facts come from `user_memory` or approved source rows,
  - no mojibake bullets.
- Local weather:
  - Aura uses remembered location when user says "da me",
  - no location re-ask if user-memory has active residence,
  - no unnecessary tool loop.
- Wiki graph question:
  - Aura uses `search` graph actions or bounded wiki retrieval,
  - no full graph dump in prompt.
- Failed tool validation:
  - at most one corrected retry for same field,
  - no infinite loop,
  - tool_attempts has redacted arg keys only.
- Destructive deletion:
  - ask_user approval row exists before delete,
  - denied/cancelled path leaves file untouched.

Pass threshold:

- Every case passes its durable check.
- Prompt tokens for routine local/weather cases stay below 25k.
- No case exceeds 5 LLM calls unless benchmark marks it as complex.

## Benchmark B6 - Full Gates

Commands:

```powershell
go vet ./...
go build ./...
go test ./... -count=1
git diff --check
```

Pass threshold:

- All commands pass or a documented external blocker is recorded in
  `progress.md`.
