# openhuman PostTurnHook + priority-pinning — porting research for Aura US-OP07 + US-OP09

Research date: 2026-05-19
Trigger: Phase-OP+ planning — close the "LLM-decision-gated learning" gap identified in [`docs/self-improvement-patterns-2026-05-18.md`](self-improvement-patterns-2026-05-18.md) §Pattern C.
Subject codebase: openhuman Rust agent harness at `D:/tmp/openhuman/` (GPLv3; concepts-only port).

## TL;DR

- **US-OP07 (heuristic post-turn lesson)** — openhuman's `extract_lesson_from_tools` is a 17-line pure function that walks `ctx.tool_calls`, filters by `!success`, and returns a one-line lesson string. It is wired through a generic `PostTurnHook` trait dispatched via `fire_hooks` *after* the agent has produced its final response. Hooks run via `tokio::spawn` — fully off the critical path. Aura can attach an analogous hook in `internal/agent/loop.go` after `executor.go` returns the assistant turn, before the next user message arrives.
- **US-OP09 (always-pin Critical/High lessons)** — openhuman has a 3-level `ToolMemoryPriority` enum (`Normal`/`High`/`Critical`) on a `ToolMemoryRule` struct stored in a per-tool namespace `tool-{name}`. `rules_for_prompt()` pre-fetches only eager (Critical+High) rules, capped at 30 total, Critical-first. `ToolMemoryRulesSection` is a `PromptSection` that **bakes the rendered string at construction** so the system prompt stays byte-identical for the whole session (KV-cache contract). Compression-resistance comes entirely from "lives in system prompt, never rewritten mid-session", not from any special elision logic.
- **Two hooks, two priorities** — openhuman ships *two* separate `PostTurnHook` impls: `ArchivistHook` (heuristic; failed-tool lesson → episodic FTS5 row) and `ToolMemoryCaptureHook` (regex-on-user-message → Critical rule + repeated-failure tally → Normal rule). They write to different stores. Aura's port should keep this separation: US-OP07 = the failure-heuristic; US-OP09 = the priority schema + always-pin renderer.
- **All-on by default, config-gated** — `LearningConfig.enabled` is the master switch; sub-flags (`tool_memory_capture_enabled`, `reflection_enabled`, `chat_to_tree_enabled`) default to `true` when learning is on. No heuristic gating beyond config.
- **Empty-state policy** — `ToolMemoryRulesSection::new(vec![])` renders the empty string; the section is silently dropped from the prompt when no eager rules exist. No "no rules yet" placeholder.

---

## Part A — US-OP07 (extract_lesson_from_tools)

### A.1 PostTurnHook contract

The trait is defined in [`src/openhuman/agent/hooks.rs`](../../tmp/openhuman/src/openhuman/agent/hooks.rs) at line 85:

```rust
#[async_trait]
pub trait PostTurnHook: Send + Sync {
    /// Human-readable name for logging.
    fn name(&self) -> &str;

    /// Called after the agent produces a final response.
    /// Errors are logged but do not propagate to the caller.
    async fn on_turn_complete(&self, ctx: &TurnContext) -> anyhow::Result<()>;
}
```

The input `TurnContext` (line 16) is a snapshot of the completed turn:

```rust
pub struct TurnContext {
    pub user_message: String,
    pub assistant_response: String,
    pub tool_calls: Vec<ToolCallRecord>,
    pub turn_duration_ms: u64,
    pub session_id: Option<String>,
    pub iteration_count: usize,
}
```

Per-tool record (line 35):

```rust
pub struct ToolCallRecord {
    pub name: String,
    pub arguments: serde_json::Value,
    pub success: bool,
    /// Sanitized, non-sensitive summary (tool type, status/error class, safe message).
    /// Never contains raw tool output or PII.
    pub output_summary: String,
    pub duration_ms: u64,
}
```

#### Hook dispatch (line 210)

```rust
pub fn fire_hooks(hooks: &[Arc<dyn PostTurnHook>], ctx: TurnContext) {
    log::debug!(
        "[learning] dispatching {} post-turn hook(s) (tool_calls={}, response_chars={})",
        hooks.len(),
        ctx.tool_calls.len(),
        ctx.assistant_response.chars().count()
    );
    for (idx, hook) in hooks.iter().enumerate() {
        let hook = Arc::clone(hook);
        let ctx = ctx.clone();
        tokio::spawn(async move {
            let started = std::time::Instant::now();
            match hook.on_turn_complete(&ctx).await {
                Ok(()) => log::debug!("[learning] hook '{}' completed in {}ms", ...),
                Err(e) => log::warn!("[learning] hook '{}' failed after {}ms: {e:#}", ...),
            }
        });
    }
}
```

Key properties of the dispatcher:
- **Fire-and-forget** — `tokio::spawn` returns immediately; the agent doesn't await any hook.
- **Per-hook error isolation** — each hook gets its own task; one panicking hook can't take the others down.
- **Hook count is plural** — the slice is `&[Arc<dyn PostTurnHook>]`; openhuman registers 4 hooks (`ReflectionHook`, `UserProfileHook`, `ToolTrackerHook`, `ToolMemoryCaptureHook`) under one `LearningConfig.enabled` umbrella, plus an `ArchivistHook` that is currently only test-wired (see Part C).

#### Call site

Hooks fire from the agent's tool loop at [`src/openhuman/agent/harness/session/turn.rs:727-738`](../../tmp/openhuman/src/openhuman/agent/harness/session/turn.rs):

```rust
// Fire post-turn hooks (non-blocking)
if !self.post_turn_hooks.is_empty() {
    let ctx = TurnContext {
        user_message: user_message.to_string(),
        assistant_response: final_text.clone(),
        tool_calls: all_tool_records,
        turn_duration_ms: turn_started.elapsed().as_millis() as u64,
        session_id: None,
        iteration_count: iteration + 1,
    };
    hooks::fire_hooks(&self.post_turn_hooks, ctx);
}

return Ok(final_text);
```

Position: **after the LLM returned `final_text` and decided not to call any more tools**, before returning the response to the caller. The hooks observe the entire turn (user message + all tool calls + final assistant text).

#### Hook registration

Hooks are registered config-gated and per-agent at [`src/openhuman/agent/harness/session/builder.rs:936-997`](../../tmp/openhuman/src/openhuman/agent/harness/session/builder.rs):

```rust
let mut post_turn_hooks: Vec<Arc<dyn crate::openhuman::agent::hooks::PostTurnHook>> = Vec::new();
if config.learning.enabled {
    if config.learning.reflection_enabled {
        // ...
        post_turn_hooks.push(Arc::new(ReflectionHook::new(...)));
    }
    if config.learning.user_profile_enabled {
        post_turn_hooks.push(Arc::new(UserProfileHook::new(...)));
    }
    if config.learning.tool_tracking_enabled {
        post_turn_hooks.push(Arc::new(ToolTrackerHook::new(...)));
    }
    if config.learning.tool_memory_capture_enabled {
        post_turn_hooks.push(Arc::new(ToolMemoryCaptureHook::new(memory.clone(), true)));
    }
}
```

So hooks are **compile-time-known**, registered per-session at builder time, gated by per-hook boolean config flags. They are not loaded from disk or hot-reloaded.

`ArchivistHook` is only constructed in tests (`archivist_tests.rs`) and via a sub-agent delegation path — it is **not** in the standard `post_turn_hooks` vector. The heuristic-lesson piece (`extract_lesson_from_tools`) is therefore wired to the FTS5 episodic log via `ArchivistHook::on_turn_complete`, not directly to the prompt. (See Part C.)

### A.2 extract_lesson_from_tools

Verbatim, from [`src/openhuman/agent/harness/archivist.rs:559-577`](../../tmp/openhuman/src/openhuman/agent/harness/archivist.rs):

```rust
/// Extract simple lessons from tool call outcomes (no LLM needed).
fn extract_lesson_from_tools(
    tool_calls: &[crate::openhuman::agent::hooks::ToolCallRecord],
) -> Option<String> {
    let failures: Vec<&str> = tool_calls
        .iter()
        .filter(|tc| !tc.success)
        .map(|tc| tc.name.as_str())
        .collect();

    if failures.is_empty() {
        return None;
    }

    Some(format!(
        "Tools that failed in this turn: {}",
        failures.join(", ")
    ))
}
```

That is the **entire** function — 17 SLOC. Notes:
- **Input**: `&[ToolCallRecord]`. No access to the user message or the final assistant text.
- **Output**: `Option<String>` — `None` when every tool call succeeded; otherwise one line `"Tools that failed in this turn: <name1>, <name2>, ..."`.
- **Heuristic**: literally `any tool call has success=false`. No threshold like "≥2 failures" or "≥1 failure of a specific class".
- **Zero LLM cost** — pure list comprehension. Per docs/self-improvement-patterns-2026-05-18.md "essentially free" is accurate.
- **No deduplication** — `failures` is in call order; a tool that failed twice will appear twice in the joined string.

#### What the caller does with the Option

The lesson is consumed by `ArchivistHook::on_turn_complete` at [`archivist.rs:365-381`](../../tmp/openhuman/src/openhuman/agent/harness/archivist.rs):

```rust
// Extract a simple lesson from tool failures (lightweight, no LLM needed).
let lesson = extract_lesson_from_tools(&ctx.tool_calls);

fts5::episodic_insert(
    conn,
    &EpisodicEntry {
        id: None,
        session_id: session_id.to_string(),
        // Offset by 1ms so assistant entries sort after user entries within
        // the same turn. Relies on turn timestamps having >=1ms resolution.
        timestamp: timestamp + 0.001,
        role: "assistant".to_string(),
        content: ctx.assistant_response.clone(),
        lesson,
        tool_calls_json,
        cost_microdollars: 0,
    },
)?;
```

So the lesson is **stored as a field on the FTS5 episodic row for that assistant turn** — not promoted to a "rule", not auto-pinned. It's a passive indexable annotation. Retrieval would happen later via FTS5 search over the `lesson` column.

This is *not* the same path as `ToolMemoryCaptureHook` (see A.4) which writes actionable rules into a tool-namespace store. The Aura porter needs to decide which behaviour they want for US-OP07: the **light archive-annotation** (this function) or the **promotable rule** (`ToolMemoryCaptureHook::extract_repeated_failures`, A.4 below).

### A.3 Tool-failure detection

openhuman classifies failure exclusively via the `success: bool` field on `ToolCallRecord`. There is **no** parsing of stderr, exit codes, or result shape inside `extract_lesson_from_tools`.

The discriminator that *sets* `success` and synthesizes `output_summary` is [`hooks.rs:53-78`](../../tmp/openhuman/src/openhuman/agent/hooks.rs):

```rust
/// Produce a safe, non-sensitive summary of a tool result for learning records.
///
/// Strips raw payloads, file contents, API responses, and credentials — returns
/// only the tool name, status, error class (if failed), and a short length hint.
pub fn sanitize_tool_output(output: &str, tool_name: &str, success: bool) -> String {
    if success {
        let char_count = output.chars().count();
        return format!("{tool_name}: ok ({char_count} chars)");
    }

    // For failures, extract a safe error class without raw payload
    let lower = output.to_lowercase();
    let error_class = if lower.contains("timeout") {
        "timeout"
    } else if lower.contains("not found") || lower.contains("no such file") {
        "not_found"
    } else if lower.contains("permission") || lower.contains("denied") {
        "permission_denied"
    } else if lower.contains("connection") || lower.contains("network") {
        "connection_error"
    } else if lower.contains("parse") || lower.contains("invalid") || lower.contains("syntax") {
        "parse_error"
    } else if lower.contains("unknown tool") {
        "unknown_tool"
    } else {
        "error"
    };

    format!("{tool_name}: failed (count error_class})")
}
```

`success` itself comes from the tool executor — for a returned `anyhow::Result<...>` it's `Ok(_) → success=true`, `Err(_) → success=false`. openhuman never inspects raw payloads to *decide* success; that contract is the executor's job. The string-match in `sanitize_tool_output` is only for *classifying the error class* in the sanitized summary, not for the boolean.

**Privacy contract** — the comment explicitly says "Never contains raw tool output or PII", reinforced by the substring-only classification (no regex on free text). The `output_summary` field that lands on the `ToolCallRecord` is already redacted before any hook sees it.

### A.4 The `tool-{name}` namespace pattern

Namespacing is a one-liner at [`src/openhuman/memory/tool_memory/types.rs:144-146`](../../tmp/openhuman/src/openhuman/memory/tool_memory/types.rs):

```rust
pub fn tool_memory_namespace(tool_name: &str) -> String {
    format!("tool-{}", tool_name.trim().to_lowercase())
}
```

Notes:
- Whitespace-trimmed and lower-cased so user-typed tool names don't fork namespaces.
- The `tool-` prefix is "intentionally distinct from `global`, `skill-…` and `tool_effectiveness` so retrieval and clearing operations can reason about the namespace without ambiguity" (per the doc comment at line 140-143).
- A sentinel `__unscoped__` is reserved for edicts captured before any tool ran in the turn — see [`capture.rs:97-98`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs) and the filter in [`store.rs:209-215`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs).

The namespace is **only** used by `ToolMemoryCaptureHook` and `ToolMemoryStore`, not by `ArchivistHook`. (The Archivist writes to FTS5 / episodic; `ToolMemoryStore` writes KV rules to the `Memory` trait keyed by `tool-{name}`.)

#### Rules are isolated per tool but queried across tools

`ToolMemoryStore::list_rules(tool_name)` reads one namespace only ([`store.rs:108`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs)).

`ToolMemoryStore::rules_for_prompt(&[String])` iterates the requested tools (or every `tool-…` namespace when the slice is empty), pulls eager rules from each, then flattens and caps. See [`store.rs:161-194`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs):

```rust
pub async fn rules_for_prompt(
    &self,
    tools: &[String],
) -> Result<HashMap<String, Vec<ToolMemoryRule>>, String> {
    let tool_names = if tools.is_empty() {
        self.list_tool_names().await?
    } else {
        tools.iter().map(|name| name.trim().to_string()).filter(|name| !name.is_empty()).collect()
    };

    let mut collected: Vec<ToolMemoryRule> = Vec::new();
    for tool in &tool_names {
        let rules = self.list_rules(tool).await?;
        collected.extend(rules.into_iter().filter(|r| r.priority.is_eager()));
    }

    // Critical first, then High; within a priority, freshest first.
    collected.sort_by(|a, b| {
        b.priority.cmp(&a.priority)
            .then_with(|| b.updated_at.cmp(&a.updated_at))
    });
    collected.truncate(TOOL_MEMORY_PROMPT_CAP);

    let mut out: HashMap<String, Vec<ToolMemoryRule>> = HashMap::new();
    for rule in collected {
        out.entry(rule.tool_name.clone()).or_default().push(rule);
    }
    Ok(out)
}
```

Sort order:
1. `priority` descending (Critical > High > Normal — the enum derives `Ord` and Critical is largest, see B.3).
2. `updated_at` descending (freshest first).
3. Global cap `TOOL_MEMORY_PROMPT_CAP = 30` ([`store.rs:29`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs)). "Keeps the cache-friendly prefix bounded even when callers stash a long list of Critical rules over time."

#### Surface back to the LLM

Two paths:

1. **Auto-pin via Part B** — Critical/High rules are baked into the system prompt at session start via `with_tool_memory_rules` (next section).
2. **On-demand recall** — there is an RPC `tool_rules_for_prompt` at [`memory/ops/tool_memory.rs:138`](../../tmp/openhuman/src/openhuman/memory/ops/tool_memory.rs) and CRUD ops (`tool_rule_put`, `tool_rule_get`, `tool_rule_list`, `tool_rule_delete`) registered in [`memory/ops/mod.rs:53`](../../tmp/openhuman/src/openhuman/memory/ops/mod.rs). These are exposed to the LLM as memory tools. Lower-priority (`Normal`) rules are only reachable this way.

#### Where rules come from — auto-capture

[`src/openhuman/memory/tool_memory/capture.rs`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs) is `ToolMemoryCaptureHook` — a *separate* `PostTurnHook` that captures rules from two signals:

**(1) User edicts → Critical**

`extract_user_edicts` (line 71-126) scans the user message for sentence-level imperatives: `never `, `don't `, `do not `, sentence-boundary `stop `. Each matching sentence becomes one `(tool, rule_body)` pair. The tool name is picked via `pick_tool_for_edict` (line 239-261) which scans tool aliases ("email" → `send_email`, "shell" → `bash`/`exec`, "browser" → `web`/`http`, "slack" → `slack`/`dm`) — first match wins, fall back to the first tool that ran, fall back to `__unscoped__`.

Captured as:
```rust
self.store.record(
    &tool, &body,
    ToolMemoryPriority::Critical,        // ← Critical, always
    ToolMemorySource::UserExplicit,
    vec!["user-edict".into()],
)
```

**(2) Repeated failures → Normal**

`extract_repeated_failures` (line 135-166) tallies failure counts per tool name in the turn. A tool needs **≥2 failures in one turn** to qualify. Body is `"Tool failed N times in one turn (<sample summary>). Consider an alternative approach before retrying."`

Captured as:
```rust
self.store.record(
    &tool, &body,
    ToolMemoryPriority::Normal,          // ← Normal, NOT eagerly pinned
    ToolMemorySource::PostTurn,
    vec!["repeated-failure".into()],
)
```

Length cap: `MAX_RULE_LEN = 240` chars ([`capture.rs:43`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs)).

**Important asymmetry**: an explicit user `never email Sarah` becomes Critical and is auto-pinned next session. A repeated tool failure becomes Normal and is only retrievable via `memory_recall`. There is no auto-promotion path from Normal → High or High → Critical in this codebase.

---

## Part B — US-OP09 (ToolMemoryRulesSection)

### B.1 PromptSection trait

Defined at [`src/openhuman/agent/prompts/types.rs:258-261`](../../tmp/openhuman/src/openhuman/agent/prompts/types.rs):

```rust
pub trait PromptSection: Send + Sync {
    fn name(&self) -> &str;
    fn build(&self, ctx: &PromptContext<'_>) -> Result<String>;
}
```

The contract is intentionally minimal: a section knows its own name (for `insert_section_before` lookups) and renders to a `String` given the live `PromptContext` (workspace dir, tool list, learned context, curated memory snapshot, etc.).

#### Where sections sit in the section order

`SystemPromptBuilder::with_defaults()` at [`src/openhuman/agent/prompts/mod.rs:21-53`](../../tmp/openhuman/src/openhuman/agent/prompts/mod.rs) defines the canonical chain:

```rust
sections: vec![
    Box::new(IdentitySection),
    Box::new(UserFilesSection),       // PROFILE.md + MEMORY.md
    Box::new(UserMemorySection),      // tree summarizer roots
    Box::new(ToolsSection),
    Box::new(SafetySection),
    Box::new(WorkspaceSection),
    Box::new(DateTimeSection),
    Box::new(RuntimeSection),
],
```

`ToolMemoryRulesSection` is inserted via the `with_tool_memory_rules` builder method at [`mod.rs:185-206`](../../tmp/openhuman/src/openhuman/agent/prompts/mod.rs):

```rust
pub fn with_tool_memory_rules(
    mut self,
    rules: Vec<crate::openhuman::memory::ToolMemoryRule>,
) -> Self {
    if rules.is_empty() {
        return self;
    }
    // Insert before the tool-catalogue section so these rules appear
    // adjacent to the tool listings and survive tail-biased trimming.
    // Falls back to push when no tools section is present.
    let section: Box<dyn PromptSection> =
        Box::new(crate::openhuman::memory::ToolMemoryRulesSection::new(rules));
    let tools_idx = self
        .sections
        .iter()
        .position(|s| s.name() == "tools" || s.name() == "tool_catalogue");
    match tools_idx {
        Some(idx) => self.sections.insert(idx, section),
        None => self.sections.push(section),
    }
    self
}
```

**Key placement decision** — the section is inserted *immediately before the tool catalogue*, so the rules and the tool list they constrain are adjacent. This matters for two reasons:
1. Tail-biased context trimming (when LLMs prefer attending to later tokens) still keeps the rules and tools together.
2. The Critical safety rules sit just before the LLM enumerates tool options, mirroring how a human would read "rule: never email Sarah" right before "available tool: send_email".

The section is also early-exit when empty — no header, no placeholder. The whole rendering is the at-construction snapshot.

### B.2 ToolMemoryRulesSection render logic

[`src/openhuman/memory/tool_memory/prompt.rs:44-82`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs):

```rust
pub struct ToolMemoryRulesSection {
    rendered: String,
}

impl ToolMemoryRulesSection {
    /// Build a section from a pre-fetched rule snapshot.
    ///
    /// Rendering happens up-front so subsequent `build` calls — which
    /// run once per system prompt assembly — are I/O-free and
    /// deterministic.
    pub fn new(rules: Vec<ToolMemoryRule>) -> Self {
        Self {
            rendered: render_tool_memory_rules(&rules),
        }
    }

    pub fn empty() -> Self {
        Self { rendered: String::new() }
    }

    pub fn is_empty(&self) -> bool {
        self.rendered.trim().is_empty()
    }
}

impl PromptSection for ToolMemoryRulesSection {
    fn name(&self) -> &str { "tool_memory_rules" }

    fn build(&self, _ctx: &PromptContext<'_>) -> Result<String> {
        Ok(self.rendered.clone())
    }
}
```

**Two-phase pattern**:
1. **Construction** — render the bytes once from the rule snapshot.
2. **Build** — clone the pre-rendered string. `_ctx` is unused; the section is intentionally context-free after construction.

The pure rendering helper at [`prompt.rs:86-134`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs):

```rust
pub fn render_tool_memory_rules(rules: &[ToolMemoryRule]) -> String {
    if rules.is_empty() {
        return String::new();
    }

    // Stable order: Critical first, then High; within a priority, by
    // tool name, then by rule body. Callers may pass an already-sorted
    // list (the store does), but rendering must not depend on that
    // contract — the system prompt has to be byte-stable.
    let mut sorted: Vec<&ToolMemoryRule> = rules.iter().collect();
    sorted.sort_by(|a, b| {
        b.priority.cmp(&a.priority)
            .then_with(|| a.tool_name.cmp(&b.tool_name))
            .then_with(|| a.rule.cmp(&b.rule))
            .then_with(|| a.id.cmp(&b.id))
    });

    let mut out = String::new();
    out.push_str(TOOL_MEMORY_HEADING);
    out.push_str("\n\n");
    out.push_str(
        "These rules are pinned by the user or by the safety pipeline. Treat \
        every entry as a hard constraint when considering the matching tool — \
        do not override them silently. Lower-priority guidance lives in the \
        `tool-{name}` memory namespace and can be queried via `memory_recall` \
        if needed.\n\n",
    );

    let mut current_tool: Option<&str> = None;
    for rule in sorted {
        if current_tool != Some(rule.tool_name.as_str()) {
            if current_tool.is_some() {
                out.push('\n');
            }
            out.push_str("### `");
            out.push_str(rule.tool_name.as_str());
            out.push_str("`\n");
            current_tool = Some(rule.tool_name.as_str());
        }
        out.push_str("- ");
        out.push_str(priority_marker(rule.priority));
        out.push(' ');
        out.push_str(rule.rule.trim());
        out.push('\n');
    }

    out
}

fn priority_marker(priority: ToolMemoryPriority) -> &'static str {
    match priority {
        ToolMemoryPriority::Critical => "**[critical]**",
        ToolMemoryPriority::High => "**[high]**",
        ToolMemoryPriority::Normal => "**[normal]**",
    }
}
```

Header constant: `TOOL_MEMORY_HEADING = "## Tool-scoped rules"` ([`prompt.rs:35`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs)).

**Format characteristics**:
- One heading (`## Tool-scoped rules`).
- One short prose paragraph explaining the rules are pinned.
- Grouped by tool name as `### \`tool_name\`` sub-headings.
- Each rule as a markdown bullet with a bold priority marker prefix: `- **[critical]** never email Sarah`.
- **Deterministic byte-for-byte ordering** — the render fn re-sorts the input by `(priority desc, tool_name asc, rule asc, id asc)` even if callers pre-sorted, "so the system prompt has to be byte-stable" (line 92-94).
- **No deduplication** — duplicate rules with different IDs would both render. (Dedup is the caller's responsibility, but neither `rules_for_prompt` nor the capture path dedups.)

**Byte cap**: `TOOL_MEMORY_PROMPT_CAP = 30` rules total ([`store.rs:29`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs)). Not a byte cap. With `MAX_RULE_LEN = 240` chars per rule body, the worst-case rendered section is roughly `30 * (240 + ~30 framing) = ~8 KB`.

### B.3 The priority field

[`src/openhuman/memory/tool_memory/types.rs:26-51`](../../tmp/openhuman/src/openhuman/memory/tool_memory/types.rs):

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ToolMemoryPriority {
    /// Soft suggestion — surfaced on demand, not eagerly injected.
    Normal,
    /// Important guidance — eagerly injected at tool-selection time.
    High,
    /// Safety-critical rule — pinned into the (compression-resistant)
    /// system prompt so it survives the agent's full session.
    Critical,
}

impl Default for ToolMemoryPriority {
    fn default() -> Self {
        Self::Normal
    }
}

impl ToolMemoryPriority {
    /// True for priorities that must be eagerly surfaced to the agent
    /// (Critical/High rules are both pinned into the system prompt and
    /// prefetched at session start, so they survive context compression).
    pub fn is_eager(self) -> bool {
        matches!(self, Self::Critical | Self::High)
    }
}
```

**3 levels only**: `Normal` < `High` < `Critical`. (The doc comment in `types.rs` mentions Critical/High/Normal; nothing about a separate "Low" tier.)

Serialization: snake_case strings (`"normal"`, `"high"`, `"critical"`).

**Eager threshold**: `is_eager()` returns true for `Critical` and `High`. That's the entire "pin into system prompt" gate.

Default for missing field on deserialize: `Normal`.

#### Who sets the priority

Auto-set by source ([`capture.rs:185-218`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs)):

| Source | Priority | Set in |
|---|---|---|
| `UserExplicit` (user edict "never X") | `Critical` | `extract_user_edicts → record(...)` |
| `PostTurn` (repeated failure) | `Normal` | `extract_repeated_failures → record(...)` |
| `Programmatic` (RPC put_rule from another subsystem) | caller-supplied | `ToolMemoryStore::put_rule` |

**Note**: `extract_lesson_from_tools` (US-OP07 target) does NOT write to `ToolMemoryStore` — its lessons land on the FTS5 episodic row as a free-text annotation. So the heuristic-lesson path has no priority field at all in openhuman.

The auto-promotion direction is one-way and asymmetric — there's no code that observes "lesson X has fired 3 times, promote to High". A rule's priority is set at write-time and never updated by the system. The user (via UI or RPC `tool_rule_put` with an `id` that overwrites) is the only path to bump priority.

### B.4 The "compression-resistant" claim

This is a property of *where* the section lives, not any special bytes in the section itself. The provenance chain:

1. `SystemPromptBuilder::build()` ([`mod.rs:239+`](../../tmp/openhuman/src/openhuman/agent/prompts/mod.rs)) — the doc comment at line 231-238 is explicit:

```
/// The rendered bytes are intended to be **frozen for the whole
/// session** — callers build the system prompt once at session
/// start and reuse the exact bytes on every subsequent turn so the
/// inference backend's prefix cache hits uniformly. There is no
/// cache-boundary marker to emit because the entire prompt is
/// static from the provider's perspective.
```

2. `ToolMemoryRulesSection::new` ([`prompt.rs:54-58`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs)) — snapshot at construction, never re-queried.

3. `prefetch_tool_memory_rules_blocking` ([`builder.rs:1352-1384`](../../tmp/openhuman/src/openhuman/agent/harness/session/builder.rs)) — runs once at session-build time:

```rust
fn prefetch_tool_memory_rules_blocking(
    memory: Arc<dyn Memory>,
    tool_names: &[String],
) -> Vec<ToolMemoryRule> {
    let Ok(handle) = tokio::runtime::Handle::try_current() else { return Vec::new(); };
    if handle.runtime_flavor() != tokio::runtime::RuntimeFlavor::MultiThread {
        return Vec::new();
    }
    let tool_names = tool_names.to_vec();
    tokio::task::block_in_place(|| {
        handle.block_on(async move {
            let store = ToolMemoryStore::new(memory);
            match store.rules_for_prompt(&tool_names).await {
                Ok(grouped) => {
                    let mut flat: Vec<_> = grouped.into_values().flatten().collect();
                    flat.sort_by(|a, b| { /* (priority desc, name asc, rule asc) */ });
                    flat
                }
                Err(err) => {
                    log::warn!("[memory::tool_memory] prefetch failed: {err}");
                    Vec::new()
                }
            }
        })
    })
}
```

#### Caveats — when rules added later in the session pin

A user typing `never email Sarah` in turn 7 triggers `ToolMemoryCaptureHook` which writes the rule to `tool-send_email`. **But the system prompt has already been frozen** at turn 0. The Critical rule will only auto-pin when the *next session* builds its prompt.

Within the current session, the rule is retrievable via `memory_recall` (the `tool_rules_for_prompt` RPC), but only if the LLM explicitly calls that tool. The "fresh rules survive compression" claim is therefore **scoped to rules already present at session start** — rules added mid-session inherit normal recall-on-demand semantics until the next session boundary.

KV-cache trade-off explicit: openhuman picks "byte-stable system prompt" over "mid-session freshness". `docs/self-improvement-patterns-2026-05-18.md` flags this is the explicit anti-Aura-US-OP04 design — Aura's overlay hot-reload is the opposite choice. The porter must decide whether US-OP09 inherits openhuman's session-frozen model or aligns with Aura's existing US-OP04 hot-reload.

The relevant memo from [`prompt.rs:1-25`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs):

> Mid-session compression rewrites the rolling chat buffer but never the system prompt — that prompt is frozen for the whole session by design (so the inference backend's prefix cache stays warm; see `SystemPromptBuilder::build`).
>
> Anything we want to be compression-resistant therefore has to live in the system prompt. That is exactly where Critical and High priority `ToolMemoryRule`s belong: a "never email Sarah" rule cannot be silently dropped when the buffer fills up.

There is no special elision logic — the section is literally always rendered (post-construction). The hard-cap (30 rules) is enforced at `rules_for_prompt` time, before the section is constructed; the section itself doesn't drop rules.

---

## Part C — gotchas + failure modes

### Empty state

- `render_tool_memory_rules(&[])` returns `String::new()` — empty string ([`prompt.rs:87-89`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs)). No "no rules yet" placeholder.
- `with_tool_memory_rules(vec![])` returns the builder unchanged — the section is not even inserted ([`mod.rs:189-191`](../../tmp/openhuman/src/openhuman/agent/prompts/mod.rs)).
- Net effect: a session with zero eager rules produces zero bytes of tool-memory output. No header, no separator. The model never sees any indication the system exists. This is by design — keeps the prefix small when there's nothing to say.

### Single-threaded runtime safety

`prefetch_tool_memory_rules_blocking` ([`builder.rs:1356-1361`](../../tmp/openhuman/src/openhuman/agent/harness/session/builder.rs)) returns an empty Vec when the runtime is single-threaded:

```rust
let Ok(handle) = tokio::runtime::Handle::try_current() else {
    return Vec::new();
};
if handle.runtime_flavor() != tokio::runtime::RuntimeFlavor::MultiThread {
    return Vec::new();
}
```

This dodges a panic in `block_in_place` on single-threaded runtimes (tests, CLI bootstrap). Cost: those code paths get no pinned rules. Comment at line 1349: "Critical / High rules captured later in the session are still available via the `memory_tool_rules_for_prompt` RPC; this prefetch merely seeds the rules that exist at session start."

For Aura, this concern probably doesn't apply (Go has no equivalent single-threaded-runtime trap), but the more general point — **prefetch failures must be non-fatal** — does apply.

### Storage error handling

[`store.rs:108-140`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs) — `list_rules` skips malformed JSON rows with a warning rather than failing:

```rust
.filter_map(
    |entry| match serde_json::from_str::<ToolMemoryRule>(&entry.content) {
        Ok(rule) => Some(rule),
        Err(err) => {
            log::warn!(
                "[tool-memory] skipping malformed rule key={} tool={tool_name}: {err}",
                entry.key
            );
            None
        }
    },
)
```

Same pattern in `fetch_rule` ([`store.rs:255-260`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs)). A corrupted rule never blocks prompt assembly; it just silently drops out of the listing.

### Tool-aliasing in `pick_tool_for_edict`

[`capture.rs:266-286`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs) hard-codes aliases:

```rust
fn tool_aliases(tool_name: &str) -> Vec<&'static str> {
    let lower = tool_name.to_lowercase();
    let mut out = Vec::new();
    if lower.contains("mail") {
        out.push("email"); out.push("mail");
    }
    if lower.contains("shell") || lower.contains("bash") || lower.contains("exec") {
        out.push("shell"); out.push("terminal");
    }
    // ... browser/web/http, slack/dm
}
```

This is openhuman's own caveat — the code-comment at line 264-265 says: "Kept tiny on purpose — anything more ambitious belongs in an LLM extractor." Aura should not port this whole-cloth; the alias dictionary is openhuman-tool-specific. Aura's `file`/`scheduler`/`search_memory`/`workspace_write` tools need a different alias map, and even the noun-mapping logic itself is fragile to language and tense (per Aura memory: "Niente regex su linguaggio naturale").

### Stop-imperative edge case

[`capture.rs:80-86`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs) — `stop` as imperative needs to be sentence-anchored:

```rust
let stop_imperative =
    lower.starts_with("stop ") || lower.contains(". stop ") || lower.contains("\nstop ");
```

This was their guardrail against "I want to stop working" producing false captures. Same fragility class as the noun-aliases: regex on natural language is brittle, and openhuman knows it (the doc says "Both paths are conservative — they only fire on clear signals" at line 20-22).

### What happens when the LLM contradicts a Critical rule

**Nothing automatic.** openhuman has no detector that observes "LLM did email Sarah anyway", increments a counter, and bumps the rule. The system trusts the LLM to obey the rendered Critical rules; the prose paragraph in [`prompt.rs:108-112`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs) literally says "Treat every entry as a hard constraint when considering the matching tool — do not override them silently". The detection-and-amplification feedback loop is left to safety review / human-in-the-loop.

### Multi-turn test fixtures

The closest end-to-end is `safety_case_never_email_sarah_pins_into_prompt_block` at [`capture.rs:427-462`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs):

```rust
#[tokio::test]
async fn safety_case_never_email_sarah_pins_into_prompt_block() {
    let memory: Arc<dyn Memory> = Arc::new(MockMemory::default());
    let store = ToolMemoryStore::new(memory.clone());
    let hook = ToolMemoryCaptureHook::from_store(store.clone(), true);

    // 1. Capture the edict from a normal user turn.
    hook.on_turn_complete(&ctx_with(
        "Never email Sarah at sarah@example.com.",
        vec![call("send_email", true)],
    )).await.unwrap();

    // 2. The rule lands in the tool-scoped namespace with Critical
    //    priority — distinct from `tool_effectiveness` / global.
    let stored = store.list_rules("send_email").await.unwrap();
    assert_eq!(stored.len(), 1);
    assert_eq!(stored[0].priority, ToolMemoryPriority::Critical);

    // 3. `rules_for_prompt` pulls it eagerly so the session builder
    //    can pin it into the (compression-resistant) system prompt.
    let prompt = store.rules_for_prompt(&["send_email".to_string()]).await.unwrap();
    assert!(prompt.contains_key("send_email"));

    // 4. The rendered block is non-empty and mentions the edict
    //    verbatim — the exact bytes the safety pipeline puts in
    //    front of the agent on every subsequent turn.
    let mut flat: Vec<_> = prompt.into_values().flatten().collect();
    flat.sort_by(|a, b| b.priority.cmp(&a.priority));
    let rendered = render_tool_memory_rules(&flat);
    assert!(rendered.contains("Never email Sarah"));
    assert!(rendered.contains("**[critical]**"));
}
```

This covers: user-edict capture → store → rules_for_prompt → render. It does *not* cover heuristic-failure capture → promotion → render (no such promotion exists). It also does *not* cross a session boundary — the test simulates the pin path inside one `tokio::test` body. The mid-session-freshness gap (rule written turn 7, pinned starting turn 0 of next session) isn't exercised.

There is *no* fixture in openhuman where `extract_lesson_from_tools` flows through to the prompt — the heuristic lesson lives on the FTS5 episodic row, never in the system prompt.

---

## Part D — Recommended Aura port

### US-OP07 (heuristic post-turn lesson)

#### Hook attachment point

Candidate: [`internal/agent/loop.go`](../internal/agent/loop.go), at the spot where `executor.go` returns the final turn record before the loop yields back. The openhuman call site is "after the LLM returned `final_text` and decided not to call any more tools" — Aura's analogue is the same instant.

Read first (no edits):
- `D:/Aura/internal/agent/loop.go` — find the loop-exit branch (`steps >= max`, model produced no further tool calls, or terminal answer). The hook fires there.
- `D:/Aura/internal/agent/executor.go` — confirm where `success bool` is set per tool call. openhuman's contract relies on `success` being already-known by the time the loop ends.
- `D:/Aura/internal/agent/runtime.go` and `D:/Aura/internal/agent/session.go` — find where the per-conversation context (tool-call accumulator, session_id) lives so the hook can read the turn snapshot without re-fetching.

Suggested shape (Go, no code — design notes only):

```
type TurnContext struct {
    UserMessage      string
    AssistantText    string
    ToolCalls        []ToolCallRecord
    TurnDurationMs   int64
    ConversationID   string
    StepCount        int
}

type ToolCallRecord struct {
    Name           string
    ArgsKeys       []string  // privacy: keys only, never values (per Aura logging policy)
    Success        bool
    ErrorClass     string    // "timeout"|"not_found"|"permission_denied"|"connection"|"parse"|"error" (per openhuman sanitize_tool_output)
    DurationMs     int64
}

type PostTurnHook interface {
    Name() string
    OnTurnComplete(ctx context.Context, snapshot TurnContext) error
}
```

Dispatch policy (mirror openhuman `fire_hooks`): spawn a goroutine per hook, log errors, **never block the loop**. Aura's `internal/agent/pool.go` already has goroutine-pool primitives that could host this safely.

#### The lesson function

Mirror `extract_lesson_from_tools` in Go: walk `ToolCalls`, collect failed names, return a single string. Should be ~17 SLOC like the original. Output a Telegram-safe one-liner.

#### Storage

This is a design choice the porter must make. openhuman writes the lesson onto an FTS5 episodic row. Aura doesn't have an FTS5 episodic table, but has analogous surfaces:

1. **Annotate `conversations` archive row** — add a `lesson TEXT` column to the per-turn archive table. Simplest, most aligned with the openhuman model. Searchable via existing conversation FTS5 if Aura adds one later.
2. **Auto-call `propose_patch action=operational`** — write the heuristic lesson into the same `compact_memory_documents` table that US-OP01..05 already feeds. Auto-accepted by US-OP01. Surfaces in the top-10 system-prompt overlay via US-OP03.
3. **New `tool_lessons` table** — separate from operational lessons; queryable by `(tool_name, error_class)`. Closer to openhuman's `tool-{name}` namespace philosophy.

**Recommendation (porter's call): option 2.** The Aura pipeline US-OP01..05 already does the heavy lifting (auto-accept + top-10 injection + hot-reload). US-OP07 should be a *new producer* on the existing pipeline, not a new storage table. The hook just synthesizes "tool X failed with class Y" and calls into the same `propose_patch action=operational` path used by the LLM-driven path. Aura's US-OP01 auto-accept makes this transparent.

This also handles the gap between openhuman's two paths (Archivist annotation vs Capture rule) — Aura collapses them into one, since `compact_memory_documents` is the single source of truth.

#### Failure detection

Mirror openhuman: the boolean `Success` is set by the executor, not re-derived. The substring-based error-class classifier (timeout/not_found/permission_denied/connection_error/parse_error/error) is portable verbatim **as a design** (the actual regex strings are 6 lines and trivial to rewrite in Go). Use Aura's existing tool result envelope to source the body, then classify.

### US-OP09 (priority + always-pin)

#### Schema migration

[`D:/Aura/internal/db/migrations/`](../internal/db/migrations/) — add a numbered migration. Field design:

```sql
-- compact_memory_documents already has 'kind' (e.g. 'operational'); add 'priority':
ALTER TABLE compact_memory_documents
    ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal'
    CHECK (priority IN ('normal','high','critical'));
```

**Backwards-compat**: existing rows default to `normal`. The current US-OP03 top-10 injection continues to work unchanged for `priority='normal'` rows.

Alternative — separate table `tool_memory_rules(tool_name, rule, priority, source, tags, created_at, updated_at)`. Cleaner separation from operational lessons, but adds a new table + new tools to write/read it. The openhuman model is "separate table"; the simpler Aura port is "extend operational documents with a priority field".

#### Renderer location

[`D:/Aura/internal/conversation/system_prompt.go`](../internal/conversation/system_prompt.go) or [`D:/Aura/internal/conversation/overlay.go`](../internal/conversation/overlay.go). Read first:
- `system_prompt.go` — find the existing top-10 operational lessons block (US-OP03). The new "always-pinned Critical/High" block sits **alongside or before** that block.
- `overlay.go` — find the hot-reload trigger from US-OP04. Decide whether the always-pinned block also hot-reloads (Aura's choice) or is session-frozen (openhuman's choice).

Two sections produce the rendered lessons block:

| Section | Source query | Trigger | Cap |
|---|---|---|---|
| Always-pinned (NEW, US-OP09) | `priority IN ('high','critical')` ordered by `priority DESC, updated_at DESC` | Always when not empty | 30 rules (mirror `TOOL_MEMORY_PROMPT_CAP`) or byte-based (8 KB) |
| Top-10 operational (existing, US-OP03) | `priority = 'normal'` ordered by `updated_at DESC LIMIT 10` | When not empty | 10 entries / 5 KB |

#### Render format

Mirror openhuman's layout (in Markdown, since Aura overlays are MD):

```
## Pinned operational rules

These rules are pinned by the user or by the safety pipeline. Treat every entry as a hard constraint — do not override them silently.

- **[critical]** <rule body 1>
- **[critical]** <rule body 2>
- **[high]** <rule body 3>
```

Or group by tool name if Aura adopts the per-tool namespace. Aura's current `compact_memory_documents kind='operational'` doesn't carry a tool-name field; if that's added, group-by-tool becomes possible. If not, a single flat bullet list is fine.

#### Hot-reload trade-off

openhuman session-freezes (KV-cache hit). Aura US-OP04 already hot-reloads (Telegram session can last weeks; mid-session freshness matters more than cache hit rate on a self-hosted endpoint).

**Recommendation: stay consistent with US-OP04 — hot-reload Critical/High alongside the existing top-10 reload.** Aura's KV-cache trade-off was already decided in favour of freshness. The `compression-resistant` guarantee in Aura then comes from "the lessons block always renders in the system prompt overlay" rather than "the system prompt bytes never change".

### Promotion logic — open decision

openhuman has no auto-promotion from `Normal` → `Critical`. Promotion is user/explicit. Aura must decide whether US-OP07's heuristic lesson defaults to `priority='normal'` (recall-on-demand only, mirrors openhuman repeated-failures) or `priority='high'` (auto-pin every tool failure into the prompt).

See Part E.

---

## Part E — Open decisions for Aura

| # | Question | openhuman answer | Suggested Aura answer | Why |
|---|---|---|---|---|
| 1 | Hook firing condition | Unconditional after every turn; the lesson function self-gates by `failures.is_empty()` | Same — unconditional, function self-gates | Symmetric. Cheap when there are no failures (early return). |
| 2 | Number of priority levels | 3: Normal/High/Critical | 3: same. The current `compact_memory_documents.kind='operational'` continues to exist orthogonally | 3 maps cleanly to "soft / eager / pinned"; adding Low has no use-case in the openhuman model and inflates the schema. |
| 3 | Auto-promotion to Critical | Not implemented | Skip for v1. Reconsider after observing real lesson churn. | openhuman explicitly chose "Critical = user edict only". Implicit promotion is hard to get right and easy to make annoying. |
| 4 | Default priority for US-OP07 heuristic lesson | N/A (openhuman writes these to FTS5, no priority) | `priority='high'`. A tool failure observed by Aura's hook is concrete operational signal — pin it. | This deviates from openhuman, but openhuman's split (heuristic→FTS5, edict→rule) isn't well-motivated for Aura's unified `compact_memory_documents` model. If the porter wants strict openhuman parity, default to `'normal'`. |
| 5 | Always-pin cap: bytes vs count | Count (30 rules), with per-rule body cap (240 chars) → ~8 KB worst-case | Count + byte ceiling. Cap at 30 rules AND 8 KB rendered. Whichever hits first wins. | Belt-and-suspenders. A single 5 KB pathological rule body shouldn't blow the budget. |
| 6 | Sort tie-break | `priority desc, tool_name asc, rule asc, id asc` (stable, byte-deterministic) | Same. Aura's hot-reload trade-off still benefits from deterministic rendering for given DB state. | Predictability; testability. |
| 7 | What happens to existing US-OP03 top-10? | N/A | Keep US-OP03 unchanged. Add US-OP09 as a *new* section "Pinned" above the existing "Recent operational" section. `priority='normal'` rules flow through US-OP03; `priority IN ('high','critical')` rules flow through US-OP09. | Migration safety. Existing `kind='operational' priority='normal'` rows continue to render exactly as before. |
| 8 | User-edict capture (analogue of `ToolMemoryCaptureHook::extract_user_edicts`) | Implemented; regex on user message | **Skip for US-OP09.** Per Aura memory "Niente regex su linguaggio naturale", this is brittle. Aura's existing `propose_patch action=operational` already supports priority via tool args — let the LLM tag user edicts as `priority='critical'` consciously. | Aligns with `feedback_no_regex_for_nlp` memory; keeps the heuristic surface small. |
| 9 | Repeated-failure capture (analogue of `extract_repeated_failures`) | Implemented; `Normal` priority, ≥2 failures threshold | Subsumed by US-OP07 — if Aura's hook fires on every turn with any failure, repeated-failures are already captured. The "≥2 in one turn" threshold can be added later as a stricter heuristic if noise becomes a problem. | Simpler v1; observable, then tuned. |
| 10 | Where to attach the prompt section in overlay order | Immediately before `ToolsSection` (rules adjacent to tool catalogue) | Mirror: render the pinned-rules block right before the tool list in the overlay assembly. If Aura's overlay doesn't split a "tools" segment, attach right after `SOUL`/`AGENT` and before the recent-operational top-10. | Tail-attendance bias makes adjacency to the tool catalogue useful. |

---

## License check

**openhuman is licensed GPLv3** (see `D:/tmp/openhuman/LICENSE`). This research extracts **concepts and design notes only** — no Rust code may be copy-pasted into Aura. The verbatim quotes in this document are for analysis under fair use; the Go implementation must be rewritten from scratch using these notes as a specification.

In particular:
- The `extract_lesson_from_tools` function shape (filter-map-collect-format) is a generic functional-programming pattern; the Go port should be a straightforward `for ... if !success { append }` loop. No translation, fresh code.
- The `ToolMemoryRulesSection` rendering format (heading, prose paragraph, per-tool sub-headings, priority markers) is sufficiently described in this document that the Go port can be written without re-reading `prompt.rs`.
- The schema (`Normal`/`High`/`Critical`, `tool_memory_namespace`, sort order) is conceptual; the SQL migration and Go enum should be written fresh.
- The 3-level priority enum, `is_eager()` predicate, and `TOOL_MEMORY_PROMPT_CAP = 30` value are design constants, not copyrighted expressions.

Pattern E from the codebase study (`docs/self-improvement-patterns-2026-05-18.md`): openhuman's `ArchivistHook` + `ToolMemoryRulesSection` is "the strongest pre-existing design for Pattern C (retrospective reflection loop) in any of the codebases studied". Aura's port should adopt the **architecture** (post-turn hook + priority field + always-pin renderer) and rewrite the **bytes**.

---

## Appendix — file:line reference index

US-OP07 (PostTurnHook + heuristic lesson):
- Trait + dispatcher: [`src/openhuman/agent/hooks.rs:85`](../../tmp/openhuman/src/openhuman/agent/hooks.rs#L85) (trait), [`L210`](../../tmp/openhuman/src/openhuman/agent/hooks.rs#L210) (`fire_hooks`)
- `TurnContext` + `ToolCallRecord`: [`hooks.rs:16-47`](../../tmp/openhuman/src/openhuman/agent/hooks.rs#L16)
- `sanitize_tool_output` (error classifier): [`hooks.rs:53-78`](../../tmp/openhuman/src/openhuman/agent/hooks.rs#L53)
- `ArchivistHook::on_turn_complete`: [`src/openhuman/agent/harness/archivist.rs:317`](../../tmp/openhuman/src/openhuman/agent/harness/archivist.rs#L317)
- `extract_lesson_from_tools`: [`archivist.rs:559-577`](../../tmp/openhuman/src/openhuman/agent/harness/archivist.rs#L559)
- Hook fire call site: [`src/openhuman/agent/harness/session/turn.rs:727-738`](../../tmp/openhuman/src/openhuman/agent/harness/session/turn.rs#L727)
- Hook registration: [`src/openhuman/agent/harness/session/builder.rs:936-997`](../../tmp/openhuman/src/openhuman/agent/harness/session/builder.rs#L936)
- LearningConfig: [`src/openhuman/config/schema/learning.rs:22`](../../tmp/openhuman/src/openhuman/config/schema/learning.rs#L22)

US-OP09 (priority schema + always-pin):
- `ToolMemoryPriority` enum + `is_eager()`: [`src/openhuman/memory/tool_memory/types.rs:26-51`](../../tmp/openhuman/src/openhuman/memory/tool_memory/types.rs#L26)
- `ToolMemoryRule` struct: [`types.rs:80-103`](../../tmp/openhuman/src/openhuman/memory/tool_memory/types.rs#L80)
- `tool_memory_namespace`: [`types.rs:144-146`](../../tmp/openhuman/src/openhuman/memory/tool_memory/types.rs#L144)
- `ToolMemoryStore::rules_for_prompt`: [`src/openhuman/memory/tool_memory/store.rs:161-194`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs#L161)
- `TOOL_MEMORY_PROMPT_CAP`: [`store.rs:29`](../../tmp/openhuman/src/openhuman/memory/tool_memory/store.rs#L29)
- `ToolMemoryRulesSection`: [`src/openhuman/memory/tool_memory/prompt.rs:44-82`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs#L44)
- `render_tool_memory_rules`: [`prompt.rs:86-134`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs#L86)
- `priority_marker`: [`prompt.rs:136-142`](../../tmp/openhuman/src/openhuman/memory/tool_memory/prompt.rs#L136)
- `PromptSection` trait: [`src/openhuman/agent/prompts/types.rs:258-261`](../../tmp/openhuman/src/openhuman/agent/prompts/types.rs#L258)
- Section ordering / `with_defaults`: [`src/openhuman/agent/prompts/mod.rs:21-53`](../../tmp/openhuman/src/openhuman/agent/prompts/mod.rs#L21)
- `with_tool_memory_rules` injection: [`mod.rs:185-206`](../../tmp/openhuman/src/openhuman/agent/prompts/mod.rs#L185)
- Builder freeze contract: [`mod.rs:231-238`](../../tmp/openhuman/src/openhuman/agent/prompts/mod.rs#L231)
- Prefetch glue: [`src/openhuman/agent/harness/session/builder.rs:1138-1149`](../../tmp/openhuman/src/openhuman/agent/harness/session/builder.rs#L1138), [`1352-1384`](../../tmp/openhuman/src/openhuman/agent/harness/session/builder.rs#L1352)

Capture (related, not the direct port target but informs design):
- `ToolMemoryCaptureHook` + edict/repeated-failure extraction: [`src/openhuman/memory/tool_memory/capture.rs`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs) (entire file)
- End-to-end safety-case test: [`capture.rs:427-462`](../../tmp/openhuman/src/openhuman/memory/tool_memory/capture.rs#L427)
