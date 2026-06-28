---
spike: 037
name: agentmd-messages1-cache-invariant
type: standard
validates: Agent.md plus always-on skills are profile-first, bounded, protected, and hash-stable at messages[1] across a 20-turn replay.
verdict: VALIDATED
related:
  - internal/agent/prompt/hash.go
  - internal/agent/prompt/builder.go
  - internal/conversations/context.go
  - cmd/aura/cache_audit.go
tags:
  - phase-14
  - agent-md
  - messages-1
  - prompt-cache
---

# Agent.md Messages[1] Cache Invariant

## What Validates

Given a static Aura system prompt, a bounded `Agent.md` profile, and always-on skill instructions, when the prompt builder assembles a 20-turn replay with volatile budget/workspace tails, then:

- `messages[0]` remains byte-stable.
- `messages[1]` remains byte-stable across normal conversation turns.
- `Agent.md` is never present in `messages[0]`.
- `messages[1]` is user-role, profile-first, and bounded.
- Updating the profile changes `messages[1]` only.

## Research

This spike implements the contract produced by spike 036. It uses Aura's real `prompt.PrefixHash`, `PromptBuilder`, `llm.Message`, and tool registry surfaces instead of a synthetic hash.

Relevant external docs:

- OpenAI prompt caching: https://developers.openai.com/api/docs/guides/prompt-caching
- Anthropic prompt caching: https://platform.claude.com/docs/en/build-with-claude/prompt-caching
- Claude Code prompt-caching lessons: https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything

## How to Run

```powershell
go run ./.planning/spikes/037-agentmd-messages1-cache-invariant
```

## What to Expect

The harness prints a base `messages[0]` and `messages[1]` hash on turn 1, then stable checks for turns 2-20. It then simulates a profile update and asserts the system hash remains unchanged while the profile hash changes.

## Observability

Key output from the run on 2026-06-08:

```text
[CHECK] turn 01 base messages[0]=a46d868e840a1f8f6b04db715a2d2800db261697438e9354c91bff6903e2e64e messages[1]=e1edfb42f9cfa5397f6ba1b222f890c176482cd74667453261a46c4ff2f243b6
[CHECK] turn 20 stable messages[0] and messages[1]
[CHECK] profile update changes messages[1] only: messages[0]=a46d868e840a1f8f6b04db715a2d2800db261697438e9354c91bff6903e2e64e messages[1]=29a0c0de9fc6898f1f853b27130e357514df778bd1b51294a9335a97c15d4235
[SUMMARY] VALIDATED phase14 Agent.md messages[1] cache invariant
```

## Investigation Trail

The current Aura code already has the right structural seam for always-on skills: `internal/conversations/context.go` injects a protected synthetic user-role turn immediately after the system prompt, and `cmd/aura/cache_audit.go` already hashes `messages[1]` when present. Phase 14 should extend the provider of that block, not introduce a new second system message.

The prompt builder appends volatile budget/workspace state after history. The harness proves that this tail does not disturb the hash of `messages[0]` or `messages[1]`.

## Results

VERDICT: VALIDATED.

Phase 14 can safely target a profile-first `messages[1]` block. The production implementation should add tests around the real Runner path once the profile store exists, mirroring this spike's assertions.
