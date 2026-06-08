---
spike: 036
name: phase14-agentmd-source-truth
type: standard
validates: Phase 14 Agent.md source-of-truth design reconciles PRD/ROADMAP drift, online standards, and D:/tmp industrial repos.
verdict: VALIDATED
related:
  - .planning/ROADMAP.md
  - prd.md
  - internal/conversations/context.go
  - internal/runner/runner.go
  - cmd/aura/cache_audit.go
tags:
  - phase-14
  - agent-md
  - prompt-cache
  - onboarding
  - source-audit
---

# Phase 14 Agent.md Source Truth

## What Validates

Given the Phase 14 roadmap, PRD Slice 10, current Aura cache-invariant code, official online docs, and local industrial repos under `D:/tmp`, this spike validates a single design contract:

- `Agent.md` is stored per identity on disk at `~/.aura/agents/<identity>/Agent.md`.
- It is injected into the LLM request as protected user-role context at `messages[1]`.
- It must not be a second system message, because `messages[0]` is the static cache anchor.
- `preferences.json`, `metadata.json`, and `changelog.md` sit beside `Agent.md`.
- Telegram first-run onboarding writes the profile after confirm/edit/skip handling, then normal chat resumes.

## Research

Online references:

- OpenAI Codex AGENTS.md guide: https://developers.openai.com/codex/guides/agents-md
- AGENTS.md spec: https://agents.md/
- Claude Code memory: https://code.claude.com/docs/en/memory
- Anthropic prompt caching docs: https://platform.claude.com/docs/en/build-with-claude/prompt-caching
- Claude Code prompt-caching lessons: https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything
- OpenAI prompt caching: https://developers.openai.com/api/docs/guides/prompt-caching
- OpenAI memory/custom instructions: https://help.openai.com/en/articles/8590148-memory-in-chatgpt
- OpenAI personalization: https://openai.com/academy/personalization/
- Letta memory overview: https://docs.letta.com/guides/agents/memory
- Letta archival memory: https://docs.letta.com/guides/ade/archival-memory

Local references:

- `D:/tmp/codex/codex-rs/core/src/agents_md.rs`: best Phase 14 reference. It bounds instruction discovery, walks from project root to cwd, supports override/fallback names, caps bytes, and warns on invalid UTF-8.
- `D:/tmp/codex/codex-rs/core/src/context/fragment.rs`: typed marked user-context fragments are the right mental model for Aura's `messages[1]` profile block.
- `D:/tmp/codex/AGENTS.md`: model-visible context rules warn against rewriting history and frequent cache misses, and require bounded fragments.
- `D:/tmp/nanobot/nanobot/agent/context.py`: loads AGENTS/SOUL/USER/memory/skills but puts them into system prompt. Useful as a caution for Aura.
- `D:/tmp/nanobot/nanobot/agent/memory.py`: useful file-store and atomic history write patterns.
- `D:/tmp/picobot/docs/CONFIG.md`: useful workspace file layout: `SOUL.md`, `AGENTS.md`, `USER.md`, `TOOLS.md`, memory files.
- `D:/tmp/picobot/internal/agent/context.go`: also uses a system-prompt assembly pattern, which Aura should avoid for profile memory.
- `D:/tmp/openhuman/AGENTS.md`: useful cautionary example that rich context must stay bounded and summarized.

## How to Run

No executable harness. This is the source-truth research artifact for the three executable Phase 14 probes:

- `go run ./.planning/spikes/037-agentmd-messages1-cache-invariant`
- `go run ./.planning/spikes/038-profile-store-atomic-contract`
- `go run ./.planning/spikes/039-telegram-onboarding-loopagent-prototype`

## What to Expect

The implementation plan for Phase 14 should treat the PRD wording "second system message" as stale. The locked contract is:

- `messages[0]`: byte-stable Aura system prompt only.
- `messages[1]`: protected user-role profile/instruction fragment.
- Profile content comes first inside `messages[1]`; always-on skills follow.
- Profile updates change `messages[1]` and must leave `messages[0]` unchanged.
- Context ladder must protect the synthetic `messages[1]` turn like it already protects always-on skills.

## Observability

Phase 14 should extend the hidden cache audit so operators can inspect:

- `messages[0]` hash, stable across replay.
- `messages[1]` hash, stable across replay until profile update.
- Presence/order assertions: profile marker before always-on skill marker.
- Bounded `messages[1]` byte size.
- `aura profile show --identity <id>` parsed section tree.
- `changelog.md` entries for every automatic or manual profile update.

## Investigation Trail

The key conflict is in the PRD: Slice 10 still says "second system message" in places, while the Phase 14 roadmap and current Aura code explicitly require `Agent.md` at `messages[1]` to preserve the KV-cache invariant. Online caching docs and `D:/tmp/codex` both support the roadmap direction: keep static provider-visible prefix stable, then place bounded user-specific context after it.

The best industrial repo is `D:/tmp/codex`. It has the most directly transferable ideas: bounded instruction docs, ordered discovery, explicit override precedence, root-to-cwd traversal, and typed model-visible user fragments. `nanobot` and `picobot` are useful for file layout and memory write ideas, but their system-prompt context assembly is the wrong cache posture for Aura.

## Results

VERDICT: VALIDATED.

Phase 14 should implement a disk-backed profile store and compose `Agent.md` into the existing protected user-role `messages[1]` seam. Neo4j is out of scope for `Agent.md`; graph memory remains Phase 15. The plan should amend or override PRD text that says "second system message".
