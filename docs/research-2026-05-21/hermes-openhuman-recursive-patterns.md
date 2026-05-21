# Patterns survey — hermes-agent, openhuman, recursive-llm (2026-05-21)

Goal: identify what Aura has NOT yet lifted from D:/tmp/{hermes-agent, openhuman, recursive-llm}. Patterns Aura already adopted (hermes voice-mode, openhuman LLM-driven onboarding pattern, openhuman payload-summarizer concept, openhuman fast-path classifier as anti-pattern) are skipped.

Translatability: 5 = lift verbatim; 1 = paradigm-incompatible.
LOC delta = added Go lines required in Aura if lifted, NOT counting deletes.

---

## TL;DR — Top 3 ROI picks

1. **openhuman TOML AgentDefinition + AgentTier + spawn_subagent / spawn_parallel_agents / spawn_worker_thread** (sections 3, 4, 6). One coherent system. Already on Aura radar as Phase 8 — confirmed shippable in 2-3 sessions. ~600-900 LOC.
2. **openhuman payload_summarizer with circuit-breaker + parent-only wiring + Arc<dyn> hook in tool loop** (section 5). Aura's noted concept "US-P8-G payload summarizer" — concrete API + breaker + scope rule are all here. ~200-300 LOC. Independent of Phase 8 timing.
3. **hermes ContextEngine ABC + ContextCompressor with structured-summary + tool-result pruning + token-budget tail + iterative summary chaining** (section 8). Phase-KV adjacent but orthogonal — addresses agent-loop runtime context, not wiki/RAG. Includes a streaming context scrubber for memory-context tags worth porting if Aura ever leaks system blocks mid-stream. ~400-600 LOC.

Honorable mentions: recursive-llm REPL idea as `read_huge_source` tool (section 1, defer); openhuman `omit_*` prompt-section toggles (section 3); hermes delegation depth + auto-deny callback for child threads (section 7); hermes Anthropic prompt-caching layouts `system_and_3` / `prefix_and_2` (section 9).

---

## 1. recursive-llm — what is it?

**WHAT:** Python implementation of MIT's RLM paper (Zhang & Khattab 2025). Stores huge contexts as a Python variable inside a RestrictedPython REPL and lets the LLM write code to peek/search/recurse over it. The LLM never sees the bytes in its prompt — only `context: str` as a REPL-visible variable, plus `recursive_llm(sub_query, sub_context)` to recurse with a cheaper model.

**WHERE:** `D:/tmp/recursive-llm/src/rlm/`
- `core.py` — `RLM` class, `acomplete` loop, `_make_recursive_fn` recursion
- `prompts.py:4-39` — system prompt is one-shot, ~25 lines
- `repl.py` — RestrictedPython + PrintCollector + 2000-char output truncation, safe-builtin allowlist
- `parser.py` — `FINAL("answer")` / `FINAL_VAR(var_name)` terminal sentinels (interesting echo of Aura's text_response answer-as-argument lift from elysia)

**SNIPPET — recursive loop in `core.py:155-181`:**
```python
for iteration in range(self.max_iterations):
    response = await self._call_llm(messages, **kwargs)
    if is_final(response):
        answer = parse_response(response, repl_env)
        if answer is not None:
            return answer
    try:
        exec_result = self.repl.execute(response, repl_env)
    except REPLError as e:
        exec_result = f"Error: {str(e)}"
    messages.append({"role": "assistant", "content": response})
    messages.append({"role": "user", "content": exec_result})
raise MaxIterationsError(...)
```

**SNIPPET — recursion via cheaper model in `core.py:249-273`:**
```python
async def recursive_llm(sub_query: str, sub_context: str) -> str:
    if self._current_depth + 1 >= self.max_depth:
        return f"Max recursion depth ({self.max_depth}) reached"
    sub_rlm = RLM(
        model=self.recursive_model,
        recursive_model=self.recursive_model,
        ...
        _current_depth=self._current_depth + 1,
    )
    return await sub_rlm.acomplete(sub_query, sub_context)
```

**SNIPPET — prompt is shockingly minimal (`prompts.py:16-37`):**
```
You are a Recursive Language Model. You interact with context through a Python REPL environment.
The context is stored in variable `context` (not in this prompt). Size: {context_size:,} characters.
IMPORTANT: You cannot see the context directly. You MUST write Python code to search and explore it.
Available in environment:
- context: str (the document to analyze)
- query: str (the question)
- recursive_llm(sub_query, sub_context) -> str
- re: already imported regex module
Write Python code to answer the query. The last expression or print() output will be shown to you.
...
CRITICAL: Do NOT guess or make up answers. You MUST search the context first to find the actual information.
Only use FINAL("answer") after you have found concrete evidence in the context.
```

**WHY (for Aura):** Direct applicability is narrow — Aura already has a Python sandbox tool (`execute_code`) and a wiki/search substrate that supplants RLM's whole purpose. BUT three angles are interesting:

1. **`read_huge_source` tool variant.** Aura's `read_source` returns full `ocr.md` files. For 100k+ token sources (ISTAT PDFs, Common Voice manifests, Apache Tika fixtures from the bench plan), Aura could expose a tool that opens the source as `source: str` in the existing Python sandbox and lets the LLM grep/slice. This is RLM-shaped: store-as-variable, search-with-code. Sandbox already exists. ~150 LOC plus 1 tool registration. Defer until a real "PDF too large for context" failure surfaces — Phase-WIKI-B's chunking + rerank may obsolete this entirely.

2. **`FINAL()` terminal sentinel parallel to `text_response`.** Aura's `text_response` answer-as-argument tool (commit 6dc2aac2) does exactly this for a different reason (single-shot bias). RLM independently converged on the pattern. Cross-validates the choice; no action needed.

3. **`max_depth` recursion guard pattern.** Same shape openhuman uses (section 4); see there for the lift.

**EFFORT:** 6-8h spike for `read_huge_source` if scoped narrow. Otherwise: read, file under "defer until concrete need".

**DELTA:** ~150 LOC if pursued. Translatability: **2** (whole paradigm assumes a python-code-writing LLM; Aura's models are tool-calling-shaped — the JSON-tool variant would call grep/slice on a source-id, not write Python).

---

## 2. hermes-agent — agent loop & beyond voice-mode

### 2.1 ContextEngine ABC + plugin slot (HIGH ROI — see section 8)

**WHAT:** Pluggable context-management strategy registered via `config.context.engine` (default `"compressor"`). The agent loop never calls compression directly; it calls `engine.should_compress()` / `engine.compress(messages, ...)` / `engine.update_from_response(usage)`.

**WHERE:** `D:/tmp/hermes-agent/agent/context_engine.py`

**SNIPPET — ABC (lines 32-100):**
```python
class ContextEngine(ABC):
    @property
    @abstractmethod
    def name(self) -> str: ...

    # Engines MUST maintain these. run_agent.py reads them directly.
    last_prompt_tokens: int = 0
    last_completion_tokens: int = 0
    threshold_tokens: int = 0
    context_length: int = 0
    compression_count: int = 0
    threshold_percent: float = 0.75
    protect_first_n: int = 3
    protect_last_n: int = 6

    @abstractmethod
    def update_from_response(self, usage: Dict[str, Any]) -> None: ...

    @abstractmethod
    def should_compress(self, prompt_tokens: int = None) -> bool: ...

    @abstractmethod
    def compress(self, messages: List[Dict[str, Any]],
                 current_tokens: int = None,
                 focus_topic: str = None,
                 ) -> List[Dict[str, Any]]: ...
```

**WHY:** Aura's `internal/conversation` currently does message-window-cap-by-count (default 50). When Phase-COMP / TokenJuice / KV-byte-faithful work lands, it needs a clean seam to compress mid-conversation. This ABC is the right shape: stateless interface, engine owns its own counters, agent loop is one branch (`if engine.should_compress() { messages = engine.compress(messages) }`). Maps directly to Go interface in `internal/conversation/engine.go`.

**EFFORT:** Half-day. Define `type ContextEngine interface` in Go, plumb into `internal/chat/agentloop.go` at the existing window-cap site. Default impl = the current behavior wrapped.

**DELTA:** ~80 LOC (interface + default wrapper). Translatability: **5** — straight port.

### 2.2 ContextCompressor — structured summary template (see section 8)

### 2.3 trajectory_compressor — POST-RUN compression for training-data (skip)

**WHAT:** Post-processes completed agent trajectories to compress them to a target token budget. Used for RL/training data, not runtime. Out of Aura scope (Aura is not collecting training trajectories).

**EFFORT/DELTA:** N/A. **Translatability: 1.**

### 2.4 Insights engine (SQLite-driven usage report)

**WHAT:** `agent/insights.py:1-30` — analyzes SQLite session history to produce token/cost/tool-usage reports modeled on Claude Code's `/insights`. Adapted for multi-platform with cost estimation + platform breakdown.

**WHY:** Aura has `internal/api/maintenance` and `CONV_ARCHIVE_ENABLED=true` archives. A `/api/insights?days=30` endpoint surfacing token spend per model + tool-call counts per tool + per-channel cost would be a 2-3h add on top of the existing archive. Light dashboard widget would close the "is Aura cost-controlled" gap in the product narrative.

**EFFORT:** 1 day end-to-end (Go service + dashboard widget). 

**DELTA:** ~250 LOC + ~150 React. Translatability: **4** — schema differs, concept clean.

### 2.5 Hermes-style auxiliary client for summarization

**WHAT:** `agent/auxiliary_client.py` — a SEPARATE LLM client (cheaper model) used only for compression / summarization. Already a pattern Aura half-uses (separate embedding endpoint). Two-tier client (primary + auxiliary) is documented in `context_compressor.py:26` (`from agent.auxiliary_client import call_llm`).

**WHY:** Aura already calls one LLM endpoint. A second cheap-and-fast client for: payload summarization, wiki-write LLM (currently uses main LLM at temp=0), title generation. Would let users pin Sonnet for chat + Haiku for housekeeping without code changes.

**EFFORT:** 2h — add `AUX_LLM_BASE_URL` / `AUX_LLM_API_KEY` envs + a second `llm.Client`. Wire one call site (wiki-write) as proof.

**DELTA:** ~60 LOC. Translatability: **5**.

---

## 3. openhuman AgentDefinition TOML registry — concrete structure

**WHAT:** Each agent is `agent.toml` + `prompt.md` (+ optional `prompt.rs` for `Dynamic` builders) under `src/openhuman/agent/agents/<id>/`. Custom user-overrides live in `$WORKSPACE/agents/*.toml` and `~/.openhuman/agents/*.toml`, overriding builtins on id collision. Registry singleton `AgentDefinitionRegistry::GLOBAL` initialized at boot.

**WHERE:** 
- `D:/tmp/openhuman/src/openhuman/agent/harness/definition.rs:36-214` — `AgentDefinition` struct
- `D:/tmp/openhuman/src/openhuman/agent/harness/definition.rs:528-664` — `AgentDefinitionRegistry`
- `D:/tmp/openhuman/src/openhuman/agent/harness/definition_loader.rs` — `load_from_workspace`
- `D:/tmp/openhuman/src/openhuman/agent/agents/*/agent.toml` — 17 built-in agents
- `D:/tmp/openhuman/src/openhuman/agent/agents/loader.rs:184-240` — `validate_tier_hierarchy`

**SNIPPET — orchestrator.toml (verbatim, the canonical example):**
```toml
id = "orchestrator"
display_name = "Orchestrator"
when_to_use = "Staff Engineer — routes, judges quality, synthesises..."
temperature = 0.4
max_iterations = 15
sandbox_mode = "none"
agent_tier = "chat"
omit_identity = true
omit_memory_context = true
omit_safety_preamble = true
omit_skills_catalog = true
omit_profile = false
omit_memory_md = false

subagents = [
    "researcher", "planner", "code_executor", "tools_agent",
    "skill_creator", "critic", "archivist", "crypto_agent",
    { skills = "*" },   # wildcard expands to one delegate_to_integrations_agent per connected toolkit
]

[model]
hint = "chat"

[tools]
named = [
    "query_memory", "memory_store", "memory_forget", "memory_tree",
    "whatsapp_data_list_chats", "whatsapp_data_list_messages", "whatsapp_data_search_messages",
    "read_workspace_state", "ask_user_clarification",
    "spawn_worker_thread", "spawn_parallel_agents",
    "composio_list_connections",
    "current_time", "cron_add", "cron_list", "cron_remove",
    "todowrite", "plan_exit",
    "update_check", "update_apply",
]
```

**SNIPPET — researcher.toml (worker example):**
```toml
id = "researcher"
display_name = "Researcher"
delegate_name = "research"
when_to_use = "Web & docs crawler..."
temperature = 0.4
max_iterations = 8
max_result_chars = 8000
sandbox_mode = "none"
omit_identity = true
omit_memory_context = true
omit_safety_preamble = true
omit_skills_catalog = true
[model]
hint = "agentic"
[tools]
named = ["http_request", "curl", "web_search", "file_read", "web_fetch", "grep", "glob", "list", "memory_recall", ...]
```

### Notable fields:

- `id`, `display_name`, `when_to_use` (LLM-visible description when this agent appears as a delegate tool), `delegate_name` (override the `delegate_{id}` default).
- `temperature`, `max_iterations` (default 8 — `defaults::max_iterations` in `definition.rs:506`), `max_result_chars` (truncates THIS agent's output before it feeds back to parent — researcher caps at 8000), `timeout_secs`.
- **8 `omit_*` flags** for prompt-section stripping: `omit_identity`, `omit_memory_context`, `omit_safety_preamble`, `omit_skills_catalog`, `omit_profile`, `omit_memory_md`. Default `true` — narrow specialists are lean by default; only user-facing agents (welcome, orchestrator) opt back in with `false`.
- `model = { hint = "..."}` or `{ exact = "..." }` or default `inherit`. Hint resolves to `{hint}-v1`.
- `tools = { wildcard }` or `{ named = [...] }`; `disallowed_tools`, `skill_filter`, `extra_tools`.
- `subagents = [...]` — what this agent can delegate to (separate from `tools`).
- `agent_tier = "chat" | "reasoning" | "worker"` (default `worker`).
- `sandbox_mode = "none" | "read_only" | "sandboxed"`.

**WHY (for Aura):** Aura's `internal/agent` is monolithic — one prompt, one tool set, one model. This TOML format is the cleanest "spawn a focused sub-agent for X" contract I've seen. Three concrete payoffs:

1. **Skill author surface.** Today `internal/skills` exposes `SKILL.md` (prompt fragment + manifest). Adding an `AGENT.md` shape (one section per agent definition, parsed to `AgentDefinition`) lets users ship `researcher.toml`, `summarizer.toml`, etc. as workspace files without touching Go.
2. **Per-agent tool allowlist.** Right now every tool is visible to every conversation. A summarizer doesn't need `schedule_task`. Lean tool surfaces lower token cost (system prompt shrinks) AND lower the agent's chance to misroute.
3. **Per-agent model.** Wiki-write currently uses the main LLM at temp=0. With AgentDefinition, wiki-write becomes a "writer" agent with `model.hint = "deterministic"`, `temperature = 0`, `omit_*` everything-but-skill-catalog.

**EFFORT:** 2-3 days for the registry + loader + 3-4 builtin agents (researcher, summarizer, wiki_writer, orchestrator). Phase 8 already scoped for this.

**DELTA:** ~600 LOC (definition struct + TOML parser + registry + loader + 3-4 builtins). Translatability: **5** — port to Go uses `github.com/BurntSushi/toml`.

---

## 4. openhuman MaxDepth + AgentTier (concrete code)

**WHAT:** Two separate gates form the spawn-hierarchy contract:
- **Static (loader-time)** — `validate_tier_hierarchy(defs)` runs at boot AND after workspace TOML overrides. Rejects same-tier delegation (chat→chat, reasoning→reasoning) and any subagent on a worker.
- **Dynamic (runtime, planned)** — `MAX_SPAWN_DEPTH = 3` task-local. Documented in `definition.rs:203` but not yet implemented (see `harness_gap_tests.rs:16` — "Spawn-depth gate (SpawnDepthExceeded) — no depth counter or variant exists.").

**WHERE:** `D:/tmp/openhuman/src/openhuman/agent/agents/loader.rs:184-240`

**SNIPPET — full validator:**
```rust
pub fn validate_tier_hierarchy(defs: &[AgentDefinition]) -> Result<()> {
    let tier_by_id: HashMap<&str, AgentTier> =
        defs.iter().map(|d| (d.id.as_str(), d.agent_tier)).collect();
    for def in defs {
        for entry in &def.subagents {
            let child_id = match entry {
                SubagentEntry::AgentId(id) => id.as_str(),
                SubagentEntry::Skills(_) => continue, // wildcards always → integrations_agent (Worker)
            };
            if def.agent_tier == AgentTier::Worker {
                anyhow::bail!("agent `{parent}` is a `worker` tier and must not list `{child}` ...",
                    parent = def.id, child = child_id);
            }
            let Some(child_tier) = tier_by_id.get(child_id).copied() else { continue };
            match (def.agent_tier, child_tier) {
                (AgentTier::Chat, AgentTier::Chat) => anyhow::bail!(
                    "agent `{parent}` (chat) lists `{child}` (chat) in subagents — the chat tier \
                     is a leaf in its own dimension. ...", parent = def.id, child = child_id),
                (AgentTier::Reasoning, AgentTier::Reasoning) => anyhow::bail!(
                    "agent `{parent}` (reasoning) lists `{child}` (reasoning) in subagents — \
                     reasoning agents compose downward into workers, not into each other.",
                    parent = def.id, child = child_id),
                _ => {}
            }
        }
    }
    Ok(())
}
```

**SNIPPET — AgentTier enum (`definition.rs:236-265`):**
```rust
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum AgentTier {
    Chat,      // user-facing fast tier; routes; never spawns Chat
    Reasoning, // deep-thinking; decomposes; spawns Workers; never spawns Reasoning
    #[default]
    Worker,    // leaf executor; NEVER spawns anything
}
```

Hierarchy is `chat → reasoning → worker` (or `chat → worker` fast path), capped at MAX_SPAWN_DEPTH=3.

**WHY:** Even without TOML registry, this 3-tier classification + static validator is the cheapest way to guarantee "Aura's orchestrator never delegates to another orchestrator". Defense in depth: model can't loop, the loader rejects it.

**EFFORT:** Half-day once `AgentDefinition` exists in Aura (depends on section 3).

**DELTA:** ~80 LOC. Translatability: **5** — direct.

### Hermes parallel — delegate_tool.py

**WHERE:** `D:/tmp/hermes-agent/tools/delegate_tool.py:128-132, 389-424`

**SNIPPET:**
```python
MAX_DEPTH = 1  # flat by default: parent (0) -> child (1); grandchild rejected unless max_spawn_depth raised.
_MIN_SPAWN_DEPTH = 1
_MAX_SPAWN_DEPTH_CAP = 3

def _get_max_spawn_depth() -> int:
    cfg = _load_config()
    val = cfg.get("max_spawn_depth")
    if val is None: return MAX_DEPTH
    try: ival = int(val)
    except: return MAX_DEPTH
    clamped = max(_MIN_SPAWN_DEPTH, min(_MAX_SPAWN_DEPTH_CAP, ival))
    return clamped
```

Hermes also enforces a **subagent auto-deny callback** so child threads never hit a parent TUI deadlock on `input()`:

```python
def _subagent_auto_deny(command: str, description: str, **kwargs) -> str:
    logger.warning("Subagent auto-denied dangerous command: %s (%s)...")
    return "deny"
```

**WHY (for Aura):** if Aura ever runs spawn_subagent in a goroutine that needs `request_dashboard_token` (out-of-band approval), the child must NOT block waiting for user — auto-deny by default with an opt-in `subagent_auto_approve: true` flag is the right escape valve. Take this. ~30 LOC.

---

## 5. openhuman payload_summarizer (Aura's noted US-P8-G)

**WHAT:** A trait + struct that intercepts oversized tool results BEFORE they enter agent history. Dispatches a `summarizer` sub-agent (model hint `summarization`) that compresses per an extraction contract. Three pass-through guards: below threshold, above max cap, circuit breaker (3 consecutive failures → disabled for session). Wired ONLY into the orchestrator session — every other agent gets `None`.

**WHERE:**
- `D:/tmp/openhuman/src/openhuman/agent/harness/payload_summarizer.rs` (487 lines, single file)
- Wiring: `D:/tmp/openhuman/src/openhuman/agent/harness/session/builder.rs:1245-1331`
- Tool-loop hook: `D:/tmp/openhuman/src/openhuman/agent/harness/session/turn.rs:998-1037`

**SNIPPET — trait (lines 89-107):**
```rust
#[async_trait]
pub trait PayloadSummarizer: Send + Sync {
    async fn maybe_summarize(
        &self,
        tool_name: &str,
        parent_task_hint: Option<&str>,
        raw: &str,
    ) -> Result<Option<SummarizedPayload>>;
}
```

**SNIPPET — wiring (orchestrator-only, builder.rs:1261-1298):**
```rust
let payload_summarizer = if agent_id == "orchestrator"
    && config.context.summarizer_payload_threshold_tokens > 0
{
    match AgentDefinitionRegistry::global() {
        Some(reg) => match reg.get("summarizer") {
            Some(summarizer_def) => {
                Some(Arc::new(SubagentPayloadSummarizer::new(
                    summarizer_def.clone(),
                    config.context.summarizer_payload_threshold_tokens,
                    config.context.summarizer_max_payload_tokens,
                )))
            }
            None => None,
        },
        None => None,
    }
} else { None };
```

**SNIPPET — pass-through guards (payload_summarizer.rs:198-238):**
```rust
let tokens = estimate_tokens(raw);
if tokens < self.threshold_tokens { return Ok(None); }          // below threshold
if tokens > self.max_payload_tokens { return Ok(None); }        // above cap — let truncation handle
if self.breaker_tripped() { return Ok(None); }                  // 3+ failures
// dispatch summarizer sub-agent ...
```

**SNIPPET — circuit breaker (lines 167-194):**
```rust
fn breaker_tripped(&self) -> bool {
    match self.failures.lock() {
        Ok(g) => *g >= self.max_failures_before_disable,
        Err(_) => true, // poisoned mutex = something panicked → fail safe
    }
}
fn record_failure(&self) {
    if let Ok(mut g) = self.failures.lock() {
        *g = g.saturating_add(1);
        if *g == self.max_failures_before_disable {
            warn!("[payload_summarizer] circuit breaker tripped after {} consecutive failures",
                  self.max_failures_before_disable);
        }
    }
}
fn record_success(&self) {
    if let Ok(mut g) = self.failures.lock() { *g = 0; }
}
```

**SNIPPET — hook in tool loop (turn.rs:1006-1037):**
```rust
if let Some(ps) = self.payload_summarizer.as_ref() {
    match ps.maybe_summarize(&call.name, None, &output).await {
        Ok(Some(payload)) => {
            log::info!("[agent_loop] payload_summarizer compressed tool={} {}->{} bytes",
                       call.name, payload.original_bytes, payload.summary_bytes);
            output = payload.summary;
        }
        Ok(None) => { /* pass through, log debug */ }
        Err(e) => {
            log::warn!("[agent_loop] payload_summarizer error tool={} err={} (passing raw payload through)",
                       call.name, e);
        }
    }
}
```

**SNIPPET — prompt-builder marker convention (lines 326-334):**
```rust
fn build_summarizer_prompt(tool_name: &str, parent_task_hint: Option<&str>, raw: &str) -> String {
    let hint_line = parent_task_hint
        .map(|h| format!("Parent task hint: {}\n\n", h))
        .unwrap_or_default();
    format!(
        "Tool name: {}\n\n{}Raw tool output (summarize per the extraction contract in your system prompt):\n\n--- BEGIN ---\n{}\n--- END ---",
        tool_name, hint_line, raw
    )
}
```

**WHY (for Aura):** Aura has `tool_result_budget_bytes` truncation but no compression. After Phase-WIKI-B lands and search returns multi-KB top-k blocks, the orchestrator turn will burn context on every retrieval. This pattern is the right shape:

- **Trait, not branch.** Hookable from `internal/agent` without touching tools.
- **Circuit breaker.** Aura ships to mini-PC; a broken summarizer model would tank EVERY tool call without it.
- **Failure = pass-through, never break.** Aligns with Aura's "boot non-fatal" convention.
- **`summary >= raw` rejection** (`payload_summarizer.rs:266-275`). If the summarizer makes things worse, don't replace.
- **Parent-only scope.** Sub-agents (researcher, wiki_writer) keep raw payloads; only the orchestrator sees compressed.
- **Token estimate `chars/4`** is fine for Aura too (model-agnostic).

**EFFORT:** 1.5 days. Independent of section 3 if you stub the "summarizer agent" as a direct LLM call. Cleaner with section 3.

**DELTA:** ~300 LOC (trait + default impl + circuit breaker + wiring + tests). Translatability: **5**.

### Note on openhuman's CURRENT state

```
NOTE: `summarizer` used to be listed here for the runtime-only
oversized-tool-result hook. That path is currently disabled
(`context.summarizer_payload_threshold_tokens = 0`) after recursive
dispatch was observed.
```

— `orchestrator/agent.toml:60-68`. Openhuman observed recursive dispatch (summarizer calling itself on a huge payload) and disabled the threshold. Lesson for Aura: the summarizer agent's own `payload_summarizer` MUST be `None`, AND the thresholds must be carefully chosen so the summarizer's OUTPUT (which becomes a tool result for the orchestrator on the next turn) doesn't itself exceed the threshold. The orchestrator-only check is correct but doesn't fully prevent re-entry on chained orchestrators. **Aura should ship with `payload_summarizer = None` for the summarizer agent definition itself, and threshold > expected summary output size.**

---

## 6. Multi-agent / delegation patterns

### openhuman has 3 distinct spawn primitives:

1. **`spawn_subagent` (typed)** — `D:/tmp/openhuman/src/openhuman/agent/harness/subagent_runner/mod.rs:38-46`. Single sub-agent, collapses to one tool result in parent history. Loop runs intra-sub-agent; transcript never leaks.

2. **`spawn_parallel_agents`** — `D:/tmp/openhuman/src/openhuman/tools/impl/agent/spawn_parallel_agents.rs`. Fans out 2+ independent tasks concurrently. Required `ownership` field per task ("Disjoint file/module/responsibility boundary for this worker") — forces the LLM to declare non-overlap or get rejected.

**SNIPPET — parallel schema (spawn_parallel_agents.rs:78-101):**
```rust
json!({
    "type": "object",
    "required": ["tasks"],
    "properties": {
        "tasks": {
            "type": "array",
            "minItems": 2,
            "items": {
                "type": "object",
                "required": ["agent_id", "prompt"],
                "properties": {
                    "agent_id": agent_id_schema,
                    "prompt": { "type": "string" },
                    "context": { "type": "string" },
                    "toolkit": { "type": "string" },
                    "ownership": {
                        "type": "string",
                        "description": "Disjoint file/module/responsibility boundary for this worker."
                    }
                }
            }
        }
    }
})
```

Concurrency cap is `parent.agent_config.max_parallel_tools.max(2)` — defaults from the parent's config.

3. **`spawn_worker_thread`** — `D:/tmp/openhuman/src/openhuman/tools/impl/agent/spawn_worker_thread.rs`. Creates a NEW persisted conversation thread labeled `worker`. Parent receives only `{thread_id, summary}`. Worker thread CANNOT spawn another worker thread.

**SNIPPET (spawn_worker_thread.rs:1-11):**
```rust
//! Unlike `spawn_subagent`, which collapses sub-agent work into a single
//! tool result in the current thread, `spawn_worker_thread` creates a new
//! persisted thread with label `worker`. The sub-agent's full transcript
//! is recorded into that thread, and the parent receives a compact
//! reference (worker thread id) instead of the full output.
//!
//! Worker threads carry a hard cap on depth: a worker thread cannot spawn
//! another worker thread.
```

**WHY:** Aura's `internal/skills` and `internal/wiki` could benefit from all three:

- `spawn_subagent` — the wiki-writer becomes a sub-agent invocation, not a separate code path. Same loop, same registry, narrower tools + temp=0.
- `spawn_parallel_agents` — research-style "go fetch 3 different sources and synthesize" tasks. Bench data shows the user issues "compare X, Y, Z" queries often. Forced `ownership` field prevents two parallel agents from writing the same wiki page.
- `spawn_worker_thread` — Aura's `scheduled_tasks` are roughly this shape today (a background goroutine with its own message log). Reframing scheduled tasks as worker threads + giving the LLM a `spawn_worker_thread` tool lets users say "kick this off but don't make me wait" via tool calls instead of cron syntax.

**EFFORT:** 3-5 days for all three. spawn_subagent is the foundation; the other two compose on top.

**DELTA:** ~500 LOC total across the three tools + runner.

### hermes parallels:

- **Subagent registry + interrupt** — `delegate_tool.py:144-216`. `_active_subagents: Dict[subagent_id, record]` so the TUI can list + interrupt running children. Pattern: every spawned child registers, deregisters on completion. Aura could lift the registry for `/api/agents/active` dashboard endpoint.
- **Subagent approval callbacks via ThreadPoolExecutor initializer** — `delegate_tool.py:67-92`. Worker threads don't inherit threading.local() state; explicit `initializer=_set_subagent_approval_cb` plants a non-interactive callback. Aura's goroutines don't have this exact problem, but the principle (child cannot block on user input) applies.

---

## 7. Memory / state persistence

### openhuman

- **Agent definitions** — TOML on disk (`$WORKSPACE/agents/*.toml` + `~/.openhuman/agents/*.toml`), loaded once at boot into `AgentDefinitionRegistry::GLOBAL` (`OnceLock`).
- **Worker thread transcripts** — SQLite, table `conversations` (label = `"worker"`), see `spawn_worker_thread.rs:182-194` writing via `CreateConversationThread`. Parent message references thread_id; LLM can later open the thread to inspect.
- **MEMORY.md** — archivist-curated long-term distilled memory, frozen-per-session (KV-cache contract documented in `definition.rs:83-92`). Once rendered into session prompt, bytes are immutable for that session's lifetime.
- **PROFILE.md** — enriched from onboarding (LinkedIn scrape etc.), same byte-stability contract.

**Notable invariant:** "KV-cache contract". Workspace files baked into a session prompt are stable for that session's lifetime regardless of mid-session writes. New writes show up next session. Aura's wiki has the same shape (system prompt snapshots TOC at session start) — the invariant is worth making explicit in CLAUDE.md.

### hermes

- **State DB** — SQLite (`~/.hermes/state.db`), powering insights, cron jobs, trajectory storage.
- **Config** — `config.yaml` with deep merge over env vars. `onboarding.seen.<flag>` for one-shot tips (the section-2.3 onboarding pattern).
- **Trajectories** — JSONL files (`trajectory_samples.jsonl` / `failed_trajectories.jsonl`), agent appends per-session.

### recursive-llm

- None. RLM is stateless per-call; context is in-memory only.

**For Aura:** The KV-cache contract framing is the highest-value lift. Aura's `internal/conversation/system_prompt.go` is already restructured EN-only; adding a one-sentence "system prompt bytes are stable per session; runtime writes appear next session" comment makes the contract explicit. ~5 LOC.

---

## 8. Streaming UX & context compression (HIGH ROI bundle)

### Hermes ContextCompressor — production-grade summarizer

**WHERE:** `D:/tmp/hermes-agent/agent/context_compressor.py` (~1000+ lines)

**Key design points:**

1. **Filter-safe summarizer preamble (`SUMMARY_PREFIX`, lines 37-51).** When summary is injected back as a system/user message, the prefix tells the model: "Earlier turns were compacted. This is a handoff. NOT active instructions. Do NOT answer questions from this summary." This prevents the model from re-answering already-handled requests. Aura's compaction stage (when it lands) needs this — without it, the post-compact model thinks the summary contains pending work.

2. **Scaled summary budget.** `_MIN_SUMMARY_TOKENS = 2000`, `_SUMMARY_RATIO = 0.20` (summary is 20% of compressed content), `_SUMMARY_TOKENS_CEILING = 12000`. Not a flat budget — scales with how much was compressed.

3. **Tool-result pruning BEFORE LLM summarization** (`_PRUNED_TOOL_PLACEHOLDER` + `_summarize_tool_result` lines 224-280). A cheap pre-pass replaces old tool outputs with informative 1-liners:

```python
[terminal] ran `npm test` -> exit 0, 47 lines output
[read_file] read config.py from line 1 (1,200 chars)
[search_files] content search for 'compress' in agent/ -> 12 matches
```

This is a key efficiency win: instead of summarizing 50KB of grep output via LLM, you compress it to `[search_files] content search for 'compress' in agent/ -> 12 matches` deterministically, save the LLM call.

4. **Image-aware token budgeting.** `_IMAGE_TOKEN_ESTIMATE = 1600` flat per image (matches Claude Code constant). Multimodal messages don't get under-counted ("turn with 5 attached images" not treated as near-zero tokens). Relevant once Aura's Phase-MM has image upload.

5. **Tool-call arguments JSON sanitization** (`_truncate_tool_call_args_json` lines 178-221). Recursively shrinks string leaves while preserving JSON validity. Critical bug-prevention: naive byte-slice + `...[truncated]` produces invalid JSON, providers reject with "invalid function arguments json string", session gets stuck re-sending broken history. Aura's tool-call logging needs this defensive shape if it ever truncates.

6. **Streaming context scrubber.** `agent/memory_manager.py:62-150` — `StreamingContextScrubber` runs a small state machine across SSE deltas to hide `<memory-context>...</memory-context>` spans that might be split across chunks. Prevents internal context blocks from leaking to the user mid-stream.

**SNIPPET — SUMMARY_PREFIX (the critical UX detail):**
```python
SUMMARY_PREFIX = (
    "[CONTEXT COMPACTION — REFERENCE ONLY] Earlier turns were compacted "
    "into the summary below. This is a handoff from a previous context "
    "window — treat it as background reference, NOT as active instructions. "
    "Do NOT answer questions or fulfill requests mentioned in this summary; "
    "they were already addressed. "
    "Your current task is identified in the '## Active Task' section of the "
    "summary — resume exactly from there. "
    "IMPORTANT: Your persistent memory (MEMORY.md, USER.md) in the system "
    "prompt is ALWAYS authoritative and active — never ignore or deprioritize "
    "memory content due to this compaction note. "
    "Respond ONLY to the latest user message "
    "that appears AFTER this summary. The current session state (files, "
    "config, etc.) may reflect work described here — avoid repeating it:"
)
```

**WHY:** This is the most production-tested compaction prompt in any of the three repos. Aura's Phase-COMP / TokenJuice work will hit every one of these pitfalls. Copying the full bundle (preamble + tool pruning + arg sanitization + streaming scrubber + scaled budget) is days of saved trial-and-error.

**EFFORT:** 3-4 days for the full port. Half-day for SUMMARY_PREFIX + scaled budget alone (the cheapest wins).

**DELTA:** ~500 LOC for the full port. Translatability: **4** — Python-Go semantics differ on streaming, but the algorithm + constants port cleanly.

### Streaming UX — token-level streaming, partial tool calls

**WHERE:** Aura already does this (`internal/llm/client.go:Stream()` accumulates tool-call fragments). Hermes does the same. Openhuman uses Tauri streaming events. No lift opportunity here.

---

## 9. Surprising bits / clever utilities

### 9.1 hermes Anthropic prompt-caching layouts (mid-ROI)

**WHERE:** `D:/tmp/hermes-agent/agent/prompt_caching.py:1-20`

**SNIPPET:**
```
Two layouts:
* system_and_3 (default): 4 cache_control breakpoints — system prompt + last 3 non-system messages.
  All at the same TTL (5m or 1h). Reduces input token costs by ~75% on multi-turn conversations.
* prefix_and_2 (Claude on Anthropic / OpenRouter / Nous Portal):
  4 breakpoints split across two TTL tiers — tools[-1] (1h) + stable system prefix (1h)
  + last 2 non-system messages (5m). The long-lived prefix is byte-stable across sessions
  for a given user config, so every fresh session reads the cached system+tools instead
  of re-paying for them.
```

**WHY:** Aura uses OpenAI-compatible HTTP only. If Aura ever proxies Anthropic models (currently does not but is on the platform roadmap), this code is the right shape. Currently: **defer**.

### 9.2 openhuman `defaults::true_` shorthand

Tiny but pleasant: 
```rust
pub(crate) fn true_() -> bool { true }
#[serde(default = "defaults::true_")]
pub omit_identity: bool,
```

Saves 8 omit-flag boilerplate per agent definition. Direct Go equivalent uses pointer-to-bool or an `OmitFlags` struct with `IsZero()` mapping; less clean than serde's. **No action, just a flavor note.**

### 9.3 hermes "ownership" required-field on parallel tasks

```rust
"ownership": {
    "type": "string",
    "description": "Disjoint file/module/responsibility boundary for this worker."
}
```

Forcing the LLM to declare non-overlap in the function-calling schema is more reliable than telling it in the system prompt. If two parallel children both write to `wiki/users.md`, you've got a race. Schema-level forcing is the cheapest fix. **Take this when adding parallel-spawn (~5 LOC).**

### 9.4 recursive-llm `FINAL_VAR(varname)` 

Sentinel that returns the value of a REPL variable as the final answer, not a string literal. Avoids re-serializing large structured outputs through string-passing:

```python
match = re.search(r'FINAL_VAR\s*\(\s*(\w+)\s*\)', response)
if not match: return None
var_name = match.group(1)
if var_name in env:
    return str(env[var_name])
```

Variant of Aura's `text_response` answer-as-argument that returns a structured-result reference instead of a string. Could apply to Aura tool calls that produce large JSON: instead of streaming the full JSON back, return `{"ref": "task_12345_result", "preview": "...first 500 chars..."}` and let later tool calls dereference. **Defer — needs concrete use case.**

### 9.5 openhuman PromptSource three variants

```rust
pub enum PromptSource {
    Inline(String),
    File { path: String },
    Dynamic(PromptBuilder),  // fn(&PromptContext) -> Result<String>
}
```

`Dynamic` is the killer — built-in agents register a function pointer that builds the prompt from runtime state (tools, skills, profile, memory). User-shipped TOML agents are stuck with `Inline` or `File`. Aura's prompt overlay system (SOUL.md, AGENT.md, USER.md, TOOLS.md re-read every turn) is the `File` variant. Adding a `Dynamic` slot for built-in Go agents lets specific agents (wiki_writer, summarizer) build their prompts from current toolset / current wiki TOC without going through the overlay file pipeline. **Defer until section 3 lands.**

### 9.6 hermes per-platform tool config

`hermes-agent/cron/scheduler.py:58-86` — `_resolve_cron_enabled_toolsets` reads per-platform toolset overrides. cron jobs can have a SMALLER toolset than interactive sessions. Reasoning:

```
_DEFAULT_OFF_TOOLSETS ({moa, homeassistant, rl}) are removed by
_get_platform_tools for unconfigured platforms, so fresh installs
get cron WITHOUT moa by default (issue reported by Norbert —
surprise $4.63 run).
```

A misconfigured cron job hit a $4.63/run model — defaults-off for expensive tools. **For Aura:** scheduled_tasks could similarly have a default-disabled list (e.g. `web_search` capped at 5 calls per task). 1h add. ~30 LOC.

---

## Lifted-already check (skipped in body)

- Hermes voice-mode pattern (off/voice_only/all) — already in Wave 3 plan, see memory `reference_hermes_voice_mode_pattern.md`.
- Openhuman LLM-driven self-onboarding pattern — already in `docs/onboarding-research/` + memory `reference_phase_onb_design_2026-05-20.md`.
- Openhuman payload_summarizer concept (section 5 above provides the concrete code reference) — was already noted as US-P8-G but never given the wiring sketch; that's why it's in the top-3.
- Openhuman fast-path classifier — already confirmed anti-pattern.

---

## Sequencing recommendation

If picking ONE thing now, take section 5 (payload_summarizer). It is independent of Phase 8 timing, has a concrete API contract, and immediately improves orchestrator context efficiency once Phase-WIKI-B starts returning multi-KB retrieval blocks. Bake it BEFORE Phase-RAG ships large search results — easier to plug in early than retrofit after token spend regression.

If picking TWO, add section 8 (hermes ContextEngine ABC + SUMMARY_PREFIX + tool-result pruning). Phase-COMP / TokenJuice work needs this seam regardless of when it lands; the constants and the preamble are mature.

Section 3+4 (TOML AgentDefinition + tier validator) is the highest LOC delta but the highest ceiling — every other pattern composes cleaner on top of it. Phase 8 is the right home.
