# Aura, Picobot, Hermes Inventory

Date: 2026-05-03

Goal: compare Aura, Picobot, and Hermes Agent as product/runtime patterns, then decide what Aura should adopt without rewriting the whole system.

References:

- Aura current state: `prd.md`, `docs/implementation-tracker.md`, `docs/daily-questions-gap-audit-2026-05-03.md`
- Picobot local reference: `D:\tmp\picobot`
- Picobot prior audit: `docs/picobot-tools-audit.md`
- Hermes Agent repo: https://github.com/NousResearch/hermes-agent
- Hermes skills: https://hermes-agent.nousresearch.com/docs/user-guide/features/skills
- Hermes cron: https://hermes-agent.nousresearch.com/docs/user-guide/features/cron
- Hermes delegation: https://hermes-agent.nousresearch.com/docs/user-guide/features/delegation
- Hermes security: https://hermes-agent.nousresearch.com/docs/user-guide/security

## Executive Decision

Aura should stay the core product. It already has the right durable shape for a standalone second brain: SQLite, source inbox, OCR, wiki, review queue, dashboard, Telegram, auth, scheduled jobs, and swarm telemetry.

Picobot is useful as a runtime pattern library: tool registry, stateless system channels, simple local memory/skill tools, MCP wrapping, and channel dispatch. Copy patterns, not the full surface.

Hermes is useful as a product direction reference: procedural memory through skills, self-improvement after repeated work, skill-backed cron jobs, isolated subagents, and a stronger security/approval model. Adopt these as Aura-native flows behind review gates.

The synthesis:

> Aura should become a second-brain compiler with procedural learning.
> Wiki/source/archive are declarative memory. Skills are procedural memory. Agent jobs are routines. Swarm is parallel read/audit. The review queue is the mutation gate.

## Capability Inventory

| Area | Aura today | Picobot best | Hermes best | Aura decision |
| --- | --- | --- | --- | --- |
| Agent loop | Telegram tool loop, bounded `internal/agent.Runner`, parallel tool calls, structured tool errors. | Compact loop and channel hub; background `heartbeat`/`cron` channels are stateless to avoid context bloat. | Fresh sessions for cron/subagents; progress and cancellation model. | Keep Aura runner. Add stricter "fresh context" defaults for scheduled routines and long audits. |
| Durable memory | Wiki pages, source inbox, OCR text, conversation archive, summarizer proposals. | `MEMORY.md` plus daily notes; quick append tools. | Persistent memory, session search, user model. | Do not add a parallel memory directory. Build `search_memory` across wiki + sources + archive and a small profile/preferences layer. |
| Wiki/source pipeline | Strong: PDFs, OCR, source dedup, ingest, lint, graph, dashboard. | Weak: file memory only. | General agent memory, not a source/wiki compiler. | Aura's unique advantage. Invest here first. |
| Skills | Progressive-disclosure manifest, multi-root loader, dashboard, catalog install/delete behind admin. | Direct `create_skill`, `read_skill`, `delete_skill` tools using workspace root. | Skills as procedural memory; agent creates/updates skills after complex tasks. | Add `propose_skill_change`, not direct self-write. Install only after review/smoke test. |
| Scheduler | SQLite reminders, maintenance jobs, weekday/every-minute recurrence, `agent_job`, `daily_briefing`. | In-memory cron UX reference. | Skill-backed cron, fresh sessions, output delivery, non-recursive cron guard, toolset limits, `wakeAgent`, `context_from`. | Extend Aura `agent_job` with attached skills, per-job toolset, context chaining, and skip-if-unchanged prechecks. |
| Swarm/subagents | `internal/swarm` store/manager, role planner, dashboard metrics, read-only tool routing. | `spawn` is only a stub. | `delegate_task` with isolated context, restricted toolsets, parallel batch, concurrency limit. | Keep Aura swarm. Add explicit toolset profiles and richer synthesis/provenance. |
| MCP/tools | Stdio + Streamable HTTP MCP client, dynamic tools, dashboard invocation. | Dynamic MCP wrapper pattern. | Tool gateway and toolset selection. | Keep Aura MCP behind existing config; add per-agent-job toolset controls before expanding dangerous tools. |
| UI/review | Dashboard for wiki, sources, tasks, skills, MCP, proposals, swarm. | No comparable first-party dashboard. | CLI/TUI/gateway; strong operational UX. | Aura's dashboard is a core differentiator. Improve batch review, provenance, and answer evidence. |
| Safety | Auth, admin gates, no raw tool args, review queue, propose-only agent jobs. | `os.Root` containment, but broad exec/filesystem/skill writes are too open. | Defense in depth: authorization, dangerous-command approval, sandboxing, credential filtering, session isolation. | Adopt Hermes-style policy layers before adding exec/filesystem/self-modifying skills. |
| Metrics/debug | Turn telemetry, swarm task metrics, debug harnesses, E2E prompts. | Basic logs. | Cron/subagent progress and output storage. | Add user-facing latency/evidence breakdown on demand. |

## Best Parts To Keep

### From Aura

- Standalone second-brain shape: source inbox -> OCR -> wiki -> graph -> review.
- SQLite persistence for scheduler, auth, archive, summaries, swarm, and metrics.
- Review-gated memory growth with `propose_wiki_change`.
- Dashboard as operator console, not just a chat bot sidecar.
- Bounded `agent_job` routines with propose-only write policy.
- `daily_briefing` as the start of a daily command center.

### From Picobot

- Stateless background/system channels so scheduled jobs do not grow the interactive chat context forever.
- Memory pollution guard: reject heartbeat/status/no-op content before it becomes durable memory.
- Channel hub idea: progress/delivery decoupled from the LLM loop.
- Tool registry and MCP wrapping pattern, already mostly absorbed by Aura.
- `os.Root` containment as a reference if Aura later adds admin filesystem tools.

### From Hermes

- Skills as procedural memory: repeated workflows should become reusable instructions with examples and constraints.
- Skill-backed scheduled jobs: a recurring job should attach one or more skills instead of stuffing all procedure text into the prompt.
- Fresh isolated contexts for cron/subagents.
- Per-job/subagent toolsets to reduce context, cost, and blast radius.
- Non-recursive scheduled-job guard: cron/job executions must not create more cron jobs unless explicitly allowed.
- `wakeAgent`-style prechecks for watchers: only wake the LLM when monitored state changed.
- `context_from`-style chaining for daily digests: a synthesis job can consume outputs from earlier jobs.
- Stronger safety layers before exposing shell/filesystem/self-modifying behavior.

## What Not To Copy

- Do not port Picobot's direct `write_memory`/`edit_memory`/`delete_memory` as a separate memory layer. Aura's wiki/source/archive model is better.
- Do not expose Picobot-style direct `create_skill` or `delete_skill` to the model. Skill changes should be proposals first.
- Do not add broad filesystem or exec tools yet. If added later, they need admin gates, allowlists, output redaction, and workspace containment.
- Do not move Aura into Hermes. Hermes is a reference for patterns, not the product architecture.
- Do not let scheduled jobs mutate durable memory directly by default. Propose-only should remain the default.

## Unified Architecture For Aura

| Layer | Meaning | Current object | Next evolution |
| --- | --- | --- | --- |
| Declarative memory | Facts, sources, decisions, notes. | Wiki pages, sources, archive. | Unified `search_memory` with citations and profile/preferences extraction. |
| Procedural memory | How Aura should do repeated work. | Installed skills + prompt manifest. | Reviewable skill drafts, smoke prompts, provenance. |
| Routines | Things Aura runs without being asked every time. | Scheduler + `agent_job`. | Skill-backed jobs, toolsets, prechecks, chained outputs. |
| Parallel cognition | Broad read-only exploration without context rot. | AuraBot swarm. | Toolset profiles, better planner, evidence/provenance in synthesis. |
| Mutation gate | Where durable changes become safe. | Proposal/review queue. | Batch review, provenance, confidence, duplicate/conflict detection. |

## Priority Backlog

### P0 - Unified Evidence Search

Build `search_memory` over wiki pages, source OCR/text, and optionally conversation archive snippets.

Minimum:

- Return compact evidence items: type, title, slug/source ID/turn ID, score, snippet, and page number when available.
- Prefer source/page evidence for document questions.
- Keep `search_wiki` as a narrower legacy/specialized tool.

Why first: many real user questions fail because Aura can remember, but cannot yet show a unified "why".

### P1 - Evidence Envelope

Standardize an internal evidence envelope that survives tool calls and reaches final answers when useful.

Minimum:

- Wiki slugs, source IDs, source filenames, OCR page numbers, URLs, conversation turn IDs, swarm run/task IDs.
- User-facing mode for "fammi vedere il perche" and "dammi solo le prove".
- No noisy citations in simple chat unless evidence matters.

### P1 - Proposal Provenance And Batch Review

Make proactive memory growth manageable.

Minimum:

- Proposal origin: source IDs, turn IDs, tool name, agent job ID, swarm run/task ID, confidence, reason.
- Dashboard batch approve/reject.
- Duplicate proposal detection.

### P2 - Skill Creation Workflow

Add Hermes-style procedural learning, but through Aura's review gate.

Minimum:

- `propose_skill_change` tool that creates a pending skill draft, patch, or deletion proposal.
- Draft includes `SKILL.md`, trigger rules, allowed tools, examples, and one smoke prompt.
- Dashboard review installs or rejects.
- Smoke test after install before marking usable.

### P2 - Skill-Backed Agent Jobs

Make scheduled routines reusable and less prompt-heavy.

Minimum:

- Extend `agent_job` payload with `skills`, `enabled_toolsets`, `context_from`, and `wake_if_changed`.
- Fresh context per run.
- Disable schedule-mutating tools inside scheduled agent runs.
- Persist last output and metrics for dashboard inspection.

### P2 - Hot Profile And Preferences

Create a compact, derived user/project profile that Aura can inject without searching the whole wiki every turn.

Minimum:

- `user_profile` and `operator_preferences` pages or SQLite rows.
- Update only through proposals or explicit user "save this preference".
- Staleness checks: "what do you know about me that might be old?"

### P3 - Web Watchers And Threshold Alerts

Make monitoring useful without wasting LLM calls.

Minimum:

- Watch URL/search query.
- Store last snapshot/extracted values.
- Diff or threshold before waking the LLM.
- Notify only on material change.

### P3 - Self-Improvement Loop

After slow, failed, repeated, or user-corrected workflows, Aura should propose a memory/skill/job improvement.

Minimum triggers:

- Same procedure requested 3 times.
- User corrects the same behavior twice.
- Agent job fails repeatedly.
- A manual sequence becomes stable enough to encode as a skill.

Output should be a proposal, not an automatic mutation.

## Real-User Acceptance Prompts

Use these to prevent the project from becoming impressive but useless:

1. "Trova nei miei documenti la prossima scadenza e fammi vedere la fonte."
2. "Cosa sai di me che potrebbe essere vecchio o sbagliato?"
3. "Cosa e' cambiato nella mia wiki questa settimana?"
4. "Aggiorna questa pagina senza duplicare contenuto."
5. "Crea una skill per il mio briefing mattutino."
6. "Ogni mattina usa quella skill e mandami solo le cose importanti."
7. "Monitora questa pagina e avvisami solo se cambia davvero."
8. "Usa agenti paralleli per auditare il mio second brain e dimmi cosa manca."
9. "Fammi vedere dove hai perso tempo nella risposta precedente."
10. "Questa informazione contraddice qualcosa che hai gia' salvato?"

## Recommended Next Slice

Do `search_memory` first.

Reason: it unlocks document Q&A, evidence, profile checks, daily briefings, swarm synthesis, and future proposal quality. Skill creation is exciting, but without unified evidence Aura risks learning procedures on top of incomplete memory.

Second slice: proposal provenance + batch review.

Third slice: `propose_skill_change`.

This sequence keeps Aura useful and trustworthy while it becomes more proactive.

## Implementation Tool Choice

For implementation work, use a project-local Ralph-style loop pack instead of adopting a heavyweight external orchestrator.

Selected tool:

- `loops/aura-implementation/RALPH.md`

Why:

- It matches the Ralph Loops portable package model: one `RALPH.md` entrypoint plus bundled scripts.
- It keeps Aura local-first and inspectable.
- It works with Codex today, and can be read by Claude/goose/Ralph-compatible runtimes later.
- It avoids SaaS/backlog auto-merge behavior until Aura's review and verification loops are stronger.
- It bakes in Aura-specific guardrails: explicit staging, no `.env`, no raw wiki data, no broad mutation, review-gated skills, propose-only scheduled jobs.

External references considered:

- Ralph Loops open format: https://ralphloops.io/specification/
- Claude Ralph Loop plugin: https://claude.com/plugins/ralph-loop
- PageAI Ralph loop script/runtime: https://github.com/PageAI-Pro/ralph-loop
- Goose Ralph Loop recipe: https://goose-docs.ai/docs/tutorials/ralph-loop/
- Wiggum CLI: https://wiggum.app/

Decision:

Use `loops/aura-implementation` as the implementation harness now. Revisit Wiggum/PageAI/goose only if Aura needs unattended backlog execution across many issues. For the next few slices, the bottleneck is not orchestration software; it is crisp specs, strong verification, and evidence/provenance features.
