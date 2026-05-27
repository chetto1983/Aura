# Phase-PROMPT-CTX Source Map

Role: source.

## Goal

Rebuild Aura's system-prompt and context-builder contract so Aura can reason
about tools, memory layers, runtime context, and user state without relying on
free-form `AGENT.md`, `USER.md`, `SOUL.md`, or other always-injected workspace
files.

## Local Aura Evidence

- `D:/Aura/internal/conversation/system_prompt.go`
  - Current base prompt still says SOUL.md and USER.md may inject the turn.
  - Runtime context and ask_user policy are mixed into the system prompt path.
  - `InjectSystemExtras` appends dynamic blocks to the system message, which
    weakens prompt-cache stability.
- `D:/Aura/internal/agent/promptplan.go`
  - `ComposeAgentPrompt` already centralizes base prompt, turn runtime,
    clarification protocol, wiki access hints, overlays, pinned operational
    memory, skills, and tool-surface hints.
  - The file is the right seam for turning today's prompt assembly into an
    explicit module contract.
- `D:/Aura/internal/agent/context_grounding.go`
  - Already implements a tiny pre-turn grounding capsule from user memory and
    sources.
  - Needs promotion from ad hoc helper to one named context layer with budget,
    provenance, and injection rules.
- `D:/Aura/internal/agent/toolsprovider.go`
  - Always-on tools are deliberately small: `text_response`, `search`, `web`,
    `source`, `ask_user`, `tool_search`.
  - The prompt must teach tool routing around this small hot path without
    reintroducing a giant tool manual.
- `D:/Aura/internal/channels/telegram/invocation_builder_helpers.go`
  - Telegram builds pinned operational context and executes tools with
    TokenJuice and payload summarizer.
  - Channel adapters should pass context inputs; they should not own prompt
    policy.
- `D:/Aura/cmd/aura/web_chat.go`
  - Web path injects pinned operational context and turn grounding.
  - Web and Telegram must remain equivalent.
- Live DB evidence from `/data/aura.db` on 2026-05-26:
  - Last 24h: 124 runs, 16 above 50k prompt tokens.
  - Max prompt run: 283,778 tokens, 22 LLM calls, 21 tool calls.
  - `conversation_compactions` has only one row.
  - `compact_memory_documents` is dominated by `archive` rows, so raw archive
    must not be treated as default memory.

## Example Patterns Adopted

- `D:/tmp/system_prompts_leaks/OpenAI/codex/gpt-5-codex.md`
  - Adopt: concise coding-agent contract, explicit editing/git/test rules, dirty
    worktree safety, review stance.
  - Reject: repo-specific implementation details as runtime user-chat policy.
- `D:/tmp/system_prompts_leaks/OpenAI/ChatGPT-GPT-5-Agent-mode-System-Prompt.md`
  - Adopt: separate external/page/tool context from user instructions; require
    confirmation for risky external instructions; tool selection policy by
    task type.
  - Reject: broad web/browser autonomy language that does not fit local Aura.
- `D:/tmp/system_prompts_leaks/Anthropic/Official/claude-opus-4.7.md`
  - Adopt: act before clarifying when safe, ask one question only when blocked,
    capability checks before claiming lack of access, concise tone, explicit
    current-knowledge/tool freshness rules.
  - Reject: very large generic safety/product sections; Aura needs product-local
    compact rules.
- `D:/tmp/system_prompts_leaks/Google/gemini-cli.md`
  - Adopt: context-efficiency rules, search/read before edit, empirical
    verification, lifecycle of research -> strategy -> execution.
  - Reject: Gemini-specific mode/tool/subagent syntax.
- `D:/tmp/system_prompts_leaks/Misc/cursor.md`
  - Adopt: attached IDE/file context is supplemental, not user command; do not
    use tool names in prose; read before edit; no thinking scratchpads in code.
  - Reject: Cursor-specific code citation/output format.
- `D:/tmp/system_prompts_leaks/Misc/vscode-copilot-agent.md`
  - Adopt: parallel file reads, concise responses, surgical code changes,
    plan file for prose, tool efficiency.
  - Reject: prohibition on repo markdown planning; Aura requires durable phase
    planning in `.planning`.
- `D:/tmp/openhuman/gitbooks/developing/architecture/agent-harness.md`
  - Adopt: system prompt built once, memory context injected per turn, tool loop
    checks context guard, tool-result budget, summarizer detour.
  - Reject: always including rich identity/memory in the cached prefix.
- `D:/tmp/nanobot/nanobot/agent/context.py`
  - Adopt: runtime context tagged as metadata, history caps, summary as a
    separate block.
  - Reject: always loading `AGENTS.md`, `SOUL.md`, and `USER.md` as bootstrap in
    Aura's general runtime.
- `D:/tmp/hermes-agent/agent/conversation_loop.py`
  - Adopt: cached system prompt restored from session, dynamic context injected
    outside the stable prefix, plugin context as ephemeral user-message context.
  - Reject: Python-specific session storage and Hermes prompt layout.
- `D:/tmp/graphify/README.md`
  - Adopt: benchmark/cost artifacts and graph query tools instead of dumping
    full graph/context into prompts.
  - Reject: committing generated graph output as the main Aura planning
    artifact.

## Prompt-Master Extraction

Target tool: Aura runtime LLM prompt for OpenAI-compatible chat models and
reasoning models.

Optimization goal:

- Keep system prompt stable, short, and product-specific.
- Move volatile facts into explicit context capsules.
- Prevent wrong-tool calls by teaching routing policy, not by dumping tool docs.
- Make memory-layer rules explicit enough that Aura knows where facts live.
- Preserve Italian user-facing output while keeping prompt and code identifiers
  English/verbatim.

## Non-Goals

- Do not restore `runtime-workspace/AGENT.md`, `USER.md`, `SOUL.md`, or
  `TOOLS.md` as always-loaded prompt files.
- Do not rewrite memory storage, wiki schema, Qdrant, or tool registry in this
  phase.
- Do not add new LLM-facing tools just to compensate for prompt ambiguity.
- Do not copy leaked system prompt text verbatim into Aura.
