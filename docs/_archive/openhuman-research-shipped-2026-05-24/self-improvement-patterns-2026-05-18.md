# Self-improvement patterns across D:/tmp agent codebases

Research date: 2026-05-18
Trigger: Phase-OP in flight (US-OP01..05). Want to validate the chosen direction (static operational lessons file + system-prompt injection + mid-conv overlay reload) against the canonical patterns implemented elsewhere, and identify Phase-OP+ extensions worth queuing.

Codebases studied: `openhuman` (Rust, heavy), `mem0` (Python, memory library), `hermes-agent` (Python, dev-assistant), `elysia` (Python, web), `cli-printing-press` (Go), `nanobot` (Python), `picobot` (Go), `recursive-llm` (Python, toy), `aura-agent-loop-papers` (PDF/txt set), `aura-phase8-research` (notes).

---

## Inventory

| Repo | Lang | One-line | Self-improvement mechanism |
|------|------|----------|----------------------------|
| `openhuman/` | Rust | Multi-agent harness (Anthropic-style chat) | **Full stack**: Archivist post-turn hook → FTS5 episodic + `MEMORY.md` curation; `ReflectionHook` (LLM-extracted observations/patterns/user_preferences/user_reflections); `ToolMemoryCaptureHook` (user edicts + repeated tool failures → `tool-{name}` namespace); `ToolMemoryRulesSection` pins Critical/High rules into the **system prompt** (compression-resistant) |
| `mem0/` | Python | Universal long-term memory library for agents | LLM-judged ADD/UPDATE/DELETE/NONE per-fact (`DEFAULT_UPDATE_MEMORY_PROMPT`); separate `USER_MEMORY_EXTRACTION_PROMPT` vs `AGENT_MEMORY_EXTRACTION_PROMPT`; `PROCEDURAL_MEMORY_SYSTEM_PROMPT` records full agent execution histories verbatim; semantic retrieval via vector store + BM25 hybrid; caller decides recall (`memory.search()` is explicit). No system-prompt auto-injection. |
| `hermes-agent/` | Python | CLI dev-assistant with skill curation | `curator.py` is **skill-lifecycle maintenance** (pin/archive/consolidate/patch agent-created skills via background `auxiliary_client`), inactivity-triggered (last run > 7d, idle > 2h). No tool-failure lesson extraction. `insights.py` is token-cost analytics, not learning. |
| `elysia/` | Python | UI-focused multi-agent | Hits in compiled Next.js bundle only; nothing in Python source. **N/A** for our question. |
| `cli-printing-press/` | Go | Skill/MCP descriptor composition | **N/A** — no self-improvement layer. |
| `nanobot/` | Python | Minimal MCP-host agent | **N/A** — no memory/learning module. |
| `picobot/` | Go | Minimal Telegram agent | **N/A** — no memory/learning module. |
| `recursive-llm/` | Python | Toy recursive-prompt demo | **N/A** — no persistent memory. |
| `aura-agent-loop-papers/` | papers | Academic survey (Feb–May 2026) | SkillX (auto-skill-construction via reflection), Signals paper cites canonical refs: Reflexion (verbal RL → episodic memory), Self-Refine (same-model critique loop), ExpeL (NL insights from training tasks), Voyager (skill library via env feedback). |
| `aura-phase8-research/` | notes | Aura's own swarm research | Multi-agent topology only. No self-improvement notes. |

**One-liner takeaway:** only **openhuman** does end-to-end agent self-improvement comparably to what Aura wants. **mem0** is the canonical memory-CRUD library but ships no auto-recall-into-prompt. Everyone else either delegates to a memory library, does skill-lifecycle (hermes), or has nothing.

---

## Pattern A — Static operational lessons file (Aura's current Phase-OP direction)

### Who does this

- **openhuman** — workspace `MEMORY.md` is injected into the system prompt of the orchestrator (and any agent with `omit_memory_md = false`). Other agents (researcher, planner, critic, code_executor, archivist itself, …) opt out.
  - Injection: `inject_workspace_file_capped(out, workspace_dir, "MEMORY.md", USER_FILE_MAX_CHARS)` in `src/openhuman/agent/prompts/mod.rs:1049`.
  - **Cap** is `USER_FILE_MAX_CHARS` per file.
  - **Session-frozen** semantics — KV-cache contract: the rendered bytes are stable for the lifetime of the session. Any mid-session archivist write lands on the **next** session. This is the explicit anti-Aura-US-OP04 design.
  - Refresh: post-turn `ArchivistHook` (`src/openhuman/agent/harness/archivist.rs`) calls `update_memory_md` tool from a background, low-cost "Archivist" sub-agent (model hint `local`, `temperature=0.4`, `max_iterations=3`, `background = true`). The Archivist's prompt (`agent.toml` + `prompt.md`) is short: "extract lessons, label by category {pattern, mistake, preference, fact}, deduplicate, never log secrets".

### How it surfaces in the system prompt

openhuman puts user-files (`PROFILE.md` + `MEMORY.md`) in a dedicated `PromptSection` that runs **right after** identity/safety preamble. Empty when neither file is opted-in. Each file is wrapped with a heading and capped.

### When it loads

**At session start only** — by design. openhuman trades mid-session freshness for KV-cache stability. Aura's US-OP04 explicitly chooses the opposite trade-off (hot-reload mid-conversation). Both are valid; pick consciously.

### Verdict for Aura

Aura's US-OP03 (top-10 lessons in overlay) maps 1:1 to openhuman's `MEMORY.md` injection. The differences:

- openhuman caps **by bytes** (`USER_FILE_MAX_CHARS`); Aura US-OP03 caps **by rank** (top-10) AND **by total size** (5 KB). Both reasonable; rank is more predictable.
- openhuman uses an LLM (`update_memory_md` tool called by the Archivist sub-agent) for **dedup + deletion**, not just append. Aura's `compact_memory_documents kind=operational ORDER BY updated_at DESC LIMIT 10` is purely recency-based — newer lessons silently bump older ones with no dedup or contradiction-resolution. **This is the first Phase-OP+ gap**.
- openhuman is session-frozen; Aura US-OP04 is hot-reload. Aura's pick is the correct one for daily Telegram (one "session" can last weeks).

---

## Pattern B — Dynamic memory store (Mem0-style)

### Who does this

- **mem0** — the canonical library. Vector store + BM25 hybrid retrieval, LLM-judged write conflicts (ADD/UPDATE/DELETE/NONE per fact).
- **openhuman** — also has this layer via `Memory` trait + `memory_store/query_memory/memory_forget/memory_tree` tools. The Archivist also writes to FTS5 episodic alongside `MEMORY.md`.
- Aura already has it: `compact_memory_documents` + `search_memory`/`recall_operational` tools.

### Storage backend

- mem0: Qdrant/Chroma/Pinecone/etc. (plug-in `VectorStoreFactory`).
- openhuman: SQLite FTS5 for episodic, separate vector store for `Memory` trait. `tool-{name}` namespace per tool.
- Aura: SQLite `compact_memory_documents` with embedding cache.

### Retrieval trigger

- mem0: **explicit** — the agent calls `mem0.search(query, user_id=...)`. The library never auto-injects.
- openhuman: **hybrid** — explicit via `query_memory` tool, AND auto-pinned via `ToolMemoryRulesSection` for Critical/High rules.
- Aura: **hybrid in Phase-OP** — explicit via `recall_operational`, plus US-OP03 auto-injects top-10 into overlay.

### Verdict for Aura

Aura is already mem0-shaped for memory CRUD. Phase-OP doesn't change this. The auto-injection layer matches openhuman's `ToolMemoryRulesSection` more than mem0 (which deliberately stays explicit).

The **specific mem0 idea Aura is missing**: the LLM-judged `DEFAULT_UPDATE_MEMORY_PROMPT` that produces `{event: ADD|UPDATE|DELETE|NONE}` per fact. Aura's propose_patch is bivalent (insert or do-nothing), so contradicting lessons stack up. This is Pattern A's gap from the other angle.

---

## Pattern C — Retrospective / reflection loop

### Who does this

- **openhuman** is the only repo that implements it in code. Two distinct post-turn hooks:
  1. `ArchivistHook` — **heuristic** lesson extraction (no LLM). `extract_lesson_from_tools(tool_calls) -> Option<String>` joins failed tool names. Cheap, always-on.
  2. `ReflectionHook` — **LLM-extracted** structured `ReflectionOutput { observations, patterns, user_preferences, user_reflections }`. Gated by `LearningConfig.reflection_enabled`, throttled at `max_reflections_per_session=20`, requires `tool_count >= min_turn_complexity (=1)` OR response > 500 chars. Local Ollama by default (`ReflectionSource::Local`), cloud fallback if local disabled.
- Reflexion (paper, 2023) — verbal RL stored in episodic memory. Inspired most of the above.
- Self-Refine (paper, 2023) — same model generates feedback then refines itself, no persistence.
- ExpeL (paper) — extract NL insights from training tasks for inference-time use.

### When reflection fires

- openhuman: **end-of-turn**, automatically, on every qualifying turn (≥1 tool call OR ≥500 char response).
- ExpeL: at training time, offline.
- Reflexion: end-of-attempt, in-trajectory.

### What reflection updates

- openhuman: `learning_observations`, `learning_patterns`, `user_profile`, `learning_reflections` namespaces in the `Memory` trait. From there it's recall-on-demand or pinned via `ToolMemoryRulesSection`. **Reflection output never directly rewrites the system prompt** — it goes through the memory layer first, and the system prompt pulls from `MEMORY.md` (curated, session-frozen).

### Verdict for Aura

**Aura has no Pattern C.** Phase-OP US-OP01 only covers the **write-side privilege** (auto-accept propose_patch) — there's no automatic post-turn observation extraction. Today the LLM has to consciously decide "I should propose_patch about this".

**This is the highest-impact Phase-OP+ extension.** openhuman's `extract_lesson_from_tools` is 17 lines of pure Rust, no LLM. Aura could mirror it: after every turn, if any tool failed, append a heuristic "tool X failed with arg Y because Z" to operational store. The richer LLM-driven `ReflectionHook` is a Phase-OP++ option.

---

## Pattern D — Self-modifying prompt / RAG-over-own-history

### Who does this

- **No repo studied does true self-modifying system prompt rewriting**. Closest analogs:
  - openhuman: agents have `omit_*` flags (`omit_identity`, `omit_safety_preamble`, `omit_skills_catalog`, `omit_memory_md`, …) — composable sections, not rewrite.
  - Aura: prompt overlay files (SOUL/AGENT/USER/TOOLS/OPS.md) — read every turn, editable at runtime. Closest the field gets.
- Voyager (paper) — builds a **skill library** of executable code through iterative prompting. The skills are tools, not prompts.
- SkillX (paper, 2026, in `aura-agent-loop-papers`) — automatically constructs a multi-level skill hierarchy (strategic plans → functional skills → atomic skills) via iterative refinement. Skills, not prompts.

### Voyager / SkillX comparison

What they have that Aura doesn't:
- Skills are **iteratively refined** based on execution feedback. Aura's skills (`internal/skills/`) are static — install via `skill_install`, read via `read_skill`, no auto-refine.

### Verdict for Aura

Aura's prompt-overlay system (with US-OP04 hot-reload) is **already more powerful** than what openhuman ships — openhuman explicitly opted out of mid-session prompt mutation for KV-cache reasons. Aura's choice is correct for Telegram daily-use.

**Skill auto-refinement (Voyager/SkillX style) is a separate dimension** — not Phase-OP scope. Park it for a future "Phase-Skills-Adaptive" milestone if/when skill quality becomes a friction point.

---

## Pattern E — Critic / dual-agent self-improvement

### Who does this

- **openhuman** has a `critic` sub-agent (`src/openhuman/agent/agents/critic/`) but it's **code-review oriented** — adversarial reviewer for diffs, runs linter/tests, read-only. **Not** a self-improvement critic in the Reflexion sense.
- **Self-Refine** (paper) — same model critiques + refines its own output before emitting. Single agent, two roles.
- **No repo studied implements "dual-agent self-improvement"** where Agent-A is critiqued by Agent-B and the critique updates persistent memory.

### Verdict for Aura

No clear Phase-OP application. Self-Refine is a **per-turn quality** improvement (refine before emit), distinct from **across-turn learning** (Phase-OP's goal). The two are orthogonal.

If a Pattern E were ever to fit Aura, it would be a "QA critic" sub-agent that reviews completed conversations daily (cron-style, like hermes' curator) and writes a synthesized "agent grading" document to memory. Out of Phase-OP scope.

---

## Recommendations for Aura beyond current Phase-OP

| Pattern | Phase-OP covers it? | Extension priority | 1-line story sketch |
|---------|---------------------|---------------------|---------------------|
| **A — Static lessons file** | **YES** (US-OP02 unified projection + US-OP03 top-10 inject + US-OP04 hot-reload) | **MED** for dedup | US-OP06: LLM-judged ADD/UPDATE/DELETE on propose_patch operational so contradictions resolve, not stack (mem0 `DEFAULT_UPDATE_MEMORY_PROMPT` pattern) |
| **B — Dynamic memory store** | **YES** (compact_memory_documents + recall_operational already exist) | **LOW** | None — Aura is already mem0-shaped here |
| **C — Reflection loop** | **NO** (Aura relies on LLM consciously calling propose_patch) | **HIGH** | US-OP07: post-turn heuristic — if ≥1 tool call failed this turn, auto-append `{tool, arg_keys, error_class}` to operational store (port of openhuman `extract_lesson_from_tools`, 17 lines, no LLM) |
| **C+ — LLM reflection** | **NO** | **MED** (after US-OP07 ships) | US-OP08: optional post-turn LLM call (local embedding model, low cost) extracts `{observation, pattern, user_preference}` JSON, gated by turn complexity ≥ N tools OR response > 500 chars (openhuman `ReflectionHook` pattern) |
| **D — Self-modifying prompt** | **YES, better than peers** (US-OP04 hot-reload overlay) | **LOW** | None — Aura already exceeds openhuman here |
| **E — Critic / dual-agent** | **NO** (not in scope) | **LOW** | Park for future "QA critic" milestone |
| **Cross-cutting** | partial | **MED** | US-OP09: store a `lesson_priority` field (Critical/High/Normal/Low) on operational lessons, render Critical/High in **system prompt always-pin** (openhuman `ToolMemoryRulesSection` pattern), leave Normal/Low for recall-on-demand |

---

## Bottom line

Aura's Phase-OP is well-aligned with the only mature reference implementation in the studied set (openhuman). Specifically, US-OP03 mirrors openhuman's `MEMORY.md` system-prompt injection 1:1 except Aura uses recency-rank (top-10) instead of byte-cap, which is a minor stylistic choice. US-OP04 (hot-reload mid-conversation) is more aggressive than openhuman's session-frozen design, but that's the correct trade-off for a Telegram bot whose "session" can last weeks — KV-cache benefits matter less when the user expects "if I tell her don't do X, she stops doing X in the same conversation". US-OP01 (auto-accept operational) is a private architectural decision openhuman doesn't have a direct equivalent for, because openhuman's Archivist runs as a background agent with write privileges by default — Aura backs into the same place by removing the review gate.

The **single largest gap** Phase-OP leaves on the table is **Pattern C — automatic post-turn reflection**. Every other reference (Reflexion, ExpeL, Voyager, openhuman) treats reflection as a default-on, runs-every-turn behaviour, not as a tool the LLM has to consciously decide to call. Aura today requires the LLM to (a) notice it made a mistake, (b) remember the propose_patch tool exists, and (c) phrase the lesson well — three failure modes openhuman eliminated by making lesson-extraction a `PostTurnHook` outside the LLM's decision surface. The 17-line heuristic version (`extract_lesson_from_tools`: log every turn where any tool failed) is essentially free and should ship as US-OP07 once Phase-OP closes. The richer LLM-driven version (US-OP08) is gated on local-model availability and can wait.

The **second-largest gap** is **dedup/contradiction-resolution** on writes. Mem0's `DEFAULT_UPDATE_MEMORY_PROMPT` produces `{event: ADD|UPDATE|DELETE|NONE}` per fact via LLM judgement so contradicting lessons either merge or supersede — Aura's `propose_patch action=operational` stacks lessons monotonically. After 30 daily conversations, top-10 will be 10 near-duplicates. US-OP06 fixes this.

Everything else is icing. Phase-OP as queued (5 stories) is the right minimum-viable foundation; US-OP06/07/09 are the high-ROI follow-ups that turn Aura from "learns when prompted" into "learns automatically".
